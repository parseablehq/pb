package openai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"pb/pkg/analyze/duckdb"
)

// Define the structure for the OpenAI request
type OpenAIRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Define the structure for the response from OpenAI
type OpenAIResponse struct {
	Choices []struct {
		Message Message `json:"message"`
	} `json:"choices"`
}

// Function to send events to OpenAI GPT
func AnalyzeEventsWithGPT(podName, namespace string, data []duckdb.SummaryStat) (string, error) {
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

	// Build the OpenAI request payload
	openAIRequest := OpenAIRequest{
		Model: "gpt-4o",
		Messages: []Message{
			{Role: "user", Content: prompt},
		},
	}

	// Marshal the request to JSON
	payload, err := json.Marshal(openAIRequest)
	if err != nil {
		return "", fmt.Errorf("failed to marshal OpenAI request: %w", err)
	}

	// Send the request to the OpenAI API
	apiKey := os.Getenv("P_LLM_KEY")
	req, err := http.NewRequest("POST", "https://api.openai.com/v1/chat/completions", bytes.NewBuffer(payload))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send request to OpenAI: %w", err)
	}
	defer resp.Body.Close()

	// Parse the OpenAI response
	var openAIResponse OpenAIResponse

	if err := json.NewDecoder(resp.Body).Decode(&openAIResponse); err != nil {
		return "", fmt.Errorf("failed to decode OpenAI response: %w", err)
	}

	// Return the GPT response
	if len(openAIResponse.Choices) > 0 {
		return openAIResponse.Choices[0].Message.Content, nil
	}

	return "No response from OpenAI.", nil
}
