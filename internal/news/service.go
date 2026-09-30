package news

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const (
	// defaultListingURL is the blog index the digest lives on.
	defaultListingURL = "https://www.zainfathoni.com/blog"

	// defaultFilter keeps only the AI Tools Digest posts from the listing.
	defaultFilter = "AI Tools Digest"

	// maxListingBytes caps the listing download. The page is around 140 KiB.
	maxListingBytes = 2 << 20

	// fetchTimeout bounds one listing fetch.
	fetchTimeout = 20 * time.Second

	// userAgent identifies this backend to the blog host.
	userAgent = "sweg-ai-lists-backend (+https://github.com/swegrowthid/sweg-ai-lists-backend)"
)

// Store is the persistence port. Service depends on it, never on SQL.
type Store interface {
	// Upsert stores one entry keyed by URL: a new URL is inserted, a known
	// URL updates the fields that changed. It returns the stored row.
	Upsert(ctx context.Context, input UpsertInput) (Entry, error)

	// List returns every entry, newest published first.
	List(ctx context.Context) ([]Entry, error)
}

// source describes the listing page the digest entries come from.
type source struct {
	listingURL string
	filter     string
	client     *http.Client
}

// Service owns the news use-cases. No HTTP, no SQL here.
type Service struct {
	store  Store
	source source
}

// NewService wires the store to the live blog source. Nil store is a
// programmer bug, so panic early.
func NewService(store Store) *Service {
	return newServiceWithSource(store, source{})
}

// newServiceWithSource fills the source defaults and wires the store. Tests
// point it at an httptest server; production uses NewService.
func newServiceWithSource(store Store, src source) *Service {
	if store == nil {
		panic("news: nil Store")
	}
	if src.listingURL == "" {
		src.listingURL = defaultListingURL
	}
	if src.filter == "" {
		src.filter = defaultFilter
	}
	if src.client == nil {
		src.client = &http.Client{Timeout: fetchTimeout}
	}
	return &Service{store: store, source: src}
}

// List returns every stored entry, newest published first.
func (s *Service) List(ctx context.Context) ([]Entry, error) {
	return s.store.List(ctx)
}

// Sync fetches the listing page, keeps the cards whose title contains the
// filter, and upserts each into the store. It returns how many entries were
// stored. A fetch or parse failure returns an error and leaves the stored
// list unchanged; a failed upsert stops the run and the next sync heals it,
// because every upsert is keyed by URL.
func (s *Service) Sync(ctx context.Context) (int, error) {
	entries, err := s.fetchListing(ctx)
	if err != nil {
		return 0, err
	}
	for _, entry := range entries {
		if _, err := s.store.Upsert(ctx, UpsertInput{
			Title:       entry.Title,
			URL:         entry.URL,
			Summary:     entry.Summary,
			PublishedAt: entry.PublishedAt,
		}); err != nil {
			return 0, fmt.Errorf("news: sync: %w", err)
		}
	}
	return len(entries), nil
}

// fetchListing downloads the listing page and parses the filtered entries.
func (s *Service) fetchListing(ctx context.Context) ([]Entry, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.source.listingURL, nil)
	if err != nil {
		return nil, fmt.Errorf("news: build listing request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := s.source.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("news: fetch listing: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("news: fetch listing: status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxListingBytes+1))
	if err != nil {
		return nil, fmt.Errorf("news: read listing: %w", err)
	}
	if len(body) > maxListingBytes {
		return nil, errors.New("news: listing page exceeds the size limit")
	}

	// The listing URL is the base for the page's relative links.
	base, err := url.Parse(s.source.listingURL)
	if err != nil {
		return nil, fmt.Errorf("news: parse listing url: %w", err)
	}
	return parseListing(bytes.NewReader(body), base, s.source.filter)
}
