package cmd

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestAgentManifestContainsOnlyReadOnlyCommands(t *testing.T) {
	manifest := newAgentManifest()
	if manifest.SchemaVersion != "1" || len(manifest.Commands) == 0 {
		t.Fatalf("unexpected manifest metadata: %+v", manifest)
	}

	seen := make(map[string]struct{}, len(manifest.Commands))
	for _, item := range manifest.Commands {
		if item.Mutates {
			t.Fatalf("mutating command included in agent catalog: %s", item.Command)
		}
		if !strings.Contains(item.Command, "-o json") {
			t.Fatalf("agent command does not request JSON: %s", item.Command)
		}
		if _, exists := seen[item.Command]; exists {
			t.Fatalf("duplicate agent command: %s", item.Command)
		}
		seen[item.Command] = struct{}{}
	}
	for _, command := range []string{"pb -o json", "pb help <command...> -o json", "pb dataset list -o json", "pb dataset schema <dataset> -o json", "pb sql list -o json", "pb promql active-queries -o json"} {
		if _, exists := seen[command]; !exists {
			t.Fatalf("read-only command missing from agent catalog: %s", command)
		}
	}
	var sqlCommand agentCommand
	for _, item := range manifest.Commands {
		if item.Command == "pb sql run \"<SELECT query>\" --from <time> --to <time> -o json" {
			sqlCommand = item
			break
		}
	}
	constraints := strings.Join(sqlCommand.Constraints, " ")
	if !strings.Contains(constraints, "SELECT-only") || !strings.Contains(constraints, "--save-as") {
		t.Fatalf("SQL agent safety constraints are incomplete: %v", sqlCommand.Constraints)
	}
}

func TestAgentCommandJSONDoesNotRequireProfile(t *testing.T) {
	if err := AgentCmd.Flags().Set("output", "json"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = AgentCmd.Flags().Set("output", "text")
		AgentCmd.SetOut(nil)
	})

	var output bytes.Buffer
	AgentCmd.SetOut(&output)
	if err := AgentCmd.RunE(AgentCmd, nil); err != nil {
		t.Fatal(err)
	}

	var manifest agentManifest
	if err := json.Unmarshal(output.Bytes(), &manifest); err != nil {
		t.Fatalf("invalid JSON output: %v\n%s", err, output.String())
	}
	if len(manifest.Commands) == 0 || manifest.Contract.SuccessOutput != "stdout" {
		t.Fatalf("incomplete agent manifest: %+v", manifest)
	}
}
