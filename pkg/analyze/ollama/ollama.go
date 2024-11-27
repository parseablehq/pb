package ollama

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"pb/pkg/analyze/duckdb"
)

// Define the structure for the Ollama request
type OllamaRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	Stream bool   `json:"stream"` // Set to false for a single response
}

// Define the structure for the response from Ollama
type OllamaResponse struct {
	Response string `json:"response"`
	Done     bool   `json:"done"`
}

// Function to analyze events using Ollama
func AnalyzeEventsWithOllama(podName, namespace string, data []duckdb.SummaryStat) (string, error) {
	// Format the data into a readable string
	var formattedData string
	for _, stat := range data {
		formattedData += fmt.Sprintf("Reason: %s, Message: %s, Timestamp: %s\n", stat.Reason, stat.Message, stat.Timestamp)
	}

	// Create the prompt with the data
	prompt := fmt.Sprintf(
		`You are an expert at debugging Kubernetes Events. 
		I have a table containing those events and want to debug what is happening in this pod (%s) / namespace (%s). 
		Give me a detailed summary of what happened by looking at these events. 
		Provide a root cause analysis and suggest steps to mitigate the error if present.
		When sending the response give it in a structured body json. With fields summary, root_cause_analysis and mitigation_steps.
		Don't add any json keywords in the response, make sure it just a clean json dump. Please adhere to the following structure
		type AnalysisResponse struct {
			summary             string   json:"summary"
			root_cause_analysis string   json:"root_cause_analysis"
			mitigation_steps   []string json:"mitigation_steps"
		}
		In mitigation steps give a command to get logs. The general command to get logs is 'kubectl logs <pod_name> -n <namespace>. 
		Make sure you give a clean json response, without an '''json in the beginning. Please respect the json tags should be summary, root_cause_analysis and mitigation_steps.
		Make sure summary is a string, root_cause_analysis is a string and mitigation_steps is []string. Please respect the datatypes.
		In case you are unable to figure out what happened, just say "I'm unable to figure out what is happening here.".
		%s`, podName, namespace, formattedData)

	// Build the Ollama request payload
	ollamaRequest := OllamaRequest{
		Model:  "llama3.2", // Adjust model as needed
		Prompt: prompt,
		Stream: false,
	}

	// Marshal the request to JSON
	payload, err := json.Marshal(ollamaRequest)
	if err != nil {
		return "", fmt.Errorf("failed to marshal Ollama request: %w", err)
	}

	// Send the request to the Ollama API
	req, err := http.NewRequest("POST", "http://"+os.Getenv("P_LLM_ENDPOINT")+"/api/generate", bytes.NewBuffer(payload))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send request to Ollama: %w", err)
	}
	defer resp.Body.Close()

	// Parse the Ollama response
	var ollamaResponse OllamaResponse
	if err := json.NewDecoder(resp.Body).Decode(&ollamaResponse); err != nil {
		return "", fmt.Errorf("failed to decode Ollama response: %w", err)
	}

	// Return the Ollama response
	if ollamaResponse.Done {
		return ollamaResponse.Response, nil
	}

	return "No response from Ollama.", nil
}
