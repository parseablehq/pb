package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
)

const testDatasetSchema = `{"fields":[{"name":"service_name","data_type":"Utf8","nullable":true},{"name":"timestamp","data_type":{"Timestamp":["Nanosecond",null]}}],"metadata":{"source":"test"}}`

func TestDatasetSchemaOutput(t *testing.T) {
	useCommandTransport(t, func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodGet || req.URL.Path != "/api/v1/logstream/events/schema" {
			t.Fatalf("unexpected request: %s %s", req.Method, req.URL.Path)
		}
		return commandHTTPResponse(http.StatusOK, "200 OK", testDatasetSchema), nil
	})

	for _, test := range []struct {
		format string
		want   string
	}{
		{format: "text", want: (&DatasetListItem{Name: "service_name", Type: "Utf8"}).Render() + "\n" +
			(&DatasetListItem{Name: "timestamp", Type: `{"Timestamp":["Nanosecond",null]}`}).Render() + "\n"},
		{format: "json"},
	} {
		t.Run(test.format, func(t *testing.T) {
			if err := SchemaDatasetCmd.Flags().Set("output", test.format); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = SchemaDatasetCmd.Flags().Set("output", "text") })
			var output bytes.Buffer
			SchemaDatasetCmd.SetOut(&output)
			t.Cleanup(func() { SchemaDatasetCmd.SetOut(nil) })

			if err := SchemaDatasetCmd.RunE(SchemaDatasetCmd, []string{"events"}); err != nil {
				t.Fatal(err)
			}
			if test.format == "text" {
				if got := output.String(); got != test.want {
					t.Fatalf("unexpected schema text: %q", got)
				}
				return
			}
			var got, want any
			if err := json.Unmarshal(output.Bytes(), &got); err != nil {
				t.Fatalf("invalid JSON output: %v", err)
			}
			if err := json.Unmarshal([]byte(testDatasetSchema), &want); err != nil {
				t.Fatal(err)
			}
			gotJSON, _ := json.Marshal(got)
			wantJSON, _ := json.Marshal(want)
			if !bytes.Equal(gotJSON, wantJSON) {
				t.Fatalf("schema JSON changed: got %s, want %s", gotJSON, wantJSON)
			}
		})
	}
}

func TestDatasetSchemaNotFound(t *testing.T) {
	useCommandTransport(t, func(*http.Request) (*http.Response, error) {
		return commandHTTPResponse(http.StatusNotFound, "404 Not Found", "missing dataset"), nil
	})
	var output bytes.Buffer
	SchemaDatasetCmd.SetOut(&output)
	t.Cleanup(func() { SchemaDatasetCmd.SetOut(nil) })
	if err := SchemaDatasetCmd.RunE(SchemaDatasetCmd, []string{"missing"}); err == nil || errorDetails(err).Code != ErrorNotFound {
		t.Fatalf("expected not-found error, got %v", err)
	}
	if output.Len() != 0 {
		t.Fatalf("unexpected output on error: %q", output.String())
	}
}
