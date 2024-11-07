package analytics

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"pb/pkg/config"
	internalHTTP "pb/pkg/http"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"gopkg.in/yaml.v2"
)

type Event struct {
	CLIVersion      string         `json:"cli_version"`
	UUID            string         `json:"uuid"`
	CommitHash      string         `json:"commit_hash"`
	OSName          string         `json:"os_name"`
	OSVersion       string         `json:"os_version"`
	ReportCreatedAt string         `json:"report_created_at"`
	Command         Command        `json:"command"`
	Profile         config.Profile `json:"profile"`
	Errors          *string        `json:"errors"`
	ExecutionStatus string         `json:"execution_status"`
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
	UUID string `yaml:"uuid"`
}

// CheckAndCreateUUID checks for a UUID in the config file and creates it if absent.
func CheckAndCreateUUID(_ *cobra.Command, _ []string) error {
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

	// Check if UUID is missing
	if config.UUID == "" {
		config.UUID = uuid.New().String() // Generate a new UUID
		newData, err := yaml.Marshal(&config)
		if err != nil {
			fmt.Printf("could not marshal config data: %v\n", err)
			return err
		}

		// Write updated config with UUID back to the file
		if err := os.WriteFile(configPath, newData, 0644); err != nil {
			fmt.Printf("could not write to config file: %v\n", err)
			return err
		}
		fmt.Printf("Generated and saved new UUID: %s\n", config.UUID)
	}

	return nil
}

func PostRunAnalytics(cmd *cobra.Command, args []string) {
	executionTime := cmd.Annotations["executionTime"]
	commandError := cmd.Annotations["error"]
	flags := make(map[string]string)
	cmd.Flags().VisitAll(func(flag *pflag.Flag) {
		flags[flag.Name] = flag.Value.String()
	})
	// Call SendEvent in PostRunE
	err := sendEvent(
		cmd.Name(),
		args,
		&commandError, // Pass the error here if there was one
		executionTime,
		flags,
	)
	if err != nil {
		fmt.Println("Error sending analytics event:", err)
	}

}

// sendEvent is a placeholder function to simulate sending an event after command execution.
func sendEvent(commandName string, arguments []string, errors *string, executionStatus string, flags map[string]string) error {

	uuid, err := ReadUUID()
	if err != nil {
		return fmt.Errorf("could not load UUID: %v", err)
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
		CLIVersion:      about.Commit,
		UUID:            uuid,
		CommitHash:      about.Commit,
		Profile:         profile,
		OSName:          GetOSName(),
		OSVersion:       GetOSVersion(),
		ReportCreatedAt: GetCurrentTimestamp(),
		Command:         cmd,
		Errors:          errors,
		ExecutionStatus: executionStatus,
	}

	// Marshal the event to JSON for sending
	eventJSON, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event JSON: %v", err)
	}

	// Simulate sending the event (print or make an HTTP request)
	fmt.Println("Sending event:", string(eventJSON))

	// err = sendHTTPRequest("POST", "https://example.com/events", eventJSON)
	// if err != nil {
	//     return fmt.Errorf("failed to send event: %v", err)
	// }

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

func ReadUUID() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("could not find home directory: %v", err)
	}

	configPath := filepath.Join(homeDir, ".parseable", "config.yaml")

	// Check if config path exists
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return "", fmt.Errorf("config file does not exist, please run CheckAndCreateUUID first")
	}

	// Read the config file
	var config Config
	data, err := os.ReadFile(configPath)
	if err != nil {
		return "", fmt.Errorf("could not read config file: %v", err)
	}

	// Unmarshal the content to get the UUID
	if err := yaml.Unmarshal(data, &config); err != nil {
		return "", fmt.Errorf("could not parse config file: %v", err)
	}

	if config.UUID == "" {
		return "", fmt.Errorf("UUID is missing in config file")
	}

	return config.UUID, nil
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
