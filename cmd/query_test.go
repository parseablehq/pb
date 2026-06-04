package cmd

import "testing"

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
