package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

type agentManifest struct {
	SchemaVersion string           `json:"schema_version"`
	Catalog       string           `json:"catalog"`
	Authorization string           `json:"authorization"`
	Contract      agentContract    `json:"contract"`
	ErrorCodes    []agentErrorCode `json:"error_codes"`
	Commands      []agentCommand   `json:"commands"`
	Excluded      string           `json:"excluded"`
}

type agentContract struct {
	SuccessOutput string   `json:"success_output"`
	ErrorOutput   string   `json:"error_output"`
	Diagnostics   string   `json:"diagnostics"`
	FailureExit   string   `json:"failure_exit"`
	EmptyLists    string   `json:"empty_lists"`
	SpecialCases  []string `json:"special_cases,omitempty"`
}

type agentErrorCode struct {
	Code      ErrorCode `json:"code"`
	Retryable bool      `json:"retryable"`
}

type agentCommand struct {
	Command         string   `json:"command"`
	Description     string   `json:"description"`
	Scope           string   `json:"scope"`
	Mutates         bool     `json:"mutates"`
	RequiresProfile bool     `json:"requires_profile"`
	Streaming       bool     `json:"streaming,omitempty"`
	Constraints     []string `json:"constraints,omitempty"`
}

// AgentCmd describes the machine-readable, read-only CLI surface available to agents.
var AgentCmd = &cobra.Command{
	Use:     "agent",
	Short:   "Show the read-only command catalog for agents",
	Example: "  pb agent -o json",
	Args:    cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		format, err := commandOutputFormat(cmd)
		if err != nil {
			return err
		}
		manifest := newAgentManifest()
		if format == outputJSON {
			return writeJSON(cmd.OutOrStdout(), manifest)
		}

		out := cmd.OutOrStdout()
		fmt.Fprintln(out, "Read-only command catalog for agents")
		fmt.Fprintln(out, manifest.Authorization)
		for _, item := range manifest.Commands {
			fmt.Fprintf(out, "- %s: %s\n", item.Command, item.Description)
		}
		return nil
	},
}

func init() {
	AgentCmd.Flags().StringP("output", "o", "text", "Output format (text|json)")
}

func newAgentManifest() agentManifest {
	return agentManifest{
		SchemaVersion: "1",
		Catalog:       "cli-declared read-only commands",
		Authorization: "The server still enforces the active profile's permissions; inclusion here does not guarantee authorization.",
		Contract: agentContract{
			SuccessOutput: "stdout",
			ErrorOutput:   "stderr as JSON when -o json is explicit",
			Diagnostics:   "stderr",
			FailureExit:   "non-zero",
			EmptyLists:    "[]",
			SpecialCases: []string{
				"pb status -o json reports an unhealthy status as JSON on stdout and exits non-zero",
			},
		},
		ErrorCodes: []agentErrorCode{
			{Code: ErrorInvalidInput, Retryable: false},
			{Code: ErrorAuthFailed, Retryable: false},
			{Code: ErrorPermissionDenied, Retryable: false},
			{Code: ErrorNotFound, Retryable: false},
			{Code: ErrorConflict, Retryable: false},
			{Code: ErrorRateLimited, Retryable: true},
			{Code: ErrorConnectionFailed, Retryable: true},
			{Code: ErrorTimeout, Retryable: true},
			{Code: ErrorInvalidResponse, Retryable: false},
			{Code: ErrorServer, Retryable: true},
			{Code: ErrorCommandFailed, Retryable: false},
		},
		Commands: []agentCommand{
			{Command: "pb agent -o json", Description: "Discover this agent contract and command catalog", Scope: "local", Mutates: false, RequiresProfile: false},
			{Command: "pb -o json", Description: "Discover the complete reachable CLI command tree and flags", Scope: "local", Mutates: false, RequiresProfile: false},
			{Command: "pb help <command...> -o json", Description: "Inspect one command and its flags or subcommands", Scope: "local", Mutates: false, RequiresProfile: false},
			{Command: "pb status -o json", Description: "Check the active profile and server connection", Scope: "local+server", Mutates: false, RequiresProfile: false},
			{Command: "pb version -o json", Description: "Show the local client version. For server and connection details, use pb status -o json", Scope: "local", Mutates: false, RequiresProfile: false},
			{Command: "pb profile list -o json", Description: "List locally configured profiles without credentials", Scope: "local", Mutates: false, RequiresProfile: false, Constraints: []string{"Requires the local pb config file"}},
			{Command: "pb dataset list -o json", Description: "List datasets", Scope: "server", Mutates: false, RequiresProfile: true},
			{Command: "pb dataset info <dataset> -o json", Description: "Read dataset statistics and configuration", Scope: "server", Mutates: false, RequiresProfile: true},
			{Command: "pb user list -o json", Description: "List users and their roles", Scope: "server", Mutates: false, RequiresProfile: true},
			{Command: "pb role list -o json", Description: "List roles and privileges", Scope: "server", Mutates: false, RequiresProfile: true},
			{Command: "pb sql run \"<SELECT query>\" --from <time> --to <time> -o json", Description: "Run a read-only SQL query", Scope: "server", Mutates: false, RequiresProfile: true, Constraints: []string{"Use SELECT-only SQL", "Never use --save-as because it creates a saved query"}},
			{Command: "pb sql list -o json", Description: "List saved SQL queries without opening the TUI", Scope: "server", Mutates: false, RequiresProfile: true},
			{Command: "pb promql run \"<expression>\" --dataset <dataset> -o json", Description: "Run a PromQL query", Scope: "server", Mutates: false, RequiresProfile: true},
			{Command: "pb promql labels <dataset> -o json", Description: "List PromQL label names", Scope: "server", Mutates: false, RequiresProfile: true},
			{Command: "pb promql label-values <label> <dataset> -o json", Description: "List values for a PromQL label", Scope: "server", Mutates: false, RequiresProfile: true},
			{Command: "pb promql series <dataset> --match <selector> -o json", Description: "List matching PromQL series", Scope: "server", Mutates: false, RequiresProfile: true},
			{Command: "pb promql cardinality label-names <dataset> -o json", Description: "Inspect label-name cardinality", Scope: "server", Mutates: false, RequiresProfile: true},
			{Command: "pb promql cardinality label-values <dataset> <label> -o json", Description: "Inspect label-value cardinality", Scope: "server", Mutates: false, RequiresProfile: true},
			{Command: "pb promql cardinality active-series <dataset> <selector> -o json", Description: "Inspect active-series cardinality", Scope: "server", Mutates: false, RequiresProfile: true},
			{Command: "pb promql active-queries -o json", Description: "List active PromQL queries", Scope: "server", Mutates: false, RequiresProfile: true},
			{Command: "pb promql tsdb <dataset> -o json", Description: "Read PromQL TSDB statistics", Scope: "server", Mutates: false, RequiresProfile: true},
			{Command: "pb tail <dataset> -o json", Description: "Stream live dataset events until interrupted", Scope: "server", Mutates: false, RequiresProfile: true, Streaming: true, Constraints: []string{"Long-running command; stop with SIGINT"}},
		},
		Excluded: "Commands that add, update, save, set, remove, delete, install, or uninstall resources are intentionally omitted.",
	}
}
