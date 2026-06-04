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

func TestEnsureDefaultSQLLimitIgnoresLimitInComments(t *testing.T) {
	tests := map[string]string{
		`SELECT * FROM logs -- limit later`:         "SELECT * FROM logs -- limit later\nLIMIT 500",
		`SELECT * FROM logs /* limit later */`:      `SELECT * FROM logs /* limit later */ LIMIT 500`,
		`SELECT * FROM logs -- limit later` + "\n":  "SELECT * FROM logs -- limit later\nLIMIT 500",
		`SELECT * FROM logs WHERE msg = 'limit 1'`:  `SELECT * FROM logs WHERE msg = 'limit 1' LIMIT 500`,
		`SELECT * FROM logs WHERE "limit" = 'true'`: `SELECT * FROM logs WHERE "limit" = 'true' LIMIT 500`,
	}

	for query, want := range tests {
		if got := ensureDefaultSQLLimit(query); got != want {
			t.Fatalf("ensureDefaultSQLLimit(%q) = %q, want %q", query, got, want)
		}
	}
}

func TestEnsureDefaultSQLLimitKeepsRealLimitWithComments(t *testing.T) {
	tests := []string{
		`SELECT * FROM logs LIMIT 10 -- keep`,
		`SELECT * FROM logs /* comment */ LIMIT 10`,
	}

	for _, query := range tests {
		if got := ensureDefaultSQLLimit(query); got != query {
			t.Fatalf("ensureDefaultSQLLimit(%q) = %q, want unchanged", query, got)
		}
	}
}

func TestQuoteUnsafeSQLTableNamesQuotesHyphenatedDataset(t *testing.T) {
	query := `SELECT * FROM claudecode-logs LIMIT 100`
	want := `SELECT * FROM "claudecode-logs" LIMIT 100`

	if got := quoteUnsafeSQLTableNames(query); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestQuoteUnsafeSQLTableNamesLeavesUnderscoreDatasetAlone(t *testing.T) {
	query := `SELECT * FROM fly_logs LIMIT 100`

	if got := quoteUnsafeSQLTableNames(query); got != query {
		t.Fatalf("got %q, want %q", got, query)
	}
}

func TestQuoteUnsafeSQLTableNamesLeavesPlaceholderAndQuotedDatasetAlone(t *testing.T) {
	tests := []string{
		`SELECT * FROM dataset LIMIT 100`,
		`SELECT * FROM "claudecode-logs" LIMIT 100`,
	}

	for _, query := range tests {
		if got := quoteUnsafeSQLTableNames(query); got != query {
			t.Fatalf("got %q, want %q", got, query)
		}
	}
}

func TestQuoteUnsafeSQLTableNamesQuotesJoinAndOtherUnsafeNames(t *testing.T) {
	tests := map[string]string{
		`SELECT * FROM fly_logs JOIN claudecode-logs ON fly_logs.id = claudecode-logs.id`: `SELECT * FROM fly_logs JOIN "claudecode-logs" ON fly_logs.id = claudecode-logs.id`,
		`SELECT * FROM metrics.v1 LIMIT 100`:                                              `SELECT * FROM "metrics.v1" LIMIT 100`,
		`SELECT * FROM 123logs LIMIT 100`:                                                 `SELECT * FROM "123logs" LIMIT 100`,
		`SELECT * FROM (SELECT * FROM claudecode-logs LIMIT 10) logs`:                     `SELECT * FROM (SELECT * FROM "claudecode-logs" LIMIT 10) logs`,
	}

	for query, want := range tests {
		if got := quoteUnsafeSQLTableNames(query); got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	}
}

func TestQuoteUnsafeSQLFieldNamesQuotesDottedAndMixedCaseFields(t *testing.T) {
	query := `SELECT * FROM "astronomy-shop-logs" WHERE app.order.id > 100 AND StatusCode = 'FailedPrecondition' LIMIT 100`
	want := `SELECT * FROM "astronomy-shop-logs" WHERE "app.order.id" > 100 AND "StatusCode" = 'FailedPrecondition' LIMIT 100`

	if got := quoteUnsafeSQLFieldNames(query); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestNormalizeInteractiveSQLQuotesTableAndFields(t *testing.T) {
	query := "SELECT * FROM astronomy-shop-logs \nWHERE app.order.id > 100\nLIMIT 100;"
	want := "SELECT * FROM \"astronomy-shop-logs\" \nWHERE \"app.order.id\" > 100\nLIMIT 100;"

	got := quoteUnsafeSQLFieldNames(quoteUnsafeSQLTableNames(query))
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestQuoteUnsafeSQLFieldNamesLeavesKeywordsFunctionsStringsAndQuotedNamesAlone(t *testing.T) {
	query := `SELECT COUNT(StatusCode) FROM "astronomy-shop-logs" WHERE "app.order.id" = 'app.order.id' AND service.name = 'cart'`
	want := `SELECT COUNT("StatusCode") FROM "astronomy-shop-logs" WHERE "app.order.id" = 'app.order.id' AND "service.name" = 'cart'`

	if got := quoteUnsafeSQLFieldNames(query); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
