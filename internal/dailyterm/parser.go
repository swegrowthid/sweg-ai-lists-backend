package dailyterm

import (
	"bufio"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// monthTokens maps the month token of a source file name to its month number.
// Upstream is inconsistent: it carries "june" next to "july" and "sept" next to
// "october", so every spelling it has ever used is listed. An unknown token
// means that file is skipped, never that the sync fails.
var monthTokens = map[string]time.Month{
	"jan":     time.January,
	"feb":     time.February,
	"mar":     time.March,
	"apr":     time.April,
	"may":     time.May,
	"jun":     time.June,
	"june":    time.June,
	"jul":     time.July,
	"july":    time.July,
	"aug":     time.August,
	"august":  time.August,
	"sep":     time.September,
	"sept":    time.September,
	"oct":     time.October,
	"october": time.October,
	"nov":     time.November,
	"dec":     time.December,
}

// monthFilePattern matches one month file path in the source tree, e.g.
// "2026/jan-term.md". Only these paths are fetched, so an unrelated markdown
// file added to the repo is ignored.
var monthFilePattern = regexp.MustCompile(`^(\d{4})/([a-z]+)-term\.md$`)

// dayMarkerPattern matches the journal's day marker, "day - 12". Upstream also
// writes it as a heading (`## day - 24`) and with odd spacing, so leading
// hashes and spaces are tolerated.
var dayMarkerPattern = regexp.MustCompile(`(?i)^\s*#{0,6}\s*day\s*-\s*(\d+)\s*$`)

// termHeadingPattern matches a term title: an H2 or H3 heading with a space
// after the hashes. H3 is accepted because two real terms sit a level lower.
var termHeadingPattern = regexp.MustCompile(`^(#{2,3})\s+(\S.*?)\s*$`)

// sectionLabelPattern matches a body label such as "### Definition:",
// "### example", "## Example:" or "### Profile:". A label is never a term
// title, however it is spelled or capitalised.
var sectionLabelPattern = regexp.MustCompile(`(?i)^#{2,6}\s*(definition|example|profile)s?\s*:?\s*$`)

// definitionLabelPattern matches only a definition or profile label.
var definitionLabelPattern = regexp.MustCompile(`(?i)^#{2,6}\s*(definition|profile)s?\s*:?\s*$`)

// rulePattern matches the horizontal rule that closes a term. Upstream uses
// three or more dashes, and once (=) three equals signs.
var rulePattern = regexp.MustCompile(`^ {0,3}(?:-{3,}|={3,})\s*$`)

// fencePattern matches a code fence line, indented or not. Upstream indents one
// fence by sixteen spaces, past what CommonMark accepts, so indentation is not
// required to match it here.
var fencePattern = regexp.MustCompile(`^\s*(?:` + "```" + `|~~~)`)

// lineKind classifies the last significant line, which decides whether a
// heading starts a new term or belongs to the term already open.
type lineKind int

const (
	// kindEmpty is the start of the file and any run of blank lines.
	kindEmpty lineKind = iota
	// kindRule is a horizontal rule that closed the previous term.
	kindRule
	// kindDay is a "day - N" marker, which introduces the next term.
	kindDay
	// kindHeading is a heading already consumed, as a title or as body.
	kindHeading
	// kindText is ordinary body prose, a code line, or a table row.
	kindText
)

// monthLocation is where one month file lives. It fills the provenance fields
// of every term parsed out of it.
type monthLocation struct {
	year     int
	month    time.Month
	file     string
	pageBase string
}

// bodyParts accumulates one term body while it is read: the preamble above the
// first label, the text under a Definition or Profile label, and the text under
// an Example label. Each section keeps every line it is given, so nothing a
// client could need is dropped even when the source layout is irregular.
type bodyParts struct {
	top        []string
	definition []string
	example    []string
	// inFence tracks an open code fence so a label inside a code block is left
	// alone. One upstream term body holds a literal "### Example:" in a fence.
	inFence bool
}

// section says where a body line belongs.
type section int

const (
	// sectionTop is body text above the first label. It becomes the definition
	// for a term that opens with prose instead of a Definition heading.
	sectionTop section = iota
	// sectionDefinition is the text under a Definition or Profile heading.
	sectionDefinition
	// sectionExample is the text under an Example heading.
	sectionExample
)

// parseMonth reads one month file and returns its terms in file order. A file
// that holds no term is not an error: a month can legitimately hold none, and
// the service decides whether an empty month is worth reporting.
func parseMonth(r io.Reader, loc monthLocation) ([]Term, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)

	titles := make([]string, 0, 24)
	days := make([]int, 0, 24)
	parts := make([]bodyParts, 0, 24)
	current := -1
	where := sectionTop
	previous := kindEmpty
	day := 0

	for scanner.Scan() {
		// Trailing whitespace and a stray CR are dropped so they never reach the
		// stored text. Leading whitespace is kept: the bodies carry indented code.
		line := strings.TrimRight(strings.TrimSuffix(scanner.Text(), "\r"), " \t")
		trimmed := strings.TrimSpace(line)

		if rulePattern.MatchString(line) {
			previous = kindRule
			day = 0
			continue
		}
		if match := dayMarkerPattern.FindStringSubmatch(line); match != nil {
			if n, err := strconv.Atoi(match[1]); err == nil {
				day = n
			}
			previous = kindDay
			continue
		}

		if match := termHeadingPattern.FindStringSubmatch(line); match != nil {
			// A title is anchored only when nothing but a rule, a day marker, or
			// the start of the file stands above it. An H3 below a title, or a
			// label such as "### Example:", is body, not a new term.
			//
			// No whole-file fence tracking is needed at this point: upstream has
			// a handful of "## " lines inside fenced code blocks, but every one is
			// preceded by a code line, so "previous" is kindText and the heading is
			// not anchored. The fence state below is a separate concern, it only
			// guards the label match inside a term body.
			anchored := previous == kindEmpty || previous == kindRule || previous == kindDay
			title := strings.TrimSpace(match[2])
			if anchored && title != "" && !sectionLabelPattern.MatchString(line) {
				titles = append(titles, title)
				days = append(days, day)
				parts = append(parts, bodyParts{})
				current = len(parts) - 1
				where = sectionTop
				previous = kindHeading
				continue
			}
		}

		if current < 0 {
			if trimmed != "" {
				previous = kindText
			}
			continue
		}
		// A label flips the section and is dropped: the client gets the text, not
		// the scaffolding. A label inside an open code fence is left as body.
		if !parts[current].toggleFence(line) {
			switch {
			case definitionLabelPattern.MatchString(trimmed):
				where = sectionDefinition
				if trimmed != "" {
					previous = kindText
				}
				continue
			case sectionLabelPattern.MatchString(trimmed):
				where = sectionExample
				if trimmed != "" {
					previous = kindText
				}
				continue
			}
		}
		if trimmed != "" {
			previous = kindText
		}
		switch where {
		case sectionDefinition:
			parts[current].definition = append(parts[current].definition, line)
		case sectionExample:
			parts[current].example = append(parts[current].example, line)
		default:
			parts[current].top = append(parts[current].top, line)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("dailyterm: read %s: %w", loc.file, err)
	}

	terms := make([]Term, 0, len(titles))
	for i, title := range titles {
		parts[i].finish()
		terms = append(terms, newTerm(loc, title, days[i], parts[i]))
	}
	return terms, nil
}

// toggleFence records a code fence boundary and reports whether the line is a
// fence line, or sits inside an open fence. A label line inside a fence is body.
//
// A term body with an unbalanced fence (two upstream bodies carry one) leaves
// the tracker stuck open, so a later label is read as body. That keeps content
// and loses only the definition/example split for that one term.
func (p *bodyParts) toggleFence(line string) bool {
	if !fencePattern.MatchString(line) {
		return p.inFence
	}
	p.inFence = !p.inFence
	return true
}

// finish trims each accumulated section. When a term carries no Definition or
// Profile label, its preamble is the definition: a handful of upstream terms
// open with prose instead (e.g. "The Virtuous Cycle", "Linus Torvald"). When a
// label is present the preamble only introduces what the definition repeats, so
// it is dropped.
func (p *bodyParts) finish() {
	p.top = trimBlankLines(p.top)
	p.definition = trimBlankLines(p.definition)
	p.example = trimBlankLines(p.example)
	if len(p.definition) == 0 {
		p.definition = p.top
	}
}

// trimBlankLines drops empty lines from both ends while keeping the interior
// spacing a code block depends on.
func trimBlankLines(lines []string) []string {
	start, end := 0, len(lines)
	for start < end && strings.TrimSpace(lines[start]) == "" {
		start++
	}
	for end > start && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	if start == end {
		return nil
	}
	return lines[start:end]
}

// newTerm assembles one stored term from its parsed pieces.
func newTerm(loc monthLocation, title string, day int, parts bodyParts) Term {
	url := ""
	if loc.pageBase != "" {
		url = loc.pageBase + "/" + loc.file
	}
	return Term{
		ID:         termID(loc.year, loc.month, title),
		Year:       loc.year,
		Month:      int(loc.month),
		MonthKey:   monthKey(loc.year, loc.month),
		Day:        day,
		Slug:       slugify(title),
		Title:      title,
		Definition: strings.Join(parts.definition, "\n"),
		Example:    strings.Join(parts.example, "\n"),
		SourceFile: loc.file,
		SourceURL:  url,
	}
}
