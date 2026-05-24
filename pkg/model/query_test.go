package model

import "testing"

func TestResolveColumnPlaceholder(t *testing.T) {
	tests := []struct {
		name   string
		query  string
		column string
		want   string
	}{
		{
			name:   "standalone placeholder replaced",
			query:  "SELECT column FROM logs",
			column: "status",
			want:   `SELECT "status" FROM logs`,
		},
		{
			name:   "column_count not touched",
			query:  "SELECT column, column_count FROM logs",
			column: "status",
			want:   `SELECT "status", column_count FROM logs`,
		},
		{
			name:   "mycolumn prefix not touched",
			query:  "SELECT mycolumn FROM logs",
			column: "status",
			want:   "SELECT mycolumn FROM logs",
		},
		{
			name:   "multiple standalone occurrences all replaced",
			query:  "SELECT column FROM logs WHERE column IS NOT NULL",
			column: "status",
			want:   `SELECT "status" FROM logs WHERE "status" IS NOT NULL`,
		},
		{
			name:   "empty column returns query unchanged",
			query:  "SELECT column FROM logs",
			column: "",
			want:   "SELECT column FROM logs",
		},
		{
			name:   "column name with double quote is SQL-escaped",
			query:  "SELECT column FROM logs",
			column: `my"col`,
			want:   `SELECT "my""col" FROM logs`,
		},
		{
			name:   "no placeholder in query",
			query:  "SELECT status FROM logs",
			column: "status",
			want:   "SELECT status FROM logs",
		},
		{
			name:   "empty query",
			query:  "",
			column: "status",
			want:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveColumnPlaceholder(tt.query, tt.column)
			if got != tt.want {
				t.Errorf("\nquery:  %q\ncolumn: %q\ngot:    %q\nwant:   %q", tt.query, tt.column, got, tt.want)
			}
		})
	}
}
