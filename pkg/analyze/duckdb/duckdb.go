package duckdb

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"pb/pkg/analyze/k8s"
	internalHTTP "pb/pkg/http"
	"strings"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// Struct to hold summary statistics (same as before)
type SummaryStat struct {
	Reason             string
	Message            string
	ObjectName         string
	ObjectNamespace    string
	ReportingComponent string
	Timestamp          string
}

func QueryPb(client *internalHTTP.HTTPClient, query, startTime, endTime string) (string, error) {
	queryTemplate := `{
		"query": "%s",
		"startTime": "%s",
		"endTime": "%s"
	}`
	finalQuery := fmt.Sprintf(queryTemplate, query, startTime, endTime)

	req, err := client.NewRequest("POST", "query", bytes.NewBuffer([]byte(finalQuery)))
	if err != nil {
		return "", fmt.Errorf("failed to create new request: %w", err)
	}

	resp, err := client.Client.Do(req)
	if err != nil {
		return "", fmt.Errorf("request execution failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		fmt.Println(string(body))
		return "", fmt.Errorf("non-200 status code received: %s", resp.Status)
	}

	var jsonResponse []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&jsonResponse); err != nil {
		return "", fmt.Errorf("error decoding JSON response: %w", err)
	}
	encodedResponse, _ := json.MarshalIndent(jsonResponse, "", "  ")

	return string(encodedResponse), nil
}

func StoreInDuckDB(data string) error {
	// Parse the JSON response
	var jsonResponse []map[string]interface{}
	if err := json.Unmarshal([]byte(data), &jsonResponse); err != nil {
		return fmt.Errorf("error decoding JSON response: %w", err)
	}

	// Open a connection to DuckDB
	db, err := sql.Open("duckdb", "k8s_events.duckdb")
	if err != nil {
		return fmt.Errorf("error connecting to DuckDB: %w", err)
	}
	defer db.Close()

	// Create a table if it doesn't exist
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS k8s_events (
		reason VARCHAR,
		message VARCHAR,
		involvedObject_name VARCHAR,
		involvedObject_namespace VARCHAR,
		reportingComponent VARCHAR,
		timestamp TIMESTAMP
	)`)
	if err != nil {
		return fmt.Errorf("error creating table: %w", err)
	}

	// Insert data into the table
	for _, record := range jsonResponse {
		_, err := db.Exec(
			`INSERT INTO k8s_events (reason, message, involvedObject_name, involvedObject_namespace, reportingComponent, timestamp) VALUES (?, ?, ?, ?, ?, ?)`,
			record["reason"],
			record["message"],
			record["involvedObject_name"],
			record["involvedObject_namespace"],
			record["reportingComponent"],
			record["timestamp"],
		)
		if err != nil {
			return fmt.Errorf("error inserting record into DuckDB: %w", err)
		}
	}

	// Run the reason_counts query to get summary statistics
	summaryQuery := `
		WITH reason_counts AS (
			SELECT 
				involvedObject_name,
				involvedObject_namespace,
				reason,
				COUNT(*) AS reason_count
			FROM 
				k8s_events
			GROUP BY 
				involvedObject_name,
				involvedObject_namespace,
				reason
		)
		SELECT 
			involvedObject_namespace,
			involvedObject_name,
			STRING_AGG(CONCAT(reason, ' ', reason_count, ' times'), ', ' ORDER BY reason) AS reason_summary
		FROM 
			reason_counts
		GROUP BY 
			involvedObject_namespace,
			involvedObject_name
		ORDER BY 
			involvedObject_namespace, involvedObject_name;
	`

	// Execute the summary query
	rows, err := db.Query(summaryQuery)
	if err != nil {
		return fmt.Errorf("error executing summary query: %w", err)
	}
	defer rows.Close()

	// Create a summary table if it doesn't exist
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS reason_summary_stats (
		involvedObject_namespace VARCHAR,
		involvedObject_name VARCHAR,
		reason_summary VARCHAR
	)`)
	if err != nil {
		return fmt.Errorf("error creating summary table: %w", err)
	}

	// Insert the summary stats into the new table
	for rows.Next() {
		var namespace, objectName, reasonSummary sql.NullString
		if err := rows.Scan(&namespace, &objectName, &reasonSummary); err != nil {
			return fmt.Errorf("error scanning summary row: %w", err)
		}

		// Handle NULL values and convert to empty strings if needed
		if !namespace.Valid {
			namespace.String = "" // or some default value
		}
		if !objectName.Valid {
			objectName.String = "" // or some default value
		}
		if !reasonSummary.Valid {
			reasonSummary.String = "" // or some default value
		}

		_, err := db.Exec(
			`INSERT INTO reason_summary_stats (involvedObject_namespace, involvedObject_name, reason_summary) VALUES (?, ?, ?)`,
			namespace.String, objectName.String, reasonSummary.String,
		)
		if err != nil {
			return fmt.Errorf("error inserting summary record into DuckDB: %w", err)
		}
	}

	return nil
}

// fetchPodEventsfromDb fetches summary statistics for a given pod from DuckDB.
func FetchPodEventsfromDb(podName string) ([]SummaryStat, error) {
	// Open a connection to DuckDB
	db, err := sql.Open("duckdb", "k8s_events.duckdb")
	if err != nil {
		return nil, fmt.Errorf("error connecting to DuckDB: %w", err)
	}
	defer db.Close()

	// Prepare the query with a placeholder for podName
	query := `
		SELECT DISTINCT ON (message) *
		FROM k8s_events
		WHERE involvedObject_name = ?
		ORDER BY "timestamp";
	`

	// Execute the query with podName as a parameter
	rows, err := db.Query(query, podName)
	if err != nil {
		return nil, fmt.Errorf("error executing summary query: %w", err)
	}
	defer rows.Close()

	var stats []SummaryStat
	for rows.Next() {
		var reason, message, objectName, objectNamespace, reportingComponent, timestamp string
		if err := rows.Scan(&reason, &message, &objectName, &objectNamespace, &reportingComponent, &timestamp); err != nil {
			return nil, fmt.Errorf("error scanning summary row: %w", err)
		}
		stats = append(stats, SummaryStat{
			Reason:             reason,
			Message:            message,
			ObjectName:         objectName,
			ObjectNamespace:    objectNamespace,
			ReportingComponent: reportingComponent,
			Timestamp:          timestamp,
		})

		// Check if the message mentions another object like PVC
		referencedObject, _ := extractReferencedObject(message, objectName, objectNamespace)
		fmt.Println("rfo", referencedObject)
		if referencedObject != "" {
			// Fetch additional events for the referenced object
			relatedEvents, err := fetchRelatedEvents(db, referencedObject)
			if err != nil {
				return nil, fmt.Errorf("error fetching related events for %s: %w", referencedObject, err)
			}
			stats = append(stats, relatedEvents...)
		}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error reading rows: %w", err)
	}

	return stats, nil
}

// FetchNamespaceEventsfromDb fetches summary statistics for a given namespace from DuckDB.
func FetchNamespaceEventsfromDb(namespace string) ([]SummaryStat, error) {
	// Open a connection to DuckDB
	db, err := sql.Open("duckdb", "k8s_events.duckdb")
	if err != nil {
		return nil, fmt.Errorf("error connecting to DuckDB: %w", err)
	}
	defer db.Close()

	// Prepare the query with a placeholder for podName
	query := `
		SELECT DISTINCT ON (message) *
		FROM k8s_events
		WHERE involvedObject_namespace = ?
		ORDER BY "timestamp";
	`

	// Execute the query with namespace as a parameter
	rows, err := db.Query(query, namespace)
	if err != nil {
		return nil, fmt.Errorf("error executing summary query: %w", err)
	}
	defer rows.Close()

	var stats []SummaryStat
	for rows.Next() {
		var reason, message, objectName, objectNamespace, reportingComponent, timestamp string
		if err := rows.Scan(&reason, &message, &objectName, &objectNamespace, &reportingComponent, &timestamp); err != nil {
			return nil, fmt.Errorf("error scanning summary row: %w", err)
		}
		stats = append(stats, SummaryStat{
			Reason:             reason,
			Message:            message,
			ObjectName:         objectName,
			ObjectNamespace:    objectNamespace,
			ReportingComponent: reportingComponent,
			Timestamp:          timestamp,
		})

	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error reading rows: %w", err)
	}

	return stats, nil
}

// fetchRelatedEvents retrieves events for the specified object from DuckDB.
func fetchRelatedEvents(db *sql.DB, objectName string) ([]SummaryStat, error) {
	query := `
		SELECT DISTINCT ON (message) *
		FROM k8s_events
		WHERE involvedObject_name = ?
		ORDER BY "timestamp";
	`

	fmt.Println("try", query)
	rows, err := db.Query(query, objectName)
	if err != nil {
		return nil, fmt.Errorf("error executing related events query: %w", err)
	}
	defer rows.Close()

	var relatedStats []SummaryStat
	for rows.Next() {
		var reason, message, objectName, objectNamespace, reportingComponent, timestamp string
		if err := rows.Scan(&reason, &message, &objectName, &objectNamespace, &reportingComponent, &timestamp); err != nil {
			return nil, fmt.Errorf("error scanning related row: %w", err)
		}

		relatedStats = append(relatedStats, SummaryStat{
			Reason:             reason,
			Message:            message,
			ObjectName:         objectName,
			ObjectNamespace:    objectNamespace,
			ReportingComponent: reportingComponent,
			Timestamp:          timestamp,
		})
	}

	return relatedStats, nil
}

// getPodLabels fetches labels of the given pod using the Kubernetes API.
func getPodLabels(podName, namespace string) (map[string]string, error) {

	clientset := k8s.GetKubeClient()

	// Fetch the pod
	pod, err := clientset.CoreV1().Pods(namespace).Get(context.TODO(), podName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("error fetching pod: %w", err)
	}

	return pod.Labels, nil
}

// findPVCWithLabels searches for a PVC in the namespace matching the pod labels.
func findPVCWithLabels(clientset *kubernetes.Clientset, namespace string, labels map[string]string) (string, error) {
	pvcs, err := clientset.CoreV1().PersistentVolumeClaims(namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return "", fmt.Errorf("error listing PVCs: %w", err)
	}

	for _, pvc := range pvcs.Items {
		if pvcMatchesLabels(&pvc, labels) {
			return pvc.Name, nil
		}
	}

	return "", fmt.Errorf("no matching PVC found")
}

// pvcMatchesLabels checks if the PVC has at least one matching label.
func pvcMatchesLabels(pvc *v1.PersistentVolumeClaim, labels map[string]string) bool {
	for key, value := range labels {
		if pvcValue, exists := pvc.Labels[key]; exists && pvcValue == value {
			return true // Return true if any label matches.
		}
	}
	return false // No matching label found.
}

// extractReferencedObject checks for mentions of specific objects and retrieves the PVC name.
func extractReferencedObject(message, podName, namespace string) (string, error) {

	if !strings.Contains(message, "PersistentVolumeClaims") {

		labels, err := getPodLabels(podName, namespace)
		if err != nil {
			return "", fmt.Errorf("error fetching pod labels: %w", err)
		}

		pvcName, err := findPVCWithLabels(k8s.GetKubeClient(), namespace, labels)
		if err != nil {
			return "", fmt.Errorf("error finding PVC: %w", err)
		}

		return pvcName, nil
	}

	return "", nil
}
