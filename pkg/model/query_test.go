package model

import (
	"testing"
	"time"
)

func TestFormatSQLControlTime(t *testing.T) {
	ist := time.FixedZone("IST", 5*60*60+30*60)
	value := time.Date(2026, time.June, 2, 11, 25, 24, 0, ist)

	got := formatSQLControlTime(value, TimeDisplayLocal)
	want := "02 Jun 2026, 11:25 | UTC+05:30"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestFormatSQLControlTimeUTC(t *testing.T) {
	ist := time.FixedZone("IST", 5*60*60+30*60)
	value := time.Date(2026, time.June, 2, 11, 25, 24, 0, ist)

	got := formatSQLControlTime(value, TimeDisplayUTC)
	want := "02 Jun 2026, 05:55 | UTC"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestFormatResultTimeLabelUsesBrackets(t *testing.T) {
	if got := formatResultTimeLabel(TimeDisplayLocal); got != "[LOCAL]" {
		t.Fatalf("local label = %q, want [LOCAL]", got)
	}
	if got := formatResultTimeLabel(TimeDisplayUTC); got != "[UTC]" {
		t.Fatalf("utc label = %q, want [UTC]", got)
	}
}

func TestFormatTimestampToDisplayHMSConvertsUTCToLocal(t *testing.T) {
	ist := time.FixedZone("IST", 5*60*60+30*60)

	got := formatTimestampToDisplayHMS("2026-06-02T09:13:59Z", ist, TimeDisplayLocal)
	want := "14:43:59"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestFormatTimestampToDisplayHMSTreatsNoZoneAsUTC(t *testing.T) {
	ist := time.FixedZone("IST", 5*60*60+30*60)

	got := formatTimestampToDisplayHMS("2026-06-02T09:13:59", ist, TimeDisplayLocal)
	want := "14:43:59"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestFormatTimestampToDisplayHMSUTC(t *testing.T) {
	ist := time.FixedZone("IST", 5*60*60+30*60)

	got := formatTimestampToDisplayHMS("2026-06-02T09:13:59Z", ist, TimeDisplayUTC)
	want := "09:13:59"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestEnsureDefaultSQLLimitAddsLimit(t *testing.T) {
	query := `SELECT * FROM "astronomy-shop-logs"`
	want := `SELECT * FROM "astronomy-shop-logs" LIMIT 500`

	if got := ensureDefaultSQLLimit(query); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestEnsureDefaultSQLLimitKeepsExistingLimit(t *testing.T) {
	query := `SELECT * FROM "astronomy-shop-logs" LIMIT 10`

	if got := ensureDefaultSQLLimit(query); got != query {
		t.Fatalf("got %q, want %q", got, query)
	}
}
