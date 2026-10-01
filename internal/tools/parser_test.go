package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixtureFile opens one trimmed real source file from testdata.
func fixtureFile(t *testing.T, name string) *os.File {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("open fixture %s: %v", name, err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}

func TestParseProvidersMapsEveryColumn(t *testing.T) {
	list, err := parseTools(fixtureFile(t, "providers.md"), categories[0])
	if err != nil {
		t.Fatalf("parseTools() error = %v", err)
	}
	if len(list) != 4 {
		t.Fatalf("len(list) = %d, want 4", len(list))
	}

	openai := list[0]
	if openai.ID != "P-001" || openai.Category != "providers" || openai.Name != "OpenAI" {
		t.Fatalf("row 0 = %+v, want P-001 OpenAI in providers", openai)
	}
	if openai.Website != "https://openai.com" || openai.WebsiteLabel != "openai.com" {
		t.Fatalf("website = %q / %q, want the link split into url and label", openai.Website, openai.WebsiteLabel)
	}
	if openai.TopUp == nil || !*openai.TopUp || openai.Subscribe == nil || !*openai.Subscribe {
		t.Fatalf("flags = %v / %v, want both true", openai.TopUp, openai.Subscribe)
	}
	if openai.MinSpend != "$5 (top up)" {
		t.Fatalf("min spend = %q, want $5 (top up)", openai.MinSpend)
	}
	if openai.Status != "active" || openai.Updated != "2026-07-12" {
		t.Fatalf("status/updated = %q / %q, want active / 2026-07-12", openai.Status, openai.Updated)
	}
	if openai.AIFeatures != nil {
		t.Fatalf("ai_features = %v, want nil on a providers row", openai.AIFeatures)
	}

	groq := list[1]
	if groq.Subscribe == nil || *groq.Subscribe {
		t.Fatalf("groq subscribe = %v, want false (❌ keeps the field visible)", groq.Subscribe)
	}
}

func TestParseAgentsUsesTheSharedColumns(t *testing.T) {
	list, err := parseTools(fixtureFile(t, "codingagents.md"), categories[1])
	if err != nil {
		t.Fatalf("parseTools() error = %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("len(list) = %d, want 3", len(list))
	}
	devin := list[1]
	if devin.ID != "CA-018" || devin.Category != "coding-agents" || devin.Name != "Devin" {
		t.Fatalf("row 1 = %+v, want CA-018 Devin in coding-agents", devin)
	}
	if devin.TopUp != nil || devin.MinSpend != "" || devin.AIFeatures != nil {
		t.Fatalf("agent row carries provider/ade fields: %+v", devin)
	}
}

func TestParseADESplitsTheFeaturesList(t *testing.T) {
	list, err := parseTools(fixtureFile(t, "ade.md"), categories[2])
	if err != nil {
		t.Fatalf("parseTools() error = %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("len(list) = %d, want 3", len(list))
	}
	warp := list[1]
	if warp.ID != "ADE-012" || warp.Category != "ade" {
		t.Fatalf("row 1 = %+v, want ADE-012 in ade", warp)
	}
	want := []string{"AI Terminal", "Agent Mode", "Warp Drive", "IDE-like Editing"}
	if len(warp.AIFeatures) != len(want) {
		t.Fatalf("features = %v, want %v", warp.AIFeatures, want)
	}
	for i := range want {
		if warp.AIFeatures[i] != want[i] {
			t.Fatalf("features[%d] = %q, want %q", i, warp.AIFeatures[i], want[i])
		}
	}
}

func TestParsePicksTheIDTableNotTheLegend(t *testing.T) {
	// providers.md carries a second table (Legend: Symbol | Arti). The parser
	// must skip it and read the table with an ID column only.
	list, err := parseTools(fixtureFile(t, "providers.md"), categories[0])
	if err != nil {
		t.Fatalf("parseTools() error = %v", err)
	}
	for _, tool := range list {
		if !toolIDPattern.MatchString(tool.ID) {
			t.Fatalf("legend row leaked into the catalog: %+v", tool)
		}
	}
}

func TestParseSkipsMalformedRows(t *testing.T) {
	source := `# Tools

| ID | Tool | Website | Status | Updated |
|----|------|---------|--------|---------|
| CA-001 | Copilot | [github.com](https://github.com) | ✅ Active | 12 Jul 2026 |
| no-id | Broken | [x.dev](https://x.dev) | ✅ Active | 12 Jul 2026 |
| CA-002 |  | [x.dev](https://x.dev) | ✅ Active | 12 Jul 2026 |
| CA-003 | Cursor | [cursor.com](https://cursor.com) | ✅ Active | 12 Jul 2026 |
`
	list, err := parseTools(strings.NewReader(source), categories[1])
	if err != nil {
		t.Fatalf("parseTools() error = %v", err)
	}
	if len(list) != 2 || list[0].ID != "CA-001" || list[1].ID != "CA-003" {
		t.Fatalf("list = %+v, want only the 2 valid rows", list)
	}
}

func TestParseErrorsWithoutAnIDTable(t *testing.T) {
	source := `# Notes

| Symbol | Arti |
|--------|------|
| ✅ | Tersedia |
`
	if _, err := parseTools(strings.NewReader(source), categories[0]); err == nil {
		t.Fatal("parseTools() error = nil, want no-ID-table failure")
	}

	if _, err := parseTools(strings.NewReader("# no tables here\n"), categories[0]); err == nil {
		t.Fatal("parseTools() on empty file error = nil, want a failure")
	}
}

func TestParseHelpers(t *testing.T) {
	if label, link := parseLinkCell("[x.dev](https://x.dev)"); label != "x.dev" || link != "https://x.dev" {
		t.Fatalf("link cell = %q / %q", label, link)
	}
	if label, link := parseLinkCell("plain text"); label != "plain text" || link != "" {
		t.Fatalf("plain cell = %q / %q", label, link)
	}
	if !parseEmojiBool("✅") || parseEmojiBool("❌") || parseEmojiBool("") {
		t.Fatal("emoji bool misread")
	}
	if got := parseStatus("✅ Active"); got != "active" {
		t.Fatalf("status = %q, want active", got)
	}
	if got := parseStatus(""); got != "unknown" {
		t.Fatalf("empty status = %q, want unknown", got)
	}
	if got := parseDate("01 Oct 2026"); got != "2026-10-01" {
		t.Fatalf("date = %q, want 2026-10-01", got)
	}
	if got := parseDate("not a date"); got != "not a date" {
		t.Fatalf("unparseable date = %q, want the raw text kept", got)
	}
	if got := parseFeatures(" a, b ,, c "); len(got) != 3 || got[0] != "a" || got[2] != "c" {
		t.Fatalf("features = %v", got)
	}
}
