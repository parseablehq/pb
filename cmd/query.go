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

package cmd

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/parseablehq/pb/pkg/model"

	internalHTTP "github.com/parseablehq/pb/pkg/http"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/spf13/cobra"
)

var (
	startFlag      = "from"
	startFlagShort = "f"
	defaultStart   = "1m"

	endFlag      = "to"
	endFlagShort = "t"
	defaultEnd   = "now"

	outputFlag  = "output"
	saveAsName  string
	sqlSaveName string
)

var query = &cobra.Command{
	Use:          "run [query] [flags]",
	Example:      "  pb sql run \"select * from frontend\" --from=10m --to=now\n  pb sql run \"select * from frontend\" -i",
	Short:        "Run SQL query on a dataset",
	Long:         "\nRun SQL query on a dataset. Default output format is text.\nUse --output json for JSON output, or -i for interactive table view.",
	Args:         cobra.MaximumNArgs(1),
	SilenceUsage: true,
	PreRunE:      PreRunDefaultProfile,
	RunE: func(command *cobra.Command, args []string) error {
		startTime := time.Now()
		command.Annotations = map[string]string{
			"startTime": startTime.Format(time.RFC3339),
		}

		defer func() {
			duration := time.Since(startTime)
			command.Annotations["executionTime"] = duration.String()
		}()

		interactive, err := command.Flags().GetBool("interactive")
		if err != nil {
			command.Annotations["error"] = err.Error()
			return err
		}

		if (len(args) == 0 || strings.TrimSpace(args[0]) == "") && !interactive {
			fmt.Println("Please enter your query")
			fmt.Printf("Example:\n  pb sql run \"select * from frontend\" --from=10m --to=now\n")
			return nil
		}

		var sqlQuery string
		if len(args) > 0 {
			sqlQuery = args[0]
		}

		start, err := command.Flags().GetString(startFlag)
		if err != nil {
			command.Annotations["error"] = err.Error()
			return err
		}
		if interactive && !command.Flags().Changed(startFlag) {
			start = "1h"
		} else if start == "" {
			start = defaultStart
		}

		end, err := command.Flags().GetString(endFlag)
		if err != nil {
			command.Annotations["error"] = err.Error()
			return err
		}
		if end == "" {
			end = defaultEnd
		}

		sqlQuery = quoteStreamNames(sqlQuery)
		sqlQuery = quoteFieldsWithDots(sqlQuery)
		sqlQuery = ensureDefaultLimit(sqlQuery)

		if interactive {
			startT, err := parseTimeStr(start)
			if err != nil {
				return fmt.Errorf("invalid --from value: %w", err)
			}
			endT, err := parseTimeStr(end)
			if err != nil {
				return fmt.Errorf("invalid --to value: %w", err)
			}
			m := model.NewQueryModel(DefaultProfile, sqlQuery, startT, endT)
			p := tea.NewProgram(m, tea.WithAltScreen())
			_, err = p.Run()
			if err != nil {
				command.Annotations["error"] = err.Error()
			}
			return err
		}

		outputFmt, err := command.Flags().GetString("output")
		if err != nil {
			command.Annotations["error"] = err.Error()
			return fmt.Errorf("failed to get 'output' flag: %w", err)
		}

		client := internalHTTP.DefaultClient(&DefaultProfile)
		err = fetchData(&client, sqlQuery, start, end, outputFmt)
		if err != nil {
			command.Annotations["error"] = err.Error()
			return err
		}

		if saveAsName != "" {
			if saveErr := saveFilter(&client, sqlQuery, saveAsName, start, end); saveErr != nil {
				fmt.Fprintf(os.Stderr, "warning: could not save query: %v\n", saveErr)
			} else {
				fmt.Fprintf(os.Stderr, "Query saved as '%s'\n", saveAsName)
			}
		}
		return nil
	},
}

func init() {
	query.Flags().StringP(startFlag, startFlagShort, defaultStart, "Start time for query.")
	query.Flags().StringP(endFlag, endFlagShort, defaultEnd, "End time for query.")
	query.Flags().StringVarP(&outputFormat, "output", "o", "", "Output format (text|json)")
	query.Flags().BoolP("interactive", "i", false, "Open interactive table view")
	query.Flags().StringVar(&saveAsName, "save-as", "", "Save this query with a name for later use")
}

// parseTimeStr converts a CLI time string to time.Time.
// Accepts: "now", RFC3339 ("2024-01-01T00:00:00Z"), Go durations ("10m", "2h"), or day suffix ("1d", "7d").
func parseTimeStr(s string) (time.Time, error) {
	if s == "now" {
		return time.Now(), nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	if strings.HasSuffix(s, "d") {
		n, err := strconv.Atoi(strings.TrimSuffix(s, "d"))
		if err == nil {
			return time.Now().Add(-time.Duration(n) * 24 * time.Hour), nil
		}
	}
	if d, err := time.ParseDuration(s); err == nil {
		return time.Now().Add(-d), nil
	}
	return time.Time{}, fmt.Errorf("unrecognized time format %q (use: now, 10m, 2h, 1d, or RFC3339)", s)
}

// startSpinner prints an animated spinner to stderr while a fetch is in progress.
// Call the returned function to stop it and clear the line.
func startSpinner() func() {
	frames := []string{"|", "/", "-", "\\"}
	done := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		i := 0
		for {
			select {
			case <-done:
				fmt.Fprint(os.Stderr, "\r\033[K")
				return
			default:
				fmt.Fprintf(os.Stderr, "\r%s fetching...", frames[i%len(frames)])
				i++
				time.Sleep(100 * time.Millisecond)
			}
		}
	}()
	return func() {
		close(done)
		<-stopped // wait for goroutine to clear the line before caller prints output
	}
}

// quoteStreamNames wraps table names that are not plain SQL identifiers in
// double quotes so DataFusion does not treat names like nginx-logs as an
// expression. Already-quoted identifiers and the interactive dataset
// placeholder are left untouched.
func quoteStreamNames(query string) string {
	var result strings.Builder
	i, n := 0, len(query)
	for i < n {
		switch query[i] {
		case '\'':
			i = copySQLStringLiteral(&result, query, i)
		case '"':
			i = copySQLQuotedIdentifier(&result, query, i)
		default:
			if isSQLTableClauseAt(query, i) {
				j := i + sqlTableClauseLen(query, i)
				result.WriteString(query[i:j])
				i = j

				for i < n && isSQLSpace(query[i]) {
					result.WriteByte(query[i])
					i++
				}
				if i >= n {
					continue
				}
				if query[i] == '"' || query[i] == '\'' || query[i] == '(' {
					continue
				}

				start := i
				for i < n && !isSQLTableTokenEnd(query[i]) {
					i++
				}
				tableName := query[start:i]
				if shouldQuoteSQLTableName(tableName) {
					result.WriteByte('"')
					result.WriteString(strings.ReplaceAll(tableName, `"`, `""`))
					result.WriteByte('"')
				} else {
					result.WriteString(tableName)
				}
				continue
			}
			result.WriteByte(query[i])
			i++
		}
	}
	return result.String()
}

// quoteFieldsWithDots wraps unquoted field identifiers that DataFusion would
// otherwise reinterpret: dotted names become table.column references and
// mixed-case names are folded to lowercase.
// e.g. service.name → "service.name", StatusCode → "StatusCode"
// Already-quoted identifiers, string literals, SQL keywords, and function calls
// are left untouched.
func quoteFieldsWithDots(query string) string {
	var result strings.Builder
	i, n := 0, len(query)
	for i < n {
		ch := query[i]
		switch ch {
		case '\'':
			result.WriteByte(ch)
			i++
			for i < n {
				c := query[i]
				result.WriteByte(c)
				i++
				if c == '\'' {
					if i < n && query[i] == '\'' { // escaped '' inside string
						result.WriteByte(query[i])
						i++
					} else {
						break
					}
				}
			}
		case '"':
			result.WriteByte(ch)
			i++
			for i < n {
				c := query[i]
				result.WriteByte(c)
				i++
				if c == '"' {
					break
				}
			}
		default:
			if identStart(ch) {
				j := i + 1
				for j < n && identChar(query[j]) {
					j++
				}
				// walk dot-separated segments: a.b.c
				k, hasDot := j, false
				for k < n && query[k] == '.' && k+1 < n && identChar(query[k+1]) {
					hasDot = true
					k++
					for k < n && identChar(query[k]) {
						k++
					}
				}
				identifier := query[i:k]
				if hasDot || shouldQuoteIdentifier(identifier, query, k) {
					result.WriteByte('"')
					result.WriteString(identifier)
					result.WriteByte('"')
					i = k
				} else {
					result.WriteString(query[i:j])
					i = j
				}
			} else {
				result.WriteByte(ch)
				i++
			}
		}
	}
	return result.String()
}

var sqlKeywords = map[string]struct{}{
	"all": {}, "and": {}, "as": {}, "asc": {}, "between": {}, "by": {}, "case": {}, "cast": {},
	"desc": {}, "distinct": {}, "else": {}, "end": {}, "false": {}, "from": {}, "full": {},
	"group": {}, "having": {}, "in": {}, "inner": {}, "is": {}, "join": {}, "left": {}, "like": {},
	"limit": {}, "not": {}, "null": {}, "on": {}, "or": {}, "order": {}, "outer": {}, "right": {},
	"select": {}, "then": {}, "true": {}, "when": {}, "where": {},
}

func shouldQuoteIdentifier(identifier, query string, end int) bool {
	if _, ok := sqlKeywords[strings.ToLower(identifier)]; ok {
		return false
	}
	if nextNonSpace(query, end) == '(' {
		return false
	}
	return hasMixedCase(identifier)
}

func hasMixedCase(s string) bool {
	hasUpper, hasLower := false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			hasUpper = true
		}
		if c >= 'a' && c <= 'z' {
			hasLower = true
		}
	}
	return hasUpper && hasLower
}

func nextNonSpace(s string, start int) byte {
	for start < len(s) {
		switch s[start] {
		case ' ', '\t', '\n', '\r':
			start++
		default:
			return s[start]
		}
	}
	return 0
}

func identStart(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '_'
}

func identChar(c byte) bool {
	return identStart(c) || (c >= '0' && c <= '9')
}

func copySQLStringLiteral(result *strings.Builder, query string, start int) int {
	result.WriteByte(query[start])
	i := start + 1
	for i < len(query) {
		c := query[i]
		result.WriteByte(c)
		i++
		if c == '\'' {
			if i < len(query) && query[i] == '\'' {
				result.WriteByte(query[i])
				i++
				continue
			}
			break
		}
	}
	return i
}

func copySQLQuotedIdentifier(result *strings.Builder, query string, start int) int {
	result.WriteByte(query[start])
	i := start + 1
	for i < len(query) {
		c := query[i]
		result.WriteByte(c)
		i++
		if c == '"' {
			if i < len(query) && query[i] == '"' {
				result.WriteByte(query[i])
				i++
				continue
			}
			break
		}
	}
	return i
}

func isSQLTableClauseAt(query string, idx int) bool {
	return isSQLKeywordAt(query, idx, "from") || isSQLKeywordAt(query, idx, "join")
}

func sqlTableClauseLen(query string, idx int) int {
	if isSQLKeywordAt(query, idx, "from") {
		return len("from")
	}
	return len("join")
}

func isSQLKeywordAt(query string, idx int, keyword string) bool {
	if idx+len(keyword) > len(query) || !strings.EqualFold(query[idx:idx+len(keyword)], keyword) {
		return false
	}
	if idx > 0 && identChar(query[idx-1]) {
		return false
	}
	next := idx + len(keyword)
	return next >= len(query) || !identChar(query[next])
}

func isSQLSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}

func isSQLTableTokenEnd(c byte) bool {
	return isSQLSpace(c) || c == ',' || c == ';' || c == '(' || c == ')'
}

func shouldQuoteSQLTableName(tableName string) bool {
	if tableName == "" || strings.EqualFold(tableName, "dataset") {
		return false
	}
	if !identStart(tableName[0]) {
		return true
	}
	for i := 1; i < len(tableName); i++ {
		if !identChar(tableName[i]) {
			return true
		}
	}
	return false
}

var QueryCmd = query

var SaveSQLCmd = &cobra.Command{
	Use:          "save [query]",
	Example:      "  pb sql save 'select * from frontend'\n  pb sql save \"select * from frontend\" --name frontend-errors --from=1h --to=now",
	Short:        "Save SQL query",
	Long:         "\nSave a SQL query without running it.",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	PreRunE:      PreRunDefaultProfile,
	RunE: func(command *cobra.Command, args []string) error {
		startTime := time.Now()
		command.Annotations = map[string]string{
			"startTime": startTime.Format(time.RFC3339),
		}
		defer func() {
			command.Annotations["executionTime"] = time.Since(startTime).String()
		}()

		sqlQuery := strings.TrimSpace(args[0])
		if sqlQuery == "" {
			fmt.Println("Please enter your query")
			fmt.Printf("Example:\n  pb sql save \"select * from frontend\"\n")
			return nil
		}

		start, err := command.Flags().GetString(startFlag)
		if err != nil {
			command.Annotations["error"] = err.Error()
			return err
		}
		if start == "" {
			start = defaultStart
		}

		end, err := command.Flags().GetString(endFlag)
		if err != nil {
			command.Annotations["error"] = err.Error()
			return err
		}
		if end == "" {
			end = defaultEnd
		}

		sqlQuery = quoteStreamNames(sqlQuery)
		sqlQuery = quoteFieldsWithDots(sqlQuery)
		sqlQuery = ensureDefaultLimit(sqlQuery)

		name := strings.TrimSpace(sqlSaveName)
		if name == "" {
			name = defaultSavedQueryName(sqlQuery)
		}

		client := internalHTTP.DefaultClient(&DefaultProfile)
		if err := saveFilter(&client, sqlQuery, name, start, end); err != nil {
			command.Annotations["error"] = err.Error()
			return err
		}

		fmt.Printf("Query saved as '%s'\n", name)
		command.Annotations["error"] = "none"
		return nil
	},
}

func init() {
	SaveSQLCmd.Flags().StringP(startFlag, startFlagShort, defaultStart, "Start time for query.")
	SaveSQLCmd.Flags().StringP(endFlag, endFlagShort, defaultEnd, "End time for query.")
	SaveSQLCmd.Flags().StringVar(&sqlSaveName, "name", "", "Saved query name. Defaults to the dataset name when available.")
}

func defaultSavedQueryName(sqlQuery string) string {
	if stream := extractStreamName(sqlQuery); stream != "" {
		return stream
	}
	return "saved-query"
}

func ensureDefaultLimit(query string) string {
	trimmed := strings.TrimSpace(query)
	if trimmed == "" || hasTopLevelLimit(trimmed) {
		return query
	}
	suffix := ""
	if strings.HasSuffix(trimmed, ";") {
		trimmed = strings.TrimSpace(strings.TrimSuffix(trimmed, ";"))
		suffix = ";"
	}
	separator := " "
	if endsInLineComment(trimmed) {
		separator = "\n"
	}
	return trimmed + separator + "LIMIT 500" + suffix
}

func hasTopLevelLimit(query string) bool {
	depth := 0
	for i := 0; i < len(query); {
		switch query[i] {
		case '\'':
			i++
			for i < len(query) {
				if query[i] == '\'' {
					i++
					if i < len(query) && query[i] == '\'' {
						i++
						continue
					}
					break
				}
				i++
			}
		case '"':
			i++
			for i < len(query) {
				if query[i] == '"' {
					i++
					break
				}
				i++
			}
		case '-':
			if i+1 < len(query) && query[i+1] == '-' {
				i += 2
				for i < len(query) && query[i] != '\n' {
					i++
				}
				continue
			}
			i++
		case '/':
			if i+1 < len(query) && query[i+1] == '*' {
				i += 2
				for i+1 < len(query) {
					if query[i] == '*' && query[i+1] == '/' {
						i += 2
						break
					}
					i++
				}
				continue
			}
			i++
		case '(':
			depth++
			i++
		case ')':
			if depth > 0 {
				depth--
			}
			i++
		default:
			if depth == 0 && isLimitTokenAt(query, i) {
				return true
			}
			i++
		}
	}
	return false
}

func isLimitTokenAt(query string, idx int) bool {
	const token = "limit"
	if idx+len(token) > len(query) || !strings.EqualFold(query[idx:idx+len(token)], token) {
		return false
	}
	if idx > 0 && identChar(query[idx-1]) {
		return false
	}
	next := idx + len(token)
	return next >= len(query) || !identChar(query[next])
}

func endsInLineComment(query string) bool {
	inSingleQuote := false
	inDoubleQuote := false
	inBlockComment := false
	lineCommentStart := -1

	for i := 0; i < len(query); i++ {
		if inSingleQuote {
			if query[i] == '\'' {
				if i+1 < len(query) && query[i+1] == '\'' {
					i++
					continue
				}
				inSingleQuote = false
			}
			continue
		}
		if inDoubleQuote {
			if query[i] == '"' {
				if i+1 < len(query) && query[i+1] == '"' {
					i++
					continue
				}
				inDoubleQuote = false
			}
			continue
		}
		if inBlockComment {
			if query[i] == '*' && i+1 < len(query) && query[i+1] == '/' {
				inBlockComment = false
				i++
			}
			continue
		}

		switch query[i] {
		case '\'':
			inSingleQuote = true
		case '"':
			inDoubleQuote = true
		case '-':
			if i+1 < len(query) && query[i+1] == '-' {
				lineCommentStart = i
				i++
			}
		case '/':
			if i+1 < len(query) && query[i+1] == '*' {
				inBlockComment = true
				i++
			}
		case '\n', '\r':
			lineCommentStart = -1
		}
	}
	if lineCommentStart < 0 {
		return false
	}
	return strings.TrimSpace(query[lineCommentStart+2:]) != ""
}

const queryErrorPreviewLimit = 64 * 1024

func fetchData(client *internalHTTP.HTTPClient, query string, startTime, endTime, outputFormat string) error {
	query = ensureDefaultLimit(query)
	body, err := json.Marshal(struct {
		Query     string `json:"query"`
		StartTime string `json:"startTime"`
		EndTime   string `json:"endTime"`
	}{Query: query, StartTime: startTime, EndTime: endTime})
	if err != nil {
		return fmt.Errorf("failed to build request body: %w", err)
	}

	req, err := client.NewRequest("POST", "query", bytes.NewBuffer(body))
	if err != nil {
		return fmt.Errorf("failed to create new request: %w", err)
	}

	stopSpinner := startSpinner()
	resp, err := client.Client.Do(req)
	stopSpinner()
	if err != nil {
		return fmt.Errorf("request execution failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		preview, err := readLimitedErrorPreview(resp.Body)
		if err != nil {
			return fmt.Errorf("failed to read error response body: %w", err)
		}
		if preview == "" {
			return fmt.Errorf("query failed: server returned %s with an empty response body", resp.Status)
		}
		return fmt.Errorf("query failed: server returned %s: %s", resp.Status, preview)
	}

	reader := bufio.NewReader(resp.Body)
	if outputFormat == "json" {
		return streamSQLJSONResponse(os.Stdout, reader, resp.Status)
	}
	return streamSQLTextResponse(os.Stdout, reader, resp.Status)
}

func readLimitedErrorPreview(body io.Reader) (string, error) {
	limited := io.LimitReader(body, queryErrorPreviewLimit+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return "", err
	}
	truncated := len(data) > queryErrorPreviewLimit
	if truncated {
		data = data[:queryErrorPreviewLimit]
	}
	preview := string(bytes.TrimSpace(data))
	if truncated {
		preview += "..."
	}
	return preview, nil
}

func streamSQLTextResponse(out io.Writer, reader *bufio.Reader, status string) error {
	prefix, empty, err := readResponsePrefix(reader)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}
	if empty {
		fmt.Fprintf(out, "No response body returned (status: %s).\n", status)
		return nil
	}

	body := io.Reader(reader)
	if firstNonSpace(prefix) == '[' {
		isEmpty, consumed, err := consumeEmptyJSONArray(reader)
		if err != nil {
			return fmt.Errorf("failed to read response body: %w", err)
		}
		if isEmpty {
			fmt.Fprintf(out, "Query succeeded: no rows returned (status: %s).\n", status)
			return nil
		}
		body = io.MultiReader(bytes.NewReader(consumed), reader)
	}

	tracker := &trailingNewlineWriter{w: out}
	if _, err := io.Copy(tracker, io.MultiReader(bytes.NewReader(prefix), body)); err != nil {
		return fmt.Errorf("failed to stream response body: %w", err)
	}
	if tracker.wrote && tracker.last != '\n' {
		fmt.Fprintln(out)
	}
	return nil
}

func streamSQLJSONResponse(out io.Writer, reader *bufio.Reader, status string) error {
	prefix, empty, err := readResponsePrefix(reader)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}
	if empty {
		fmt.Fprintf(out, "No response body returned (status: %s).\n", status)
		return nil
	}
	if err := writePrettyJSONArray(out, io.MultiReader(bytes.NewReader(prefix), reader)); err != nil {
		return fmt.Errorf("error decoding JSON response: %w", err)
	}
	return nil
}

func readResponsePrefix(reader *bufio.Reader) ([]byte, bool, error) {
	var prefix []byte
	for {
		b, err := reader.ReadByte()
		if err == io.EOF {
			return prefix, true, nil
		}
		if err != nil {
			return prefix, false, err
		}
		prefix = append(prefix, b)
		if !isSQLSpace(b) {
			return prefix, false, nil
		}
	}
}

func consumeEmptyJSONArray(reader *bufio.Reader) (bool, []byte, error) {
	var consumed []byte
	for {
		b, err := reader.ReadByte()
		if err == io.EOF {
			return false, consumed, nil
		}
		if err != nil {
			return false, consumed, err
		}
		consumed = append(consumed, b)
		if isSQLSpace(b) {
			continue
		}
		if b != ']' {
			return false, consumed, nil
		}
		break
	}

	for {
		b, err := reader.ReadByte()
		if err == io.EOF {
			return true, consumed, nil
		}
		if err != nil {
			return false, consumed, err
		}
		consumed = append(consumed, b)
		if !isSQLSpace(b) {
			return false, consumed, nil
		}
	}
}

func writePrettyJSONArray(out io.Writer, body io.Reader) error {
	decoder := json.NewDecoder(body)
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok || delim != '[' {
		return fmt.Errorf("expected JSON array response")
	}

	first := true
	fmt.Fprint(out, "[")
	for decoder.More() {
		var item interface{}
		if err := decoder.Decode(&item); err != nil {
			return err
		}
		encoded, err := json.MarshalIndent(item, "", "  ")
		if err != nil {
			return err
		}
		if first {
			fmt.Fprintln(out)
			first = false
		} else {
			fmt.Fprintln(out, ",")
		}
		fmt.Fprint(out, "  ")
		fmt.Fprint(out, string(bytes.ReplaceAll(encoded, []byte("\n"), []byte("\n  "))))
	}
	if _, err := decoder.Token(); err != nil {
		return err
	}
	if first {
		fmt.Fprintln(out, "]")
	} else {
		fmt.Fprintln(out)
		fmt.Fprintln(out, "]")
	}
	return nil
}

func firstNonSpace(data []byte) byte {
	for _, b := range data {
		if !isSQLSpace(b) {
			return b
		}
	}
	return 0
}

type trailingNewlineWriter struct {
	w     io.Writer
	wrote bool
	last  byte
}

func (w *trailingNewlineWriter) Write(p []byte) (int, error) {
	n, err := w.w.Write(p)
	if n > 0 {
		w.wrote = true
		w.last = p[n-1]
	}
	return n, err
}

func extractStreamName(query string) string {
	re := regexp.MustCompile(`(?i)\bfrom\s+(?:"([^"]+)"|([a-zA-Z_][a-zA-Z0-9_-]*))`)
	m := re.FindStringSubmatch(query)
	if len(m) >= 3 {
		if m[1] != "" {
			return m[1]
		}
		return m[2]
	}
	return ""
}

func saveFilter(client *internalHTTP.HTTPClient, sqlQuery, name, startTime, endTime string) error {
	startT, err := parseTimeStr(startTime)
	if err != nil {
		return fmt.Errorf("invalid start time: %w", err)
	}
	endT, err := parseTimeStr(endTime)
	if err != nil {
		return fmt.Errorf("invalid end time: %w", err)
	}

	q := sqlQuery
	body, err := json.Marshal(struct {
		StreamName string `json:"stream_name"`
		FilterName string `json:"filter_name"`
		UserID     string `json:"user_id"`
		Query      struct {
			FilterType  string  `json:"filter_type"`
			FilterQuery *string `json:"filter_query"`
		} `json:"query"`
		TimeFilter struct {
			From string `json:"from"`
			To   string `json:"to"`
		} `json:"time_filter"`
	}{
		StreamName: extractStreamName(sqlQuery),
		FilterName: name,
		UserID:     DefaultProfile.Username,
		Query: struct {
			FilterType  string  `json:"filter_type"`
			FilterQuery *string `json:"filter_query"`
		}{FilterType: "sql", FilterQuery: &q},
		TimeFilter: struct {
			From string `json:"from"`
			To   string `json:"to"`
		}{
			From: startT.UTC().Format(time.RFC3339),
			To:   endT.UTC().Format(time.RFC3339),
		},
	})
	if err != nil {
		return err
	}

	req, err := client.NewRequest("POST", "filters", bytes.NewBuffer(body))
	if err != nil {
		return err
	}

	resp, err := client.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("server returned %s: %s", resp.Status, strings.TrimSpace(string(b)))
	}
	return nil
}

// Returns start and end time for query in RFC3339 format
// func parseTime(start, end string) (time.Time, time.Time, error) {
// 	if start == defaultStart && end == defaultEnd {
// 		return time.Now().Add(-1 * time.Minute), time.Now(), nil
// 	}

// 	startTime, err := time.Parse(time.RFC3339, start)
// 	if err != nil {
// 		// try parsing as duration
// 		duration, err := time.ParseDuration(start)
// 		if err != nil {
// 			return time.Time{}, time.Time{}, err
// 		}
// 		startTime = time.Now().Add(-1 * duration)
// 	}

// 	endTime, err := time.Parse(time.RFC3339, end)
// 	if err != nil {
// 		if end == "now" {
// 			endTime = time.Now()
// 		} else {
// 			return time.Time{}, time.Time{}, err
// 		}
// 	}

// 	return startTime, endTime, nil
// }

// // create a request body for saving filter without time_filter
// func createFilter(query string, filterName string) (err error) {
// 	userConfig, err := config.ReadConfigFromFile()
// 	if err != nil {
// 		return err
// 	}

// 	var userName string
// 	if profile, ok := userConfig.Profiles[userConfig.DefaultProfile]; ok {
// 		userName = profile.Username
// 	} else {
// 		fmt.Println("Default profile not found.")
// 		return
// 	}

// 	index := strings.Index(query, "from")
// 	fromPart := strings.TrimSpace(query[index+len("from"):])
// 	streamName := strings.Fields(fromPart)[0]

// 	queryTemplate := `{
// 		"filter_type":"sql",
// 		"filter_query": "%s"
// 		}`

// 	saveFilterTemplate := `
// 	{
//     "stream_name": "%s",
//     "filter_name": "%s",
//     "user_id": "%s",
//     "query": %s,
//     "time_filter": null
//     }`

// 	queryField := fmt.Sprintf(queryTemplate, query)

// 	finalQuery := fmt.Sprintf(saveFilterTemplate, streamName, filterName, userName, queryField)

// 	saveFilterToServer(finalQuery)

// 	return err
// }

// // create a request body for saving filter with time_filter
// func createFilterWithTime(query string, filterName string, startTime string, endTime string) (err error) {
// 	userConfig, err := config.ReadConfigFromFile()
// 	if err != nil {
// 		return err
// 	}

// 	var userName string
// 	if profile, ok := userConfig.Profiles[userConfig.DefaultProfile]; ok {
// 		userName = profile.Username
// 	} else {
// 		fmt.Println("Default profile not found.")
// 		return
// 	}

// 	index := strings.Index(query, "from")
// 	fromPart := strings.TrimSpace(query[index+len("from"):])
// 	streamName := strings.Fields(fromPart)[0]

// 	start, end, err := parseTimeToUTC(startTime, endTime)
// 	if err != nil {
// 		fmt.Println("Oops something went wrong!!!!")
// 		return err
// 	}

// 	queryTemplate := `{
// 		"filter_type":"sql",
// 		"filter_query": "%s"
// 		}`

// 	timeTemplate := `{
// 			"from": "%s",
// 			"to":  "%s"
// 		}`
// 	timeField := fmt.Sprintf(timeTemplate, start, end)

// 	saveFilterTemplate := `
// 	{
//     "stream_name": "%s",
//     "filter_name": "%s",
//     "user_id": "%s",
//     "query": %s,
//     "time_filter": %s
//     }`

// 	queryField := fmt.Sprintf(queryTemplate, query)

// 	finalQuery := fmt.Sprintf(saveFilterTemplate, streamName, filterName, userName, queryField, timeField)

// 	saveFilterToServer(finalQuery)

// 	return err
// }

// // fires a request to the server to save the filter with the associated user and stream
// func saveFilterToServer(finalQuery string) (err error) {
// 	client := DefaultClient()

// 	req, err := client.NewRequest("POST", "filters", bytes.NewBuffer([]byte(finalQuery)))
// 	if err != nil {
// 		return
// 	}

// 	resp, err := client.client.Do(req)
// 	if err != nil {
// 		return
// 	}

// 	if resp.StatusCode != 200 {
// 		fmt.Printf("\nSomething went wrong")
// 	}

// 	return err
// }

// // parses a time duration to supported utc format
// func parseTimeToUTC(start, end string) (time.Time, time.Time, error) {
// 	if start == defaultStart && end == defaultEnd {
// 		now := time.Now().UTC()
// 		return now.Add(-1 * time.Minute), now, nil
// 	}

// 	startTime, err := time.Parse(time.RFC3339, start)
// 	if err != nil {
// 		duration, err := time.ParseDuration(start)
// 		if err != nil {
// 			return time.Time{}, time.Time{}, err
// 		}
// 		startTime = time.Now().Add(-1 * duration).UTC()
// 	} else {
// 		startTime = startTime.UTC()
// 	}

// 	endTime, err := time.Parse(time.RFC3339, end)
// 	if err != nil {
// 		if end == "now" {
// 			endTime = time.Now().UTC()
// 		} else {
// 			return time.Time{}, time.Time{}, err
// 		}
// 	} else {
// 		endTime = endTime.UTC()
// 	}

// 	return startTime, endTime, nil
// }
