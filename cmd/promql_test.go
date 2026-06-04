package cmd

import "testing"

func TestPromqlInteractiveFromDefaultUsesTenMinutes(t *testing.T) {
	got := promqlInteractiveFromDefault("5m", true, false)
	if got != "10m" {
		t.Fatalf("got %q, want 10m", got)
	}
}

func TestPromqlInteractiveFromDefaultKeepsExplicitFrom(t *testing.T) {
	got := promqlInteractiveFromDefault("1h", true, true)
	if got != "1h" {
		t.Fatalf("got %q, want 1h", got)
	}
}

func TestPromqlInteractiveFromDefaultKeepsNonInteractiveDefault(t *testing.T) {
	got := promqlInteractiveFromDefault("5m", false, false)
	if got != "5m" {
		t.Fatalf("got %q, want 5m", got)
	}
}
