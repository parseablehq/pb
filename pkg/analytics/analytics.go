package analytics

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"pb/pkg/config"
	internalHTTP "pb/pkg/http"

	"github.com/oklog/ulid/v2"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"gopkg.in/yaml.v2"
)

type Event struct {
	CLIVersion         string         `json:"cli_version"`
	ULID               string         `json:"ulid"`
	CommitHash         string         `json:"commit_hash"`
	OSName             string         `json:"os_name"`
	OSVersion          string         `json:"os_version"`
	ReportCreatedAt    string         `json:"report_created_at"`
	Command            Command        `json:"command"`
	Profile            config.Profile `json:"profile"`
	Errors             *string        `json:"errors"`
	ExecutionTimestamp string         `json:"execution_timestamp"`
}

// About struct
type About struct {
	Version         string    `json:"version"`
	UIVersion       string    `json:"uiVersion"`
	Commit          string    `json:"commit"`
	DeploymentID    string    `json:"deploymentId"`
	UpdateAvailable bool      `json:"updateAvailable"`
	LatestVersion   string    `json:"latestVersion"`
	LLMActive       bool      `json:"llmActive"`
	LLMProvider     string    `json:"llmProvider"`
	OIDCActive      bool      `json:"oidcActive"`
	License         string    `json:"license"`
	Mode            string    `json:"mode"`
	Staging         string    `json:"staging"`
	HotTier         string    `json:"hotTier"`
	GRPCPort        int       `json:"grpcPort"`
	Store           Store     `json:"store"`
	Analytics       Analytics `json:"analytics"`
	QueryEngine     string    `json:"queryEngine"`
}

// Store struct
type Store struct {
	Type string `json:"type"`
	Path string `json:"path"`
}

// Analytics struct
type Analytics struct {
	ClarityTag string `json:"clarityTag"`
}

type Command struct {
	Name      string            `json:"name"`
	Arguments []string          `json:"arguments"`
	Flags     map[string]string `json:"flags"`
}

// Config struct for parsing YAML
type Config struct {
	ULID string `yaml:"ulid"`
}

// CheckAndCreateULID checks for a ULID in the config file and creates it if absent.
func CheckAndCreateULID(_ *cobra.Command, _ []string) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		fmt.Printf("could not find home directory: %v\n", err)
		return err
	}

	configPath := filepath.Join(homeDir, ".parseable", "config.yaml")

	// Check if config path exists
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		// Create the directory if needed
		if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
			fmt.Printf("could not create config directory: %v\n", err)
			return err
		}
	}

	// Read the config file
	var config Config
	data, err := os.ReadFile(configPath)
	if err == nil {
		// If the file exists, unmarshal the content
		if err := yaml.Unmarshal(data, &config); err != nil {
			fmt.Printf("could not parse config file: %v\n", err)
			return err
		}
	}

	// Check if ULID is missing
	if config.ULID == "" {
		// Generate a new ULID
		entropy := ulid.Monotonic(rand.New(rand.NewSource(time.Now().UnixNano())), 0)
		ulidInstance := ulid.MustNew(ulid.Timestamp(time.Now()), entropy)
		config.ULID = ulidInstance.String()

		newData, err := yaml.Marshal(&config)
		if err != nil {
			fmt.Printf("could not marshal config data: %v\n", err)
			return err
		}

		// Write updated config with ULID back to the file
		if err := os.WriteFile(configPath, newData, 0644); err != nil {
			fmt.Printf("could not write to config file: %v\n", err)
			return err
		}
		fmt.Printf("Generated and saved new ULID: %s\n", config.ULID)
	}

	return nil
}

func PostRunAnalytics(cmd *cobra.Command, name string, args []string) {
	executionTime := cmd.Annotations["executionTime"]
	commandError := cmd.Annotations["error"]
	flags := make(map[string]string)
	cmd.Flags().VisitAll(func(flag *pflag.Flag) {
		flags[flag.Name] = flag.Value.String()
	})

	// Call SendEvent in PostRunE
	err := sendEvent(
		name,
		append(args, cmd.Name()),
		&commandError, // Pass the error here if there was one
		executionTime,
		flags,
	)
	if err != nil {
		fmt.Println("Error sending analytics event:", err)
	}

}

// sendEvent is a placeholder function to simulate sending an event after command execution.
func sendEvent(commandName string, arguments []string, errors *string, executionTimestamp string, flags map[string]string) error {
	ulid, err := ReadUULD()
	if err != nil {
		return fmt.Errorf("could not load ULID: %v", err)
	}

	profile, err := GetProfile()
	if err != nil {
		return fmt.Errorf("failed to get profile: %v", err)
	}

	httpClient := internalHTTP.DefaultClient(&profile)

	about, err := FetchAbout(&httpClient)
	if err != nil {
		return fmt.Errorf("failed to get about metadata for profile: %v", err)
	}

	// Create the Command struct
	cmd := Command{
		Name:      commandName,
		Arguments: arguments,
		Flags:     flags,
	}

	// Populate the Event struct with OS details and timestamp
	event := Event{
		CLIVersion:         about.Commit,
		ULID:               ulid,
		CommitHash:         about.Commit,
		OSName:             GetOSName(),
		OSVersion:          GetOSVersion(),
		ReportCreatedAt:    GetCurrentTimestamp(),
		Command:            cmd,
		Errors:             errors,
		ExecutionTimestamp: executionTimestamp,
	}

	event.Profile.Password = ""

	// Marshal the event to JSON for sending
	eventJSON, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event JSON: %v", err)
	}

	// Define the target URL for the HTTP request
	url := "https://analytics.parseable.io:80"

	// Create the HTTP POST request
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(eventJSON))
	if err != nil {
		return fmt.Errorf("failed to create HTTP request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-P-Stream", "pb-usage")

	// Execute the HTTP request
	resp, err := httpClient.Client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send event: %v", err)
	}
	defer resp.Body.Close()

	// Check for a non-2xx status code
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("received non-2xx response: %v", resp.Status)
	}

	//fmt.Println("Event sent successfully:", string(eventJSON))
	return nil
}

// GetOSName retrieves the OS name.
func GetOSName() string {
	switch runtime.GOOS {
	case "windows":
		return "Windows"
	case "darwin":
		return "macOS"
	case "linux":
		return getLinuxDistro()
	default:
		return "Unknown"
	}
}

// GetOSVersion retrieves the OS version.
func GetOSVersion() string {
	switch runtime.GOOS {
	case "windows":
		return getWindowsVersion()
	case "darwin":
		return getMacOSVersion()
	case "linux":
		return getLinuxVersion()
	default:
		return "Unknown"
	}
}

// GetCurrentTimestamp returns the current timestamp in ISO 8601 format.
func GetCurrentTimestamp() string {
	return time.Now().Format(time.RFC3339)
}

// GetFormattedTimestamp formats a given time.Time in ISO 8601 format.
func GetFormattedTimestamp(t time.Time) string {
	return t.Format(time.RFC3339)
}

// getLinuxDistro retrieves the Linux distribution name.
func getLinuxDistro() string {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return "Linux"
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "NAME=") {
			return strings.Trim(line[5:], "\"")
		}
	}
	return "Linux"
}

// getLinuxVersion retrieves the Linux distribution version.
func getLinuxVersion() string {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return "Unknown"
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "VERSION_ID=") {
			return strings.Trim(line[11:], "\"")
		}
	}
	return "Unknown"
}

// getMacOSVersion retrieves the macOS version.
func getMacOSVersion() string {
	out, err := exec.Command("sw_vers", "-productVersion").Output()
	if err != nil {
		return "Unknown"
	}
	return strings.TrimSpace(string(out))
}

// getWindowsVersion retrieves the Windows version.
func getWindowsVersion() string {
	out, err := exec.Command("cmd", "ver").Output()
	if err != nil {
		return "Unknown"
	}
	return strings.TrimSpace(string(out))
}

func ReadUULD() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("could not find home directory: %v", err)
	}

	configPath := filepath.Join(homeDir, ".parseable", "config.yaml")

	// Check if config path exists
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return "", fmt.Errorf("config file does not exist, please run CheckAndCreateULID first")
	}

	// Read the config file
	var config Config
	data, err := os.ReadFile(configPath)
	if err != nil {
		return "", fmt.Errorf("could not read config file: %v", err)
	}

	// Unmarshal the content to get the ULID
	if err := yaml.Unmarshal(data, &config); err != nil {
		return "", fmt.Errorf("could not parse config file: %v", err)
	}

	if config.ULID == "" {
		return "", fmt.Errorf("ULID is missing in config file")
	}

	return config.ULID, nil
}

func FetchAbout(client *internalHTTP.HTTPClient) (about About, err error) {
	req, err := client.NewRequest("GET", "about", nil)
	if err != nil {
		return
	}

	resp, err := client.Client.Do(req)
	if err != nil {
		return
	}

	bytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == 200 {
		err = json.Unmarshal(bytes, &about)
	} else {
		body := string(bytes)
		body = fmt.Sprintf("Request Failed\nStatus Code: %s\nResponse: %s\n", resp.Status, body)
		err = errors.New(body)
	}
	return
}

func GetProfile() (config.Profile, error) {
	conf, err := config.ReadConfigFromFile()
	if os.IsNotExist(err) {
		return config.Profile{}, errors.New("no config found to run this command. add a profile using pb profile command")
	} else if err != nil {
		return config.Profile{}, err
	}

	if conf.Profiles == nil || conf.DefaultProfile == "" {
		return config.Profile{}, errors.New("no profile is configured to run this command. please create one using profile command")
	}

	return conf.Profiles[conf.DefaultProfile], nil

}
