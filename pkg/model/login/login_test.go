package login

import (
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
)

func TestAPIKeyInputIsMasked(t *testing.T) {
	model := New()
	if model.apiKeyInput.EchoMode != textinput.EchoPassword {
		t.Fatalf("API key echo mode=%v", model.apiKeyInput.EchoMode)
	}
	if model.apiKeyInput.EchoCharacter != '•' {
		t.Fatalf("API key echo character=%q", model.apiKeyInput.EchoCharacter)
	}
}
