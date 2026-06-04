package cmd

import (
	"errors"
	"testing"
)

func TestStatusErrorMessageTreatsAuthFailuresAsCredentialErrors(t *testing.T) {
	input := errors.New("Request Failed\nStatus Code: 403 Forbidden\nResponse: You don't have permission")
	got := statusErrorMessage(input)
	want := "Authentication failed: invalid username/password or API key"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestStatusErrorMessageKeepsOtherErrors(t *testing.T) {
	input := errors.New("dial tcp: connection refused")
	got := statusErrorMessage(input)
	if got != input.Error() {
		t.Fatalf("got %q, want %q", got, input.Error())
	}
}
