package ui

import (
	"strings"
	"testing"

	"github.com/alecthomas/chroma/v2"
)

func TestJSONHighlightKeepsSQLAndPromQLPalette(t *testing.T) {
	previous := Active
	SetActive(Dark)
	t.Cleanup(func() { SetActive(previous) })

	jsonStyle := buildPBStyle(true)
	queryStyle := buildPBStyle(false)
	if got, want := jsonStyle.Get(chroma.NameTag).Colour.String(), string(Dark.Accent); !strings.EqualFold(got, want) {
		t.Fatalf("JSON key color = %s, want %s", got, want)
	}
	for _, token := range []chroma.TokenType{chroma.LiteralStringDouble, chroma.LiteralNumberInteger, chroma.KeywordConstant} {
		if got, want := jsonStyle.Get(token).Colour.String(), string(Dark.Body); !strings.EqualFold(got, want) {
			t.Fatalf("JSON value color for %s = %s, want %s", token, got, want)
		}
	}
	if got, want := queryStyle.Get(chroma.LiteralStringDouble).Colour.String(), string(Dark.String); !strings.EqualFold(got, want) {
		t.Fatalf("SQL/PromQL string color = %s, want %s", got, want)
	}
	if got, want := queryStyle.Get(chroma.LiteralNumberInteger).Colour.String(), string(Dark.Number); !strings.EqualFold(got, want) {
		t.Fatalf("SQL/PromQL number color = %s, want %s", got, want)
	}
	if got, want := queryStyle.Get(chroma.Keyword).Colour.String(), string(Dark.Accent); !strings.EqualFold(got, want) {
		t.Fatalf("SQL/PromQL keyword color = %s, want %s", got, want)
	}
}
