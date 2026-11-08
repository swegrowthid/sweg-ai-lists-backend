package tools

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

const (
	// defaultRawBase is where the source markdown files are fetched from.
	defaultRawBase = "https://raw.githubusercontent.com/swegrowthid/tools-ai-swe-growth/main"

	// defaultPageBase is the human-readable source root for category links.
	defaultPageBase = "https://github.com/swegrowthid/tools-ai-swe-growth/blob/main"

	// maxFileBytes caps one source file download. The largest file is ~60 KiB.
	maxFileBytes = 1 << 20

	// fetchTimeout bounds one file fetch.
	fetchTimeout = 20 * time.Second

	// userAgent identifies this backend to the file host.
	userAgent = "sweg-ai-lists-backend (+https://github.com/swegrowthid/sweg-ai-lists-backend)"
)

// Store is the snapshot port. Service depends on it, never on a concrete
// store. The catalog is read-only for clients: writes happen only inside
// Sync, through ReplaceAll.
type Store interface {
	// ReplaceAll swaps the whole snapshot atomically.
	ReplaceAll(ctx context.Context, tools []Tool) error

	// List returns the snapshot in catalog order, filtered.
	List(ctx context.Context, filter ListFilter) ([]Tool, error)

	// Get returns one tool by id, or ErrNotFound.
	Get(ctx context.Context, id string) (Tool, error)
}

// source describes where the markdown files come from.
type source struct {
	rawBase  string
	pageBase string
	client   *http.Client
}

// Service owns the tools use-cases. No HTTP, no SQL here.
type Service struct {
	store  Store
	votes  VoteStore
	source source
}

// NewService wires the store to the live GitHub source. Nil store is a
// programmer bug, so panic early. Votes start empty in memory; production
// replaces them through NewServiceWithVotes.
func NewService(store Store) *Service {
	svc := newServiceWithSource(store, source{})
	svc.votes = NewMemoryVoteStore()
	return svc
}

// NewServiceWithVotes wires the snapshot store and the vote store together.
// A nil vote store falls back to memory. Nil snapshot store is a programmer
// bug, so panic early.
func NewServiceWithVotes(store Store, votes VoteStore) *Service {
	if store == nil {
		panic("tools: nil Store")
	}
	if votes == nil {
		votes = NewMemoryVoteStore()
	}
	svc := newServiceWithSource(store, source{})
	svc.votes = votes
	return svc
}

// newServiceWithSource fills the source defaults and wires the store. Tests
// point it at an httptest server; production uses NewService.
func newServiceWithSource(store Store, src source) *Service {
	if store == nil {
		panic("tools: nil Store")
	}
	if src.rawBase == "" {
		src.rawBase = defaultRawBase
	}
	if src.pageBase == "" {
		src.pageBase = defaultPageBase
	}
	if src.client == nil {
		src.client = &http.Client{Timeout: fetchTimeout}
	}
	return &Service{store: store, votes: NewMemoryVoteStore(), source: src}
}

// List validates the filter, orders the snapshot as asked, and returns one
// page of it together with the metadata a client needs to fetch the next one.
// It is the anonymous-reader form of ListFor: votes are counted, voted stays
// false everywhere.
func (s *Service) List(ctx context.Context, filter ListFilter) (ListResult, error) {
	return s.ListFor(ctx, filter, "")
}

// ListFor is List with a viewer: the vote count is filled for every row and
// voted marks the rows the viewer voted for. An empty viewerID means an
// anonymous reader.
func (s *Service) ListFor(ctx context.Context, filter ListFilter, viewerID string) (ListResult, error) {
	if strings.ContainsRune(filter.Query, 0) || strings.ContainsRune(filter.Category, 0) {
		return ListResult{}, ErrInvalidInput
	}
	slug, ok := resolveCategory(filter.Category)
	if !ok {
		return ListResult{}, ErrUnknownCategory
	}
	filter.Category = slug
	if err := normalizeListOptions(&filter); err != nil {
		return ListResult{}, err
	}
	filter.Query = strings.ToLower(strings.TrimSpace(filter.Query))

	// The snapshot store holds no votes, so sort=votes must not reach it:
	// fetch catalog order, enrich, then sort here.
	sortVotes := filter.Sort == SortVotes
	storeFilter := filter
	if sortVotes {
		storeFilter.Sort = ""
		storeFilter.Order = ""
	}

	rows, err := s.store.List(ctx, storeFilter)
	if err != nil {
		return ListResult{}, err
	}
	if len(rows) > 0 {
		sums, err := s.voteStore().Summaries(ctx, viewerID, toolIDsOf(rows))
		if err != nil {
			return ListResult{}, err
		}
		for i := range rows {
			if sum, ok := sums[rows[i].ID]; ok {
				rows[i].Votes = sum.Votes
				rows[i].Voted = sum.Voted
			}
		}
	}
	if sortVotes {
		sortVoteRows(rows, filter.Order)
	}
	return pageRows(rows, filter.Page, filter.PerPage), nil
}

// Get returns one tool by id, case-insensitive (p-001 finds P-001).
// It is the anonymous-reader form of GetFor.
func (s *Service) Get(ctx context.Context, id string) (Tool, error) {
	return s.GetFor(ctx, id, "")
}

// GetFor is Get with a viewer: votes holds the total count and voted marks
// whether the viewer voted for the tool.
func (s *Service) GetFor(ctx context.Context, id, viewerID string) (Tool, error) {
	id = strings.ToUpper(strings.TrimSpace(id))
	if id == "" || strings.ContainsRune(id, 0) {
		return Tool{}, ErrNotFound
	}
	tool, err := s.store.Get(ctx, id)
	if err != nil {
		return Tool{}, err
	}
	sums, err := s.voteStore().Summaries(ctx, viewerID, []string{tool.ID})
	if err != nil {
		return Tool{}, err
	}
	if sum, ok := sums[tool.ID]; ok {
		tool.Votes = sum.Votes
		tool.Voted = sum.Voted
	}
	return tool, nil
}

// Vote records one usage vote. The tool id is uppercased and trimmed, then
// checked against the snapshot; an unknown id answers ErrNotFound. An empty
// user id answers ErrInvalidInput. Created is false for a duplicate vote.
// An unknown user id answers ErrUnknownUser.
func (s *Service) Vote(ctx context.Context, userID, toolID string) (VoteResult, bool, error) {
	if strings.TrimSpace(userID) == "" || strings.ContainsRune(userID, 0) {
		return VoteResult{}, false, ErrInvalidInput
	}
	toolID = strings.ToUpper(strings.TrimSpace(toolID))
	if toolID == "" || strings.ContainsRune(toolID, 0) {
		return VoteResult{}, false, ErrNotFound
	}
	if _, err := s.store.Get(ctx, toolID); err != nil {
		return VoteResult{}, false, err
	}
	created, err := s.voteStore().Add(ctx, userID, toolID)
	if err != nil {
		return VoteResult{}, false, err
	}
	return s.voteResult(ctx, userID, toolID, true), created, nil
}

// Unvote withdraws one usage vote. It validates like Vote but never fails
// when no vote exists: removing a missing vote still answers the latest
// summary with voted false.
func (s *Service) Unvote(ctx context.Context, userID, toolID string) (VoteResult, error) {
	if strings.TrimSpace(userID) == "" || strings.ContainsRune(userID, 0) {
		return VoteResult{}, ErrInvalidInput
	}
	toolID = strings.ToUpper(strings.TrimSpace(toolID))
	if toolID == "" || strings.ContainsRune(toolID, 0) {
		return VoteResult{}, ErrNotFound
	}
	if _, err := s.store.Get(ctx, toolID); err != nil {
		return VoteResult{}, err
	}
	if _, err := s.voteStore().Remove(ctx, userID, toolID); err != nil {
		return VoteResult{}, err
	}
	return s.voteResult(ctx, userID, toolID, false), nil
}

// voteResult reads the latest summary for one tool after a vote mutation.
// fallbackVoted covers a store that answers no row for the id.
func (s *Service) voteResult(ctx context.Context, userID, toolID string, fallbackVoted bool) VoteResult {
	result := VoteResult{ToolID: toolID, Voted: fallbackVoted}
	sums, err := s.voteStore().Summaries(ctx, userID, []string{toolID})
	if err != nil {
		return result
	}
	if sum, ok := sums[toolID]; ok {
		result.Votes = sum.Votes
		result.Voted = sum.Voted
	}
	return result
}

// voteStore returns the vote port, defaulting to memory when unset.
func (s *Service) voteStore() VoteStore {
	if s.votes == nil {
		s.votes = NewMemoryVoteStore()
	}
	return s.votes
}

// toolIDsOf collects the snapshot ids for one Summaries call.
func toolIDsOf(rows []Tool) []string {
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	return ids
}

// sortVoteRows orders enriched rows by vote count, tie-broken by id ascending
// so a page boundary never repeats or drops a row.
func sortVoteRows(rows []Tool, order string) {
	desc := order != OrderAsc
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Votes != rows[j].Votes {
			if desc {
				return rows[i].Votes > rows[j].Votes
			}
			return rows[i].Votes < rows[j].Votes
		}
		return rows[i].ID < rows[j].ID
	})
}

// Categories returns the static category mapping with live row counts.
func (s *Service) Categories(ctx context.Context) ([]Category, error) {
	all, err := s.store.List(ctx, ListFilter{})
	if err != nil {
		return nil, err
	}
	counts := map[string]int{}
	for _, tool := range all {
		counts[tool.Category]++
	}
	out := make([]Category, 0, len(categories))
	for _, def := range categories {
		out = append(out, Category{
			Slug:       def.slug,
			Name:       def.name,
			Prefix:     def.prefix,
			SourceFile: def.file,
			SourceURL:  s.source.pageBase + "/" + def.file,
			Count:      counts[def.slug],
		})
	}
	return out, nil
}

// Sync fetches every source file, parses its table, and replaces the whole
// snapshot atomically. A fetch or parse failure aborts the run and leaves
// the previous snapshot untouched: a missing file never blanks a category.
func (s *Service) Sync(ctx context.Context) (int, error) {
	all := []Tool{}
	for _, cat := range categories {
		parsed, err := s.fetchFile(ctx, cat)
		if err != nil {
			return 0, err
		}
		all = append(all, parsed...)
	}
	if err := s.store.ReplaceAll(ctx, all); err != nil {
		return 0, fmt.Errorf("tools: sync: %w", err)
	}
	return len(all), nil
}

// fetchFile downloads one source file and parses its data table.
func (s *Service) fetchFile(ctx context.Context, cat categoryDef) ([]Tool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.source.rawBase+"/"+cat.file, nil)
	if err != nil {
		return nil, fmt.Errorf("tools: build %s request: %w", cat.file, err)
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := s.source.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("tools: fetch %s: %w", cat.file, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tools: fetch %s: status %d", cat.file, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxFileBytes+1))
	if err != nil {
		return nil, fmt.Errorf("tools: read %s: %w", cat.file, err)
	}
	if len(body) > maxFileBytes {
		return nil, fmt.Errorf("tools: %s exceeds the size limit", cat.file)
	}
	return parseTools(bytes.NewReader(body), cat)
}

// resolveCategory maps a group name, a category slug, or an id prefix to the
// category slug the store filters on. Every spelling is case-insensitive, so
// "?group=coding_agent" and "?category=ca" select the same rows. "all" and the
// empty string mean no filter at all.
func resolveCategory(raw string) (string, bool) {
	raw = strings.ToLower(strings.TrimSpace(raw))
	switch raw {
	case "", "all":
		return "", true
	case "provider":
		return "providers", true
	case "coding-agent", "coding_agent", "coding agent":
		return "coding-agents", true
	}
	for _, def := range categories {
		if raw == def.slug || raw == strings.ToLower(def.prefix) {
			return def.slug, true
		}
	}
	return "", false
}

// normalizeListOptions fills the paging defaults and rejects a sort, order, or
// page size the catalog cannot serve. A lone Order is dropped: there is no key
// to order by yet.
func normalizeListOptions(filter *ListFilter) error {
	switch filter.Sort {
	case "":
		filter.Order = ""
	case SortName, SortUpdated:
		if filter.Order == "" {
			filter.Order = OrderAsc
		}
		if filter.Order != OrderAsc && filter.Order != OrderDesc {
			return ErrInvalidInput
		}
	case SortVotes:
		if filter.Order == "" {
			filter.Order = OrderDesc
		}
		if filter.Order != OrderAsc && filter.Order != OrderDesc {
			return ErrInvalidInput
		}
	default:
		return ErrInvalidInput
	}

	if filter.Page == 0 {
		filter.Page = 1
	}
	if filter.Page < 1 {
		return ErrInvalidInput
	}
	if filter.PerPage == 0 {
		filter.PerPage = DefaultPerPage
	}
	if filter.PerPage < 1 || filter.PerPage > MaxPerPage {
		return ErrInvalidInput
	}
	return nil
}

// pageRows cuts one page out of the ordered rows and reports the metadata for
// the whole result set. A page past the end is empty but still carries the
// total, so the client can tell "no more rows" from "nothing matched".
func pageRows(rows []Tool, page, perPage int) ListResult {
	total := len(rows)
	meta := PageMeta{
		Page:       page,
		PerPage:    perPage,
		Total:      total,
		TotalPages: (total + perPage - 1) / perPage,
	}
	// The early return keeps a huge page number from overflowing the slice
	// offsets below.
	if page > meta.TotalPages {
		return ListResult{Data: []Tool{}, Meta: meta}
	}
	start := (page - 1) * perPage
	end := start + perPage
	if end > total {
		end = total
	}
	return ListResult{Data: rows[start:end], Meta: meta}
}
