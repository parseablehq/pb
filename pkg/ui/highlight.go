// Copyright (c) 2024 Parseable, Inc
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

package ui

import (
	"bytes"
	"fmt"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
)

// pbStyle is the chroma palette mapped onto our Palette tokens.
// Built lazily on first call so it picks up whichever theme is active.
var pbStyle *chroma.Style

func buildPBStyle() *chroma.Style {
	p := Active
	// Build entries — chroma uses token-type → style rules.
	// Color values must be hex strings; pull directly from palette.
	bg := fmt.Sprintf("bg:%s", p.EditorBg)
	entries := map[chroma.TokenType]string{
		chroma.Background:         fmt.Sprintf("%s %s", p.Body, bg),
		chroma.Keyword:            fmt.Sprintf("%s bold", p.Accent),
		chroma.KeywordReserved:    fmt.Sprintf("%s bold", p.Accent),
		chroma.KeywordType:        fmt.Sprintf("%s", p.Accent2),
		chroma.Name:               string(p.Body),
		chroma.NameFunction:       fmt.Sprintf("%s", p.Warn),
		chroma.NameBuiltin:        fmt.Sprintf("%s", p.Warn),
		chroma.LiteralString:      string(p.String),
		chroma.LiteralStringDouble: string(p.String),
		chroma.LiteralStringSingle: string(p.String),
		chroma.LiteralNumber:      string(p.Number),
		chroma.LiteralNumberInteger: string(p.Number),
		chroma.LiteralNumberFloat: string(p.Number),
		chroma.Comment:            fmt.Sprintf("italic %s", p.Faint),
		chroma.CommentSingle:      fmt.Sprintf("italic %s", p.Faint),
		chroma.Operator:           string(p.Mute),
		chroma.Punctuation:        string(p.Mute),
		chroma.Text:               string(p.Body),
	}
	builder := chroma.NewStyleBuilder("pb")
	for tok, entry := range entries {
		builder.Add(tok, entry)
	}
	s, err := builder.Build()
	if err != nil {
		return styles.Fallback
	}
	return s
}

// HighlightSQL returns the given SQL string colored using ANSI truecolor
// escapes, painted with the active pb palette. Output safe to render
// inside any lipgloss block — chroma only emits SGR sequences, no
// cursor moves or clears.
//
// Caller is responsible for sizing — chroma does no wrapping.
func HighlightSQL(src string) string {
	return highlight(src, "sql")
}

// HighlightPromQL highlights PromQL queries (chroma calls the lexer
// `promql`).
func HighlightPromQL(src string) string {
	return highlight(src, "promql")
}

func highlight(src, lang string) string {
	if src == "" {
		return ""
	}
	if pbStyle == nil {
		pbStyle = buildPBStyle()
	}
	lex := lexers.Get(lang)
	if lex == nil {
		lex = lexers.Fallback
	}
	lex = chroma.Coalesce(lex)

	iter, err := lex.Tokenise(nil, src)
	if err != nil {
		return src
	}
	var buf bytes.Buffer
	fmtter := formatters.Get("terminal16m")
	if fmtter == nil {
		fmtter = formatters.Fallback
	}
	if err := fmtter.Format(&buf, pbStyle, iter); err != nil {
		return src
	}
	return buf.String()
}
