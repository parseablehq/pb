package duckdb

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	internalHTTP "pb/pkg/http"
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
		var namespace, objectName, reasonSummary string
		if err := rows.Scan(&namespace, &objectName, &reasonSummary); err != nil {
			return fmt.Errorf("error scanning summary row: %w", err)
		}
		_, err := db.Exec(
			`INSERT INTO reason_summary_stats (involvedObject_namespace, involvedObject_name, reason_summary) VALUES (?, ?, ?)`,
			namespace, objectName, reasonSummary,
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
