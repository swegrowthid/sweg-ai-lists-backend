package dailyterm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

const (
	// defaultRawBase is where the month markdown files are fetched from.
	defaultRawBase = "https://raw.githubusercontent.com/swegrowthid/daily-term-SE-Growth/main"

	// defaultTreeURL lists every path in the source repo, so the months that
	// exist are discovered rather than assumed. Upstream spells month names
	// inconsistently (june and july, sept and october) and does not carry a file
	// for every month; a hardcoded list would quietly miss them.
	defaultTreeURL = "https://api.github.com/repos/swegrowthid/daily-term-SE-Growth/git/trees/main?recursive=1"

	// defaultPageBase is the human-readable source root for term links.
	defaultPageBase = "https://github.com/swegrowthid/daily-term-SE-Growth/blob/main"

	// maxFileBytes caps one month file download. The largest file is ~360 KiB.
	maxFileBytes = 2 << 20

	// maxTreeBytes caps the tree listing download.
	maxTreeBytes = 4 << 20

	// maxPathBytes caps one path from the tree listing, so a hostile or broken
	// listing cannot make the sync build an absurd request.
	maxPathBytes = 200

	// fetchTimeout bounds one request.
	fetchTimeout = 20 * time.Second

	// userAgent identifies this backend to GitHub.
	userAgent = "sweg-ai-lists-backend (+https://github.com/swegrowthid/sweg-ai-lists-backend)"
)

// Store is the snapshot port. Service depends on it, never on a concrete store.
// The journal is read-only for clients: writes happen only inside Sync, through
// ReplaceAll.
type Store interface {
	// ReplaceAll swaps the whole snapshot atomically.
	ReplaceAll(ctx context.Context, terms []Term) error

	// List returns the snapshot in catalog order. A monthKey narrows it to one
	// month, YYYY-MM; an empty monthKey returns every month.
	List(ctx context.Context, monthKey string) ([]Term, error)

	// Get returns one term by id, or ErrNotFound.
	Get(ctx context.Context, id string) (Term, error)
}

// source describes where the markdown files come from.
type source struct {
	rawBase  string
	treeURL  string
	pageBase string
	client   *http.Client
}

// Service owns the daily term use-cases. No HTTP, no SQL here.
type Service struct {
	store  Store
	source source

	// now is a seam for the default-month rule, so tests do not depend on the
	// wall clock.
	now func() time.Time
}

// NewService wires the store to the live GitHub source. Nil store is a
// programmer bug, so panic early.
func NewService(store Store) *Service {
	return newServiceWithSource(store, source{})
}

// newServiceWithSource fills the source defaults and wires the store. Tests
// point it at an httptest server; production uses NewService.
func newServiceWithSource(store Store, src source) *Service {
	if store == nil {
		panic("dailyterm: nil Store")
	}
	if src.rawBase == "" {
		src.rawBase = defaultRawBase
	}
	if src.treeURL == "" {
		src.treeURL = defaultTreeURL
	}
	if src.pageBase == "" {
		src.pageBase = defaultPageBase
	}
	if src.client == nil {
		src.client = &http.Client{Timeout: fetchTimeout}
	}
	return &Service{store: store, source: src, now: time.Now}
}

// List returns one month of terms plus the metadata that says which month
// answered.
//
// An empty month means "the newest month that exists, unless the current server
// month does": the homepage asks for the journal without naming a month, and
// the current month is empty for the first days of every month. A named month
// that has no file is ErrNotFound, which is unambiguous because every file
// upstream yields at least one term.
func (s *Service) List(ctx context.Context, month string) (TermList, error) {
	if containsNUL(month) {
		return TermList{}, ErrInvalidInput
	}
	key, err := s.resolveMonth(ctx, strings.TrimSpace(month))
	if err != nil {
		return TermList{}, err
	}

	terms, err := s.store.List(ctx, key)
	if err != nil {
		return TermList{}, err
	}
	if len(terms) == 0 {
		return TermList{}, ErrNotFound
	}
	if terms == nil {
		terms = []Term{}
	}
	return TermList{Data: terms, Meta: s.meta(key, terms[0], len(terms))}, nil
}

// Months returns every synced month, newest first, with its term count. The
// homepage uses it to build a month picker without knowing the source layout.
func (s *Service) Months(ctx context.Context) ([]MonthSummary, error) {
	all, err := s.store.List(ctx, "")
	if err != nil {
		return nil, err
	}
	order := make([]string, 0, 8)
	seen := map[string]bool{}
	for _, term := range all {
		if !seen[term.MonthKey] {
			seen[term.MonthKey] = true
			order = append(order, term.MonthKey)
		}
	}
	// A YYYY-MM key sorts chronologically as a plain string, so newest first is
	// a reverse sort.
	sort.Sort(sort.Reverse(sort.StringSlice(order)))

	out := make([]MonthSummary, 0, len(order))
	for _, key := range order {
		first, count := firstOfMonth(all, key)
		out = append(out, s.summary(key, first, count))
	}
	return out, nil
}

// Get returns one term by id. The id is normalized to lowercase because every
// source title is, so a hand-typed id still resolves.
func (s *Service) Get(ctx context.Context, id string) (Term, error) {
	id = strings.ToLower(strings.TrimSpace(id))
	if id == "" || containsNUL(id) {
		return Term{}, ErrNotFound
	}
	return s.store.Get(ctx, id)
}

// Sync lists the source repo, fetches every month file, parses each one, and
// replaces the whole snapshot atomically. A failure at any step aborts the run
// and leaves the previous snapshot untouched, so a bad day upstream never
// blanks a month.
func (s *Service) Sync(ctx context.Context) (int, error) {
	locations, err := s.listMonths(ctx)
	if err != nil {
		return 0, err
	}
	if len(locations) == 0 {
		// The repo answered but carries no month file: upstream moved or renamed
		// them. Aborting keeps yesterday's snapshot instead of emptying the
		// journal.
		return 0, errors.New("dailyterm: source lists no month files")
	}

	all := make([]Term, 0, len(locations)*20)
	for _, loc := range locations {
		terms, err := s.fetchMonth(ctx, loc)
		if err != nil {
			return 0, err
		}
		all = append(all, terms...)
	}
	if err := s.store.ReplaceAll(ctx, all); err != nil {
		return 0, fmt.Errorf("dailyterm: sync: %w", err)
	}
	return len(all), nil
}

// resolveMonth turns an optional requested month into the key to serve. An
// empty request is the current server month when that month exists, otherwise
// the newest month available.
func (s *Service) resolveMonth(ctx context.Context, requested string) (string, error) {
	if requested != "" {
		year, month, err := parseMonthKey(requested)
		if err != nil {
			return "", err
		}
		return monthKey(year, month), nil
	}

	summaries, err := s.Months(ctx)
	if err != nil {
		return "", err
	}
	if len(summaries) == 0 {
		return "", ErrNotFound
	}
	now := s.now()
	if current := monthKey(now.Year(), now.Month()); summaries[0].MonthKey == current {
		return current, nil
	}
	for _, summary := range summaries {
		if summary.MonthKey == monthKey(now.Year(), now.Month()) {
			return summary.MonthKey, nil
		}
	}
	return summaries[0].MonthKey, nil
}

// meta builds the list metadata for one month from one of its terms.
func (s *Service) meta(key string, sample Term, count int) ListMeta {
	return ListMeta{
		MonthKey:   key,
		Name:       monthNames[time.Month(sample.Month)],
		Year:       sample.Year,
		Month:      sample.Month,
		SourceFile: sample.SourceFile,
		SourceURL:  sample.SourceURL,
		Count:      count,
	}
}

// summary builds one month row from one of its terms.
func (s *Service) summary(key string, sample Term, count int) MonthSummary {
	return MonthSummary{
		MonthKey:   key,
		Name:       monthNames[time.Month(sample.Month)],
		Year:       sample.Year,
		Month:      sample.Month,
		SourceFile: sample.SourceFile,
		SourceURL:  sample.SourceURL,
		Count:      count,
	}
}

// firstOfMonth returns the first term of a month plus how many that month
// holds. Callers pass terms already filtered by month or the whole snapshot.
func firstOfMonth(terms []Term, key string) (Term, int) {
	first := Term{}
	count := 0
	for _, term := range terms {
		if term.MonthKey != key {
			continue
		}
		if count == 0 {
			first = term
		}
		count++
	}
	return first, count
}

// listMonths asks GitHub for every path in the repo and keeps the month files.
// The result is ordered by year and month so a sync is deterministic. A month
// claimed by two files (a future "jul-term.md" next to "july-term.md") is kept
// once, in path order, so a month is never half-read.
func (s *Service) listMonths(ctx context.Context) ([]monthLocation, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.source.treeURL, nil)
	if err != nil {
		return nil, fmt.Errorf("dailyterm: build tree request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := s.source.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("dailyterm: fetch tree: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("dailyterm: fetch tree: status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxTreeBytes+1))
	if err != nil {
		return nil, fmt.Errorf("dailyterm: read tree: %w", err)
	}
	if len(body) > maxTreeBytes {
		return nil, errors.New("dailyterm: tree listing exceeds the size limit")
	}

	var listing struct {
		Truncated bool `json:"truncated"`
		Tree      []struct {
			Path string `json:"path"`
		} `json:"tree"`
	}
	if err := json.Unmarshal(body, &listing); err != nil {
		return nil, fmt.Errorf("dailyterm: decode tree: %w", err)
	}
	if listing.Truncated {
		// A truncated listing can silently omit a month, which is worse than a
		// failed sync: the snapshot would lose that month until the next run.
		return nil, errors.New("dailyterm: tree listing is truncated")
	}

	seen := map[string]bool{}
	locations := make([]monthLocation, 0, 24)
	for _, entry := range listing.Tree {
		if len(entry.Path) > maxPathBytes {
			continue
		}
		loc, ok := s.monthFile(entry.Path)
		if !ok || seen[loc.file] {
			continue
		}
		seen[loc.file] = true
		locations = append(locations, loc)
	}
	sort.Slice(locations, func(i, j int) bool {
		if locations[i].year != locations[j].year {
			return locations[i].year < locations[j].year
		}
		if locations[i].month != locations[j].month {
			return locations[i].month < locations[j].month
		}
		return locations[i].file < locations[j].file
	})
	return locations, nil
}

// monthFile reads one tree path into a location. A path that is not a month
// file, or whose month token upstream has never used, reports false so the sync
// skips it rather than guessing a month.
func (s *Service) monthFile(path string) (monthLocation, bool) {
	match := monthFilePattern.FindStringSubmatch(path)
	if match == nil {
		return monthLocation{}, false
	}
	year, err := parseDigits(match[1])
	if err != nil {
		return monthLocation{}, false
	}
	month, ok := monthTokens[match[2]]
	if !ok {
		return monthLocation{}, false
	}
	return monthLocation{year: year, month: month, file: path, pageBase: s.source.pageBase}, true
}

// fetchMonth downloads one month file and parses its terms.
func (s *Service) fetchMonth(ctx context.Context, loc monthLocation) ([]Term, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.source.rawBase+"/"+loc.file, nil)
	if err != nil {
		return nil, fmt.Errorf("dailyterm: build %s request: %w", loc.file, err)
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := s.source.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("dailyterm: fetch %s: %w", loc.file, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("dailyterm: fetch %s: status %d", loc.file, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxFileBytes+1))
	if err != nil {
		return nil, fmt.Errorf("dailyterm: read %s: %w", loc.file, err)
	}
	if len(body) > maxFileBytes {
		return nil, fmt.Errorf("dailyterm: %s exceeds the size limit", loc.file)
	}
	return parseMonth(bytes.NewReader(body), loc)
}
