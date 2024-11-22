package cmd

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os" // import os for Stdout
	internalHTTP "pb/pkg/http"

	_ "github.com/marcboeker/go-duckdb"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

var AnalyzeCmd = &cobra.Command{
	Use:     "stream", // Subcommand for "analyze"
	Short:   "Analyze streams in the Parseable server",
	Example: "pb analyze stream <stream-name>",
	Args:    cobra.ExactArgs(1), // Ensure exactly one argument is passed
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		fmt.Printf("Analyzing stream: %s\n", name)

		client := internalHTTP.DefaultClient(&DefaultProfile)

		query := `with distinct_name as (select distinct(\"involvedObject_name\") as name from \"k8s-events\" where reason ilike '%kill%' or reason ilike '%fail%' or reason ilike '%back%') select reason, message, \"involvedObject_name\", \"involvedObject_namespace\", \"reportingComponent\", p_timestamp from \"k8s-events\" as t1 join distinct_name t2 on t1.\"involvedObject_name\" = t2.name order by p_timestamp`

		allData, err := queryPb(&client, query, "2024-11-11T00:00:00+00:00", "2024-11-21T00:00:00+00:00")
		if err != nil {
			return err
		}

		// Insert the response into DuckDB
		if err := storeInDuckDB(allData); err != nil {
			return err
		}

		// Fetch and display the reason summary statistics
		stats, err := fetchSummaryStats()
		if err != nil {
			return err
		}

		// Display the statistics in a table
		displaySummaryTable(stats)

		// Prompt user for namespace and involved object
		var selectedNamespace, selectedObject string
		fmt.Print("\nEnter the namespace you're interested in: ")
		fmt.Scan(&selectedNamespace)

		// Prompt the user to select an involved object
		fmt.Print("\nEnter the involved object you're interested in: ")
		fmt.Scan(&selectedObject)

		// Display the selected data
		displaySelectedData(selectedNamespace, selectedObject)

		return nil
	},
}

func queryPb(client *internalHTTP.HTTPClient, query, startTime, endTime string) (string, error) {
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

func storeInDuckDB(data string) error {
	// Parse the JSON response
	var jsonResponse []map[string]interface{}
	if err := json.Unmarshal([]byte(data), &jsonResponse); err != nil {
		return fmt.Errorf("error decoding JSON response: %w", err)
	}

	// Open a connection to DuckDB
	db, err := sql.Open("duckdb", "mydatabasenew.duckdb")
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
		p_timestamp TIMESTAMP
	)`)
	if err != nil {
		return fmt.Errorf("error creating table: %w", err)
	}

	// Insert data into the table
	for _, record := range jsonResponse {
		_, err := db.Exec(
			`INSERT INTO k8s_events (reason, message, involvedObject_name, involvedObject_namespace, reportingComponent, p_timestamp) VALUES (?, ?, ?, ?, ?, ?)`,
			record["reason"],
			record["message"],
			record["involvedObject_name"],
			record["involvedObject_namespace"],
			record["reportingComponent"],
			record["p_timestamp"],
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

func fetchSummaryStats() ([]SummaryStat, error) {
	// Open a connection to DuckDB
	db, err := sql.Open("duckdb", "mydatabasenew.duckdb")
	if err != nil {
		return nil, fmt.Errorf("error connecting to DuckDB: %w", err)
	}
	defer db.Close()

	// Query to fetch summary statistics
	rows, err := db.Query(`
		SELECT involvedObject_namespace, involvedObject_name, reason_summary
		FROM reason_summary_stats
		ORDER BY involvedObject_namespace, involvedObject_name
	`)
	if err != nil {
		return nil, fmt.Errorf("error executing summary query: %w", err)
	}
	defer rows.Close()

	var stats []SummaryStat
	for rows.Next() {
		var namespace, objectName, reasonSummary string
		if err := rows.Scan(&namespace, &objectName, &reasonSummary); err != nil {
			return nil, fmt.Errorf("error scanning summary row: %w", err)
		}
		stats = append(stats, SummaryStat{
			Namespace:     namespace,
			ObjectName:    objectName,
			ReasonSummary: reasonSummary,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error reading rows: %w", err)
	}

	return stats, nil
}

func displaySummaryTable(stats []SummaryStat) {
	table := tablewriter.NewWriter(os.Stdout) // Use os.Stdout for io.Writer
	table.SetHeader([]string{"Namespace", "Involved Object", "Reason Summary"})

	for _, stat := range stats {
		table.Append([]string{stat.Namespace, stat.ObjectName, stat.ReasonSummary})
	}

	table.Render()
}

func displaySelectedData(namespace, objectName string) {
	// Open a connection to DuckDB
	db, err := sql.Open("duckdb", "mydatabasenew.duckdb")
	if err != nil {
		fmt.Printf("Error connecting to DuckDB: %v\n", err)
		return
	}
	defer db.Close()

	// Query to fetch selected data based on namespace and object name
	query := `
		SELECT reason, message, reportingComponent, p_timestamp
		FROM k8s_events
		WHERE involvedObject_namespace = ? AND involvedObject_name = ?
	`
	rows, err := db.Query(query, namespace, objectName)
	if err != nil {
		fmt.Printf("Error executing query: %v\n", err)
		return
	}
	defer rows.Close()

	// Display the result
	fmt.Printf("\nSelected Data for Namespace: %s, Object: %s\n", namespace, objectName)
	for rows.Next() {
		var reason, message, component, timestamp string
		if err := rows.Scan(&reason, &message, &component, &timestamp); err != nil {
			fmt.Printf("Error scanning row: %v\n", err)
			return
		}
		fmt.Printf("Reason: %s\nMessage: %s\nComponent: %s\nTimestamp: %s\n\n", reason, message, component, timestamp)
	}

	if err := rows.Err(); err != nil {
		fmt.Printf("Error reading rows: %v\n", err)
	}
}

// Struct to hold summary statistics
type SummaryStat struct {
	Namespace     string
	ObjectName    string
	ReasonSummary string
}
