package cmd

import (
	"bufio"
	"bytes"
	"strings"
	"testing"
)

func normalizeSQLQueryForTest(query string) string {
	query = quoteStreamNames(query)
	query = quoteFieldsWithDots(query)
	return query
}

func TestNormalizeSQLQueryQuotesShellStrippedIdentifiers(t *testing.T) {
	input := "SELECT * FROM astronomy-shop-logs WHERE StatusCode = 'FailedPrecondition' AND service.name = 'cart' LIMIT 500"
	want := `SELECT * FROM "astronomy-shop-logs" WHERE "StatusCode" = 'FailedPrecondition' AND "service.name" = 'cart' LIMIT 500`

	if got := normalizeSQLQueryForTest(input); got != want {
		t.Fatalf("normalized query mismatch\nwant: %s\n got: %s", want, got)
	}
}

func TestNormalizeSQLQueryLeavesUnderscoreDatasetNameAlone(t *testing.T) {
	input := `SELECT * FROM fly_logs LIMIT 100`

	if got := normalizeSQLQueryForTest(input); got != input {
		t.Fatalf("underscore dataset changed\nwant: %s\n got: %s", input, got)
	}
}

func TestNormalizeSQLQueryQuotesUnsafeJoinDatasetName(t *testing.T) {
	input := `SELECT * FROM fly_logs JOIN claudecode-logs ON fly_logs.id = claudecode-logs.id`
	want := `SELECT * FROM fly_logs JOIN "claudecode-logs" ON fly_logs.id = claudecode-logs.id`

	if got := quoteStreamNames(input); got != want {
		t.Fatalf("normalized query mismatch\nwant: %s\n got: %s", want, got)
	}
}

func TestNormalizeSQLQueryQuotesDatasetNamesWithDotsOrLeadingDigits(t *testing.T) {
	tests := map[string]string{
		`SELECT * FROM metrics.v1 LIMIT 100`: `SELECT * FROM "metrics.v1" LIMIT 100`,
		`SELECT * FROM 123logs LIMIT 100`:    `SELECT * FROM "123logs" LIMIT 100`,
	}

	for input, want := range tests {
		if got := quoteStreamNames(input); got != want {
			t.Fatalf("quoteStreamNames(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestNormalizeSQLQueryLeavesQuotedIdentifiersAndStringsAlone(t *testing.T) {
	input := `SELECT * FROM "astronomy-shop-logs" WHERE "StatusCode" = 'FailedPrecondition' AND "service.name" = 'cart'`

	if got := normalizeSQLQueryForTest(input); got != input {
		t.Fatalf("quoted query changed\nwant: %s\n got: %s", input, got)
	}
}

func TestNormalizeSQLQueryDoesNotQuoteKeywordsOrFunctionNames(t *testing.T) {
	input := "SELECT COUNT(StatusCode) FROM astronomy-shop-logs WHERE StatusCode IS NOT NULL GROUP BY service.name LIMIT 10"
	want := `SELECT COUNT("StatusCode") FROM "astronomy-shop-logs" WHERE "StatusCode" IS NOT NULL GROUP BY "service.name" LIMIT 10`

	if got := normalizeSQLQueryForTest(input); got != want {
		t.Fatalf("normalized query mismatch\nwant: %s\n got: %s", want, got)
	}
}

func TestDefaultSavedQueryNameUsesStreamName(t *testing.T) {
	query := `SELECT * FROM "astronomy-shop-logs" LIMIT 500`

	if got := defaultSavedQueryName(query); got != "astronomy-shop-logs" {
		t.Fatalf("got %q, want astronomy-shop-logs", got)
	}
}

func TestDefaultSavedQueryNameFallback(t *testing.T) {
	query := `SELECT 1`

	if got := defaultSavedQueryName(query); got != "saved-query" {
		t.Fatalf("got %q, want saved-query", got)
	}
}

func TestEnsureDefaultLimitAddsLimit(t *testing.T) {
	query := `SELECT * FROM "astronomy-shop-logs"`
	want := `SELECT * FROM "astronomy-shop-logs" LIMIT 500`

	if got := ensureDefaultLimit(query); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestEnsureDefaultLimitKeepsExistingTopLevelLimit(t *testing.T) {
	query := `SELECT * FROM "astronomy-shop-logs" LIMIT 10`

	if got := ensureDefaultLimit(query); got != query {
		t.Fatalf("got %q, want %q", got, query)
	}
}

func TestEnsureDefaultLimitIgnoresSubqueryLimit(t *testing.T) {
	query := `SELECT * FROM (SELECT * FROM "astronomy-shop-logs" LIMIT 10) logs`
	want := `SELECT * FROM (SELECT * FROM "astronomy-shop-logs" LIMIT 10) logs LIMIT 500`

	if got := ensureDefaultLimit(query); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestEnsureDefaultLimitKeepsTrailingSemicolon(t *testing.T) {
	query := `SELECT * FROM "astronomy-shop-logs";`
	want := `SELECT * FROM "astronomy-shop-logs" LIMIT 500;`

	if got := ensureDefaultLimit(query); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestEnsureDefaultLimitIgnoresLimitInComments(t *testing.T) {
	tests := map[string]string{
		`SELECT * FROM logs -- limit later`:         "SELECT * FROM logs -- limit later\nLIMIT 500",
		`SELECT * FROM logs /* limit later */`:      `SELECT * FROM logs /* limit later */ LIMIT 500`,
		`SELECT * FROM logs -- limit later` + "\n":  "SELECT * FROM logs -- limit later\nLIMIT 500",
		`SELECT * FROM logs WHERE msg = 'limit 1'`:  `SELECT * FROM logs WHERE msg = 'limit 1' LIMIT 500`,
		`SELECT * FROM logs WHERE "limit" = 'true'`: `SELECT * FROM logs WHERE "limit" = 'true' LIMIT 500`,
	}

	for query, want := range tests {
		if got := ensureDefaultLimit(query); got != want {
			t.Fatalf("ensureDefaultLimit(%q) = %q, want %q", query, got, want)
		}
	}
}

func TestEnsureDefaultLimitKeepsRealLimitWithComments(t *testing.T) {
	tests := []string{
		`SELECT * FROM logs LIMIT 10 -- keep`,
		`SELECT * FROM logs /* comment */ LIMIT 10`,
	}

	for _, query := range tests {
		if got := ensureDefaultLimit(query); got != query {
			t.Fatalf("ensureDefaultLimit(%q) = %q, want unchanged", query, got)
		}
	}
}

func TestStreamSQLTextResponseStreamsBodyAndAddsTrailingNewline(t *testing.T) {
	var output bytes.Buffer
	reader := bufio.NewReader(strings.NewReader(`[{"a":1}]`))

	if err := streamSQLTextResponse(&output, reader, "200 OK"); err != nil {
		t.Fatalf("streamSQLTextResponse failed: %v", err)
	}

	if got, want := output.String(), "[{\"a\":1}]\n"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestStreamSQLTextResponsePrintsNoRowsForEmptyArray(t *testing.T) {
	var output bytes.Buffer
	reader := bufio.NewReader(strings.NewReader("  []\n"))

	if err := streamSQLTextResponse(&output, reader, "200 OK"); err != nil {
		t.Fatalf("streamSQLTextResponse failed: %v", err)
	}

	if got, want := output.String(), "Query succeeded: no rows returned (status: 200 OK).\n"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestStreamSQLJSONResponsePrettyPrintsArray(t *testing.T) {
	var output bytes.Buffer
	reader := bufio.NewReader(strings.NewReader(`[{"b":2},{"a":1}]`))

	if err := streamSQLJSONResponse(&output, reader, "200 OK"); err != nil {
		t.Fatalf("streamSQLJSONResponse failed: %v", err)
	}

	want := "[\n  {\n    \"b\": 2\n  },\n  {\n    \"a\": 1\n  }\n]\n"
	if got := output.String(); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestStreamSQLJSONResponsePrintsEmptyArray(t *testing.T) {
	var output bytes.Buffer
	reader := bufio.NewReader(strings.NewReader(`[]`))

	if err := streamSQLJSONResponse(&output, reader, "200 OK"); err != nil {
		t.Fatalf("streamSQLJSONResponse failed: %v", err)
	}

	if got, want := output.String(), "[]\n"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestReadLimitedErrorPreviewTruncatesLargeBody(t *testing.T) {
	body := strings.NewReader(strings.Repeat("x", queryErrorPreviewLimit+10))

	preview, err := readLimitedErrorPreview(body)
	if err != nil {
		t.Fatalf("readLimitedErrorPreview failed: %v", err)
	}

	if len(preview) != queryErrorPreviewLimit+len("...") {
		t.Fatalf("preview length = %d, want %d", len(preview), queryErrorPreviewLimit+len("..."))
	}
	if !strings.HasSuffix(preview, "...") {
		t.Fatalf("preview should indicate truncation")
	}
}
