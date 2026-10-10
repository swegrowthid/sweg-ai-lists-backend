package dailyterm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// quirksLocation is the month the quirks fixture pretends to come from.
var quirksLocation = monthLocation{
	year: 2026, month: time.March, file: "2026/mar-term.md",
	pageBase: "https://example.test/blob/main",
}

// fixtureFile opens one real, trimmed source file from testdata.
func fixtureFile(t *testing.T, name string) *os.File {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("open fixture %s: %v", name, err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}

// parseFixture parses one testdata file under a synthetic month location.
func parseFixture(t *testing.T, name string, loc monthLocation) []Term {
	t.Helper()
	terms, err := parseMonth(fixtureFile(t, name), loc)
	if err != nil {
		t.Fatalf("parseMonth(%s) error = %v", name, err)
	}
	return terms
}

func TestParseMonthReadsTheRealJanuarySlice(t *testing.T) {
	loc := monthLocation{year: 2026, month: time.January, file: "2026/jan-term.md"}
	terms := parseFixture(t, "jan-term.md", loc)
	if len(terms) != 3 {
		t.Fatalf("len(terms) = %d, want the 3 terms in the slice", len(terms))
	}

	first := terms[0]
	if first.Title != "Principle of Least Surprise" || first.Day != 1 {
		t.Fatalf("term 0 = %+v, want day 1 Principle of Least Surprise", first)
	}
	if first.ID != "2026-01-principle-of-least-surprise" {
		t.Fatalf("term 0 id = %q", first.ID)
	}
	if first.MonthKey != "2026-01" || first.Year != 2026 || first.Month != 1 {
		t.Fatalf("term 0 month fields = %+v", first)
	}
	if !strings.HasPrefix(first.Definition, "Principle of Least Surprise (also called") {
		t.Fatalf("term 0 definition = %q, want the source prose", first.Definition)
	}
	// The example opens with a fenced code block; the fence and its comment line
	// must survive verbatim, because the frontend renders them.
	if !strings.HasPrefix(first.Example, "Good (Follows POLS)") {
		t.Fatalf("term 0 example = %q, want the source example", first.Example)
	}
	if !strings.Contains(first.Example, "def delete(item):") || !strings.Contains(first.Example, "```") {
		t.Fatalf("term 0 example lost its code block: %q", first.Example)
	}
	// Neither label line survives into the stored text.
	if strings.Contains(first.Definition, "### Definition") || strings.Contains(first.Example, "### Examples") {
		t.Fatalf("label lines leaked into term 0: def=%q ex=%q", first.Definition, first.Example)
	}

	if terms[1].Title != "Sieve Algorithm or Sieve of Eratosthenes" || terms[1].Day != 2 {
		t.Fatalf("term 1 = %+v, want day 2 Sieve Algorithm", terms[1])
	}
	if terms[2].Title != "AutoML Frameworks" || terms[2].Day != 5 {
		t.Fatalf("term 2 = %+v, want day 5 AutoML Frameworks", terms[2])
	}
}

func TestParseMonthReadsTheRealJune2025File(t *testing.T) {
	// The smallest real file: two day markers and three terms, because one term
	// follows a rule with no marker above it.
	loc := monthLocation{year: 2025, month: time.June, file: "2025/june-term.md", pageBase: "https://example.test/blob/main"}
	terms := parseFixture(t, "june-term.md", loc)
	if len(terms) != 3 {
		t.Fatalf("len(terms) = %d, want 3", len(terms))
	}
	if terms[0].Title != "Polya Problem Solving Technique" || terms[0].Day != 28 {
		t.Fatalf("term 0 = %+v, want day 28 Polya", terms[0])
	}
	if terms[1].Title != "gRPC" || terms[1].Day != 30 {
		t.Fatalf("term 1 = %+v, want day 30 gRPC", terms[1])
	}
	// Race Condition is separated by a rule, not a marker, so it carries day 0.
	if terms[2].Title != "Race Condition" || terms[2].Day != 0 {
		t.Fatalf("term 2 = %+v, want Race Condition with no day", terms[2])
	}
	if terms[2].SourceURL != "https://example.test/blob/main/2025/june-term.md" {
		t.Fatalf("source url = %q", terms[2].SourceURL)
	}
	// The gRPC term holds four fenced proto blocks and an H1 comment line inside
	// one of them; none of it may start a term or reach the definition.
	if len(terms) != 3 {
		t.Fatalf("a fenced line started a term: %+v", terms)
	}
	if strings.Contains(terms[1].Example, "```proto") == false {
		t.Fatalf("gRPC example lost its code fences: %q", terms[1].Example)
	}
}

// TestParseMonthHandlesTheRealQuirks drives the fixture that mirrors every
// irregular shape found upstream.
func TestParseMonthHandlesTheRealQuirks(t *testing.T) {
	terms := parseFixture(t, "quirks.md", quirksLocation)
	want := []string{
		"Alpha Term", "Beta Term", "Gamma Term", "Delta Term",
		"Epsilon Term", "Eta Term", "Zeta Term", "Theta Term", "Iota Term",
	}
	if len(terms) != len(want) {
		t.Fatalf("len(terms) = %d, want %d: %+v", len(terms), len(want), terms)
	}
	for i, title := range want {
		if terms[i].Title != title {
			t.Fatalf("terms[%d] = %q, want %q", i, terms[i].Title, title)
		}
	}

	byTitle := map[string]Term{}
	for _, term := range terms {
		byTitle[term.Title] = term
	}

	// An H2 inside a fenced code block must not start a term, and it must stay in
	// the example text.
	alpha := byTitle["Alpha Term"]
	if !strings.Contains(alpha.Example, "## Not A Term") {
		t.Fatalf("fenced heading lost from Alpha example: %q", alpha.Example)
	}

	// A title one level lower than its siblings.
	if beta := byTitle["Beta Term"]; beta.Day != 2 || !strings.Contains(beta.Definition, "one level lower") {
		t.Fatalf("Beta = %+v, want the H3 title read as a term", beta)
	}

	// A day marker written as an H2.
	if gamma := byTitle["Gamma Term"]; gamma.Day != 3 || !strings.Contains(gamma.Example, "labels its example at H2") {
		t.Fatalf("Gamma = %+v, want day 3 with an H2 example label", gamma)
	}

	// No example section at all.
	if delta := byTitle["Delta Term"]; delta.Example != "" || !strings.Contains(delta.Definition, "no example") {
		t.Fatalf("Delta = %+v, want an empty example", delta)
	}

	// A label-shaped line inside a fence must not split the term, and the text
	// after the fence must stay with the definition.
	epsilon := byTitle["Epsilon Term"]
	if !strings.Contains(epsilon.Definition, "part one") || !strings.Contains(epsilon.Definition, "part two") {
		t.Fatalf("Epsilon definition = %q, want both parts", epsilon.Definition)
	}
	if strings.Contains(epsilon.Definition, "```") == false {
		t.Fatalf("Epsilon definition lost its fence: %q", epsilon.Definition)
	}
	if epsilon.Example != "Epsilon example." {
		t.Fatalf("Epsilon example = %q", epsilon.Example)
	}

	// An example label with no definition label: the preamble is empty, so the
	// definition is empty and the example carries the text.
	if eta := byTitle["Eta Term"]; eta.Definition != "" || eta.Example != "Eta opens with the example label and carries no definition label." {
		t.Fatalf("Eta = %+v", eta)
	}

	// A term opened by an equals-sign rule rather than a dash rule.
	if zeta := byTitle["Zeta Term"]; zeta.Day != 0 || !strings.Contains(zeta.Definition, "equals-sign rule") {
		t.Fatalf("Zeta = %+v, want a rule-anchored term with no day", zeta)
	}
	// Prose-only term: the preamble is the definition.
	if theta := byTitle["Theta Term"]; !strings.Contains(theta.Definition, "prose only") || theta.Example != "" {
		t.Fatalf("Theta = %+v, want the prose as definition", theta)
	}

	// A preamble before a real definition label is dropped: the definition below
	// already says it.
	if iota := byTitle["Iota Term"]; strings.Contains(iota.Definition, "introduction paragraph") {
		t.Fatalf("Iota kept the preamble: %q", iota.Definition)
	}
	if iota := byTitle["Iota Term"]; !strings.Contains(iota.Definition, "Iota definition.") {
		t.Fatalf("Iota definition = %q", iota.Definition)
	}
}

func TestParseMonthHandlesEmptyAndTermlessFiles(t *testing.T) {
	loc := monthLocation{year: 2026, month: time.October, file: "2026/oct-term.md"}
	terms, err := parseMonth(strings.NewReader(""), loc)
	if err != nil {
		t.Fatalf("parseMonth(empty) error = %v", err)
	}
	if len(terms) != 0 {
		t.Fatalf("len(terms) = %d, want 0", len(terms))
	}

	terms, err = parseMonth(strings.NewReader("just prose\n\n## Not A Term\n\n### Definition:\n\ntext\n"), loc)
	if err != nil {
		t.Fatalf("parseMonth(prose) error = %v", err)
	}
	// "## Not A Term" sits after prose, so it is not anchored, and the file has
	// no term at all.
	if len(terms) != 0 {
		t.Fatalf("terms = %+v, want none: an unanchored heading is body", terms)
	}
}

func TestParseMonthKeepsIndentedCodeAndDropsTrailingSpace(t *testing.T) {
	source := "day - 1\n\n## Term\n\n### Definition:\n\n    indented code   \n\n### Example:\n\n```\nx\n```\n"
	terms, err := parseMonth(strings.NewReader(source), quirksLocation)
	if err != nil {
		t.Fatalf("parseMonth() error = %v", err)
	}
	if len(terms) != 1 {
		t.Fatalf("len(terms) = %d, want 1", len(terms))
	}
	if terms[0].Definition != "    indented code" {
		t.Fatalf("definition = %q, want the indentation kept and the trailing space dropped", terms[0].Definition)
	}
}

func TestSlugify(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"Sieve Algorithm or Sieve of Eratosthenes", "sieve-algorithm-or-sieve-of-eratosthenes"},
		{"CQRS (Command Query Responsibility Segregation)", "cqrs-command-query-responsibility-segregation"},
		{"SLIs/SLOs/SLAs", "slis-slos-slas"},
		{"The \"Billion Laughs\" Attack", "the-billion-laughs-attack"},
		{"Static Single Assignment form (SSA)", "static-single-assignment-form-ssa"},
		{"Manacher's Algorithm", "manacher-s-algorithm"},
		{"", ""},
		{"!!!", ""},
	} {
		if got := slugify(tc.in); got != tc.want {
			t.Fatalf("slugify(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestSlugifyCapsTheLength(t *testing.T) {
	long := strings.Repeat("ab ", 80)
	got := slugify(long)
	if len(got) > maxSlugLength {
		t.Fatalf("len(slug) = %d, want at most %d", len(got), maxSlugLength)
	}
	if strings.HasSuffix(got, "-") {
		t.Fatalf("slug = %q, want no trailing hyphen", got)
	}
}

func TestTermIDIsStableAndCarriesNoDay(t *testing.T) {
	first := termID(2026, time.January, "Sieve Algorithm")
	second := termID(2026, time.January, "Sieve Algorithm")
	if first != second {
		t.Fatalf("termID is not stable: %q then %q", first, second)
	}
	if first != "2026-01-sieve-algorithm" {
		t.Fatalf("termID = %q", first)
	}
}

func TestParseMonthKey(t *testing.T) {
	year, month, err := parseMonthKey("2026-01")
	if err != nil || year != 2026 || month != time.January {
		t.Fatalf("parseMonthKey(2026-01) = %d/%v/%v", year, month, err)
	}
	for _, raw := range []string{"", "2026", "2026-1", "2026-13", "2026-00", "2026-aa", "2026/01", "20e6-01", " 2026-01", "2026-01 "} {
		if _, _, err := parseMonthKey(raw); err == nil {
			t.Fatalf("parseMonthKey(%q) error = nil, want a rejection", raw)
		}
	}
}
