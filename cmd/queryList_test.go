package cmd

import (
	"bytes"
	"io"
	"os"
	"testing"
)

func TestSavedQueryToPbQueryPrintsEmptyQueryMessage(t *testing.T) {
	var output bytes.Buffer
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe failed: %v", err)
	}
	os.Stdout = w

	err = savedQueryToPbQuery("   ", "", "")

	w.Close()
	os.Stdout = oldStdout
	io.Copy(&output, r)

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got, want := output.String(), "Empty query selected.\n"; got != want {
		t.Fatalf("unexpected output\nwant: %q\n got: %q", want, got)
	}
}
