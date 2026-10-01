package tools

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"
	"unicode"
)

// tableShape names the column layout of one source table. Each category file
// uses one shape; the header row selects the columns by name so a reordered
// table still parses.
type tableShape int

const (
	shapeProviders tableShape = iota
	shapeAgents
	shapeADE
)

// categoryDef registers one source file: which category it fills and which
// table shape the file carries. It is the static half of Category.
type categoryDef struct {
	slug   string
	name   string
	prefix string
	file   string
	shape  tableShape
}

// categories is the source mapping, in catalog order. It mirrors the README
// table of tools-ai-swe-growth.
var categories = []categoryDef{
	{slug: "providers", name: "Providers", prefix: "P", file: "providers.md", shape: shapeProviders},
	{slug: "coding-agents", name: "Coding Agents", prefix: "CA", file: "codingagents.md", shape: shapeAgents},
	{slug: "ade", name: "AI Dev Environment", prefix: "ADE", file: "ade.md", shape: shapeADE},
}

// toolIDPattern matches the id column: a prefix, a dash, digits (P-001).
var toolIDPattern = regexp.MustCompile(`^[A-Z]+-\d+$`)

// markdownLinkPattern matches one whole link cell: [label](url).
var markdownLinkPattern = regexp.MustCompile(`^\[([^\]]*)\]\(([^)]+)\)$`)

// dateLayouts are the day formats the source tables carry.
var dateLayouts = []string{"2 Jan 2006", "02 Jan 2006", "2006-01-02"}

// parseTools reads one source file and returns the rows of its data table,
// in file order. The data table is the first table whose header carries an
// ID column; other tables (legend, notes) are ignored. A malformed row is
// skipped: one bad row never blocks the file. An error means the file has no
// usable data table at all.
func parseTools(r io.Reader, cat categoryDef) ([]Tool, error) {
	header, rows, err := parseTable(r)
	if err != nil {
		return nil, err
	}
	out := make([]Tool, 0, len(rows))
	seen := map[string]bool{}
	for _, cells := range rows {
		tool, ok := toolFromRow(cat, header, cells)
		if !ok || seen[tool.ID] {
			continue
		}
		seen[tool.ID] = true
		out = append(out, tool)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("tools: %s: table has no usable rows", cat.file)
	}
	return out, nil
}

// parseTable extracts the first table block whose header has an ID cell.
// A table block is a run of lines that start with '|'; line one is the
// header, line two the separator, the rest are data rows.
func parseTable(r io.Reader) ([]string, [][]string, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	var block [][]string
	blocks := 0
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "|") {
			block = append(block, splitRow(line))
			continue
		}
		if len(block) > 0 {
			blocks++
			if header, rows, ok := tableFromBlock(block); ok {
				return header, rows, nil
			}
			block = nil
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, nil, fmt.Errorf("tools: read table: %w", err)
	}
	if len(block) > 0 {
		blocks++
		if header, rows, ok := tableFromBlock(block); ok {
			return header, rows, nil
		}
	}
	if blocks == 0 {
		return nil, nil, errors.New("tools: file has no table")
	}
	return nil, nil, errors.New("tools: no table carries an ID column")
}

// tableFromBlock validates one block: header first, a separator row of dashes
// second, then data rows. ok is false when the block is not a data table.
func tableFromBlock(block [][]string) (header []string, rows [][]string, ok bool) {
	if len(block) < 3 {
		return nil, nil, false
	}
	header = block[0]
	if colIndex(header, "id") < 0 {
		return nil, nil, false
	}
	for _, cell := range block[1] {
		if strings.TrimSpace(strings.Trim(cell, "-:")) != "" || !strings.ContainsRune(cell, '-') {
			return nil, nil, false
		}
	}
	return header, block[2:], true
}

// splitRow cuts one markdown table line into trimmed cells. The source files
// never escape a pipe inside a cell, so a plain split is exact.
func splitRow(line string) []string {
	parts := strings.Split(line, "|")
	if len(parts) > 0 && strings.TrimSpace(parts[0]) == "" {
		parts = parts[1:]
	}
	if len(parts) > 0 && strings.TrimSpace(parts[len(parts)-1]) == "" {
		parts = parts[:len(parts)-1]
	}
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

// colIndex finds a column by header name, case-insensitive. It returns -1
// when the table has no such column.
func colIndex(header []string, names ...string) int {
	for i, h := range header {
		for _, name := range names {
			if strings.EqualFold(strings.TrimSpace(h), name) {
				return i
			}
		}
	}
	return -1
}

// cell returns the trimmed value of column i, or "" when the row is short.
func cell(row []string, i int) string {
	if i < 0 || i >= len(row) {
		return ""
	}
	return row[i]
}

// toolFromRow maps one data row to a Tool using the column names of its
// table. ok is false when the row misses a usable id or name.
func toolFromRow(cat categoryDef, header, row []string) (Tool, bool) {
	id := cell(row, colIndex(header, "id"))
	if !toolIDPattern.MatchString(id) {
		return Tool{}, false
	}
	name := cell(row, colIndex(header, "provider", "tool"))
	if name == "" || strings.ContainsRune(name, 0) {
		return Tool{}, false
	}
	label, link := parseLinkCell(cell(row, colIndex(header, "website")))
	tool := Tool{
		ID:           id,
		Category:     cat.slug,
		Name:         name,
		Website:      link,
		WebsiteLabel: label,
		Status:       parseStatus(cell(row, colIndex(header, "status"))),
		Updated:      parseDate(cell(row, colIndex(header, "updated"))),
	}
	switch cat.shape {
	case shapeProviders:
		tool.TopUp = boolPtr(parseEmojiBool(cell(row, colIndex(header, "top up"))))
		tool.Subscribe = boolPtr(parseEmojiBool(cell(row, colIndex(header, "subscribe"))))
		tool.MinSpend = cell(row, colIndex(header, "min spend"))
	case shapeADE:
		tool.AIFeatures = parseFeatures(cell(row, colIndex(header, "ai features")))
	}
	return tool, true
}

// parseLinkCell reads a markdown link cell into its label and URL. A plain
// text cell yields the text as label and an empty URL.
func parseLinkCell(raw string) (label, link string) {
	if match := markdownLinkPattern.FindStringSubmatch(raw); match != nil {
		return strings.TrimSpace(match[1]), strings.TrimSpace(match[2])
	}
	return raw, ""
}

// parseEmojiBool reads a ✅/❌ cell: ✅ means true, anything else false.
func parseEmojiBool(raw string) bool {
	return strings.ContainsRune(raw, '✅')
}

// parseStatus reads a status cell like "✅ Active" into a lowercase token.
// Emoji and marks are dropped; an empty cell yields "unknown".
func parseStatus(raw string) string {
	text := strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == ' ' || r == '-' {
			return r
		}
		return -1
	}, raw)
	text = strings.ToLower(strings.Join(strings.Fields(text), " "))
	if text == "" {
		return "unknown"
	}
	return text
}

// parseDate reads a source date like "12 Jul 2026" into YYYY-MM-DD. A value
// the layouts do not cover stays as its trimmed raw text.
func parseDate(raw string) string {
	raw = strings.TrimSpace(raw)
	for _, layout := range dateLayouts {
		if t, err := time.Parse(layout, raw); err == nil {
			return t.Format("2006-01-02")
		}
	}
	return raw
}

// parseFeatures reads a comma list cell into trimmed items.
func parseFeatures(raw string) []string {
	out := make([]string, 0, 4)
	for _, part := range strings.Split(raw, ",") {
		if v := strings.TrimSpace(part); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// boolPtr boxes a parsed flag so the JSON field can be absent entirely on
// categories whose source table lacks the column.
func boolPtr(v bool) *bool { return &v }
