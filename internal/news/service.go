package news

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"time"
)

const (
	// defaultListingURL is the blog index the digest lives on.
	defaultListingURL = "https://www.zainfathoni.com/blog"

	// defaultFilter keeps only the AI Tools Digest posts from the listing.
	defaultFilter = "AI Tools Digest"

	// maxListingBytes caps the listing download. The page is around 140 KiB.
	maxListingBytes = 2 << 20

	// fetchTimeout bounds one source fetch.
	fetchTimeout = 20 * time.Second

	// userAgent identifies this backend to the source hosts.
	userAgent = "sweg-ai-lists-backend (+https://github.com/swegrowthid/sweg-ai-lists-backend)"

	// kinds of source: an HTML listing or a syndication feed.
	kindListing = "listing"
	kindFeed    = "feed"

	// maxFeedItems caps how many entries one feed may contribute per sync.
	// Some official feeds ship their whole archive (openai.com/news/rss.xml
	// carried 1393 items back to 2015); the digest is a news surface, so a
	// feed only feeds its newest window.
	maxFeedItems = 100
)

// Store is the persistence port. Service depends on it, never on SQL.
type Store interface {
	// Upsert stores one entry keyed by URL: a new URL is inserted, a known
	// URL updates the fields that changed. It returns the stored row.
	Upsert(ctx context.Context, input UpsertInput) (Entry, error)

	// List returns every entry, newest published first.
	List(ctx context.Context) ([]Entry, error)
}

// source describes one ingest source: a display name, what kind of document
// it serves, and where to fetch it.
type source struct {
	name       string
	kind       string
	listingURL string
	filter     string
	client     *http.Client
}

// Service owns the news use-cases. No HTTP, no SQL here.
type Service struct {
	store   Store
	sources []source
}

// NewService wires the store to the default source registry. Nil store is a
// programmer bug, so panic early.
func NewService(store Store) *Service {
	return newServiceWithSource(store, defaultSources())
}

// defaultSources lists the ingest sources in sync order: the AI news feeds
// first, then the zainfathoni.com listing.
func defaultSources() []source {
	return []source{
		{name: "TechCrunch", kind: kindFeed, listingURL: "https://techcrunch.com/category/artificial-intelligence/feed/"},
		{name: "The Verge", kind: kindFeed, listingURL: "https://www.theverge.com/rss/ai-artificial-intelligence/index.xml"},
		{name: "The Decoder", kind: kindFeed, listingURL: "https://the-decoder.com/feed/"},
		{name: "Ars Technica", kind: kindFeed, listingURL: "https://arstechnica.com/ai/feed/"},
		{name: "MarkTechPost", kind: kindFeed, listingURL: "https://www.marktechpost.com/feed/"},
		{name: "OpenAI", kind: kindFeed, listingURL: "https://openai.com/news/rss.xml"},
		{name: "Google DeepMind", kind: kindFeed, listingURL: "https://deepmind.google/blog/rss.xml"},
		{name: "TLDR AI", kind: kindFeed, listingURL: "https://tldr.tech/api/rss/ai"},
		{name: "zainfathoni.com", kind: kindListing, listingURL: defaultListingURL, filter: defaultFilter},
	}
}

// newServiceWithSource wires the store to a source list. Tests point it at an
// httptest server; production uses NewService.
func newServiceWithSource(store Store, sources []source) *Service {
	if store == nil {
		panic("news: nil Store")
	}
	if len(sources) == 0 {
		sources = defaultSources()
	}
	clients := map[*http.Client]bool{}
	filled := make([]source, len(sources))
	for index, src := range sources {
		if src.kind != kindListing && src.kind != kindFeed {
			panic(fmt.Sprintf("news: unknown source kind %q", src.kind))
		}
		if src.client == nil {
			src.client = &http.Client{Timeout: fetchTimeout}
		}
		if src.kind == kindListing {
			if src.listingURL == "" {
				src.listingURL = defaultListingURL
			}
			if src.filter == "" {
				src.filter = defaultFilter
			}
		}
		clients[src.client] = true
		filled[index] = src
	}
	return &Service{store: store, sources: filled}
}

// List returns every stored entry, newest published first.
func (s *Service) List(ctx context.Context) ([]Entry, error) {
	return s.store.List(ctx)
}

// Sync fetches every source, parses it, and upserts each entry. The sources
// are isolated: a failure of one source is collected and never stops the
// others. It returns how many entries were stored across the successful
// sources and a joined error naming every failing source. Partial success is
// expected: the next sync heals a failed source, because every upsert is
// keyed by URL.
func (s *Service) Sync(ctx context.Context) (int, error) {
	stored := 0
	errs := []error{}
	for _, src := range s.sources {
		count, err := s.syncSource(ctx, src)
		if err != nil {
			errs = append(errs, fmt.Errorf("news: source %s: %w", src.name, err))
		}
		stored += count
	}
	return stored, errors.Join(errs...)
}

// syncSource fetches, parses, and upserts one source. It returns how many
// entries of this source were stored.
func (s *Service) syncSource(ctx context.Context, src source) (int, error) {
	fetchCtx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()

	entries, err := s.fetchSource(fetchCtx, src)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, entry := range entries {
		if _, err := s.store.Upsert(fetchCtx, UpsertInput{
			Title:       entry.Title,
			URL:         entry.URL,
			Source:      src.name,
			Summary:     entry.Summary,
			PublishedAt: entry.PublishedAt,
		}); err != nil {
			return count, fmt.Errorf("news: sync: %w", err)
		}
		count++
	}
	return count, nil
}

// fetchSource downloads one source document and parses it into entries.
func (s *Service) fetchSource(ctx context.Context, src source) ([]Entry, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, src.listingURL, nil)
	if err != nil {
		return nil, fmt.Errorf("news: build source request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := src.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("news: fetch source: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("news: fetch source: status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxListingBytes+1))
	if err != nil {
		return nil, fmt.Errorf("news: read source: %w", err)
	}
	if len(body) > maxListingBytes {
		return nil, errors.New("news: source page exceeds the size limit")
	}

	// The source URL is the base for the document's relative links.
	base, err := url.Parse(src.listingURL)
	if err != nil {
		return nil, fmt.Errorf("news: parse source url: %w", err)
	}
	switch src.kind {
	case kindListing:
		return parseListing(bytes.NewReader(body), base, src.filter)
	case kindFeed:
		inputs, err := parseFeed(bytes.NewReader(body), base)
		if err != nil {
			return nil, err
		}
		entries := make([]Entry, len(inputs))
		for index, input := range inputs {
			entries[index] = Entry{
				Title:       input.Title,
				URL:         input.URL,
				Summary:     input.Summary,
				PublishedAt: input.PublishedAt,
			}
		}
		return capNewest(entries, maxFeedItems), nil
	default:
		return nil, fmt.Errorf("news: unknown source kind %q", src.kind)
	}
}

// capNewest keeps the newest n entries and drops the rest, deterministic
// regardless of the order the feed listed them in.
func capNewest(entries []Entry, n int) []Entry {
	if len(entries) <= n {
		return entries
	}
	sorted := make([]Entry, len(entries))
	copy(sorted, entries)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].PublishedAt.After(sorted[j].PublishedAt)
	})
	return sorted[:n]
}
