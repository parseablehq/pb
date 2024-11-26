package anthropic

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"pb/pkg/analyze/duckdb"
)

// Define the structure for the Anthropic request
type AnthropicRequest struct {
	Model     string    `json:"model"`
	MaxTokens int       `json:"max_tokens"`
	Messages  []Message `json:"messages"`
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Define the structure for the response from Anthropic
type AnthropicResponse struct {
	Completion string `json:"completion"`
}

// Function to send events to Anthropic API
func AnalyzeEventsWithAnthropic(podName, namespace string, data []duckdb.SummaryStat) (string, error) {
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
			Summary           string   json:"summary"
			RootCauseAnalysis string   json:"root_cause_analysis"
			MitigationSteps   []string json:"mitigation_steps"
		}
		In mitigation steps give a command to get logs.
		In case you are unable to figure out what happened, just say "I'm unable to figure out what is happening here.".
		%s`, podName, namespace, formattedData)

	// Build the Anthropic request payload
	anthropicRequest := AnthropicRequest{
		Model:     "claude-3-5-sonnet-20241022",
		MaxTokens: 1024,
		Messages: []Message{
			{Role: "user", Content: prompt},
		},
	}

	// Marshal the request to JSON
	payload, err := json.Marshal(anthropicRequest)
	if err != nil {
		return "", fmt.Errorf("failed to marshal Anthropic request: %w", err)
	}

	// Send the request to the Anthropic API
	apiKey := os.Getenv("P_LLM_KEY")
	req, err := http.NewRequest("POST", "https://api.anthropic.com/v1/messages", bytes.NewBuffer(payload))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send request to Anthropic: %w", err)
	}
	defer resp.Body.Close()

	// Read the response body
	bodyBytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	// Debugging: Print the raw response body for inspection
	fmt.Printf("Raw LLM Response: %s\n", string(bodyBytes))

	// Parse the Anthropic response
	var anthropicResponse AnthropicResponse
	if err := json.NewDecoder(resp.Body).Decode(&anthropicResponse); err != nil {
		return "", fmt.Errorf("failed to decode Anthropic response: %w", err)
	}

	// Return the Anthropic response
	return anthropicResponse.Completion, nil
}
