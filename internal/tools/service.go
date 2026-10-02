package tools

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
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
	source source
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
	return &Service{store: store, source: src}
}

// List validates the filter, orders the snapshot as asked, and returns one
// page of it together with the metadata a client needs to fetch the next one.
func (s *Service) List(ctx context.Context, filter ListFilter) (ListResult, error) {
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

	rows, err := s.store.List(ctx, filter)
	if err != nil {
		return ListResult{}, err
	}
	return pageRows(rows, filter.Page, filter.PerPage), nil
}

// Get returns one tool by id, case-insensitive (p-001 finds P-001).
func (s *Service) Get(ctx context.Context, id string) (Tool, error) {
	id = strings.ToUpper(strings.TrimSpace(id))
	if id == "" || strings.ContainsRune(id, 0) {
		return Tool{}, ErrNotFound
	}
	return s.store.Get(ctx, id)
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
