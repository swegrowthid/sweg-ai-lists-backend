package news

import (
	"bytes"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

// fixtureHTML is the trimmed real listing page: six cards, four of them AI
// Tools Digest posts, one with a nested URL and one with HTML entities.
func fixtureHTML(t *testing.T) []byte {
	t.Helper()
	page, err := os.ReadFile("testdata/blog_listing.html")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return page
}

// fixtureBase is the URL the fixture's relative links resolve against.
func fixtureBase(t *testing.T) *url.URL {
	t.Helper()
	base, err := url.Parse(defaultListingURL)
	if err != nil {
		t.Fatalf("parse base: %v", err)
	}
	return base
}

func TestParseListingKeepsDigestCardsInPageOrder(t *testing.T) {
	entries, err := parseListing(bytes.NewReader(fixtureHTML(t)), fixtureBase(t), defaultFilter)
	if err != nil {
		t.Fatalf("parseListing() error = %v", err)
	}
	wantTitles := []string{
		"Pintar, Murah, atau Terlihat Kerjanya? — AI Tools Digest #35",
		"Jev Datang, Router Memanas, Memory Makin Rumit — AI Tools Digest #34",
		"AI Tools Digest: Minggu Ini Anthropic Main Blokir, AmpCode Ganti Haluan, dan Pi.dev Mulai Naik Daun",
		"AI Tools Digest: Week 2 - Model Tier Wars & The Great Price Negotiation",
	}
	if len(entries) != len(wantTitles) {
		t.Fatalf("len(entries) = %d, want %d: %+v", len(entries), len(wantTitles), entries)
	}
	for index, want := range wantTitles {
		if entries[index].Title != want {
			t.Fatalf("entries[%d].Title = %q, want %q", index, entries[index].Title, want)
		}
	}

	first := entries[0]
	if first.URL != "https://www.zainfathoni.com/blog/ai-tools-swe-growth-sep-20-sep-27-2026" {
		t.Fatalf("entries[0].URL = %q, want the absolute post URL", first.URL)
	}
	if first.Summary != "Opus 5.5, Codex, Jev, Amp runner, dan agent yang bisa diawasi. Rangkuman dari grup AI Tools SWE GROWTH, 20–27 Sep 2026." {
		t.Fatalf("entries[0].Summary = %q", first.Summary)
	}
	if want := time.Date(2026, 9, 27, 0, 0, 0, 0, time.UTC); !first.PublishedAt.Equal(want) {
		t.Fatalf("entries[0].PublishedAt = %v, want %v", first.PublishedAt, want)
	}

	// A card whose link carries a date folder still resolves under the base,
	// and HTML entities come back unescaped.
	last := entries[3]
	if last.URL != "https://www.zainfathoni.com/blog/2026-02-15/ai-tools-digest-week-2" {
		t.Fatalf("entries[3].URL = %q, want the nested post URL", last.URL)
	}
	if !strings.Contains(last.Summary, "Kimi's negotiation gamification") {
		t.Fatalf("entries[3].Summary = %q, want the unescaped apostrophe", last.Summary)
	}
	if want := time.Date(2026, 2, 15, 0, 0, 0, 0, time.UTC); !last.PublishedAt.Equal(want) {
		t.Fatalf("entries[3].PublishedAt = %v, want %v", last.PublishedAt, want)
	}
}

func TestParseListingFilterIsCaseInsensitive(t *testing.T) {
	entries, err := parseListing(bytes.NewReader(fixtureHTML(t)), fixtureBase(t), "ai tools digest")
	if err != nil {
		t.Fatalf("parseListing() error = %v", err)
	}
	if len(entries) != 4 {
		t.Fatalf("len(entries) = %d, want 4", len(entries))
	}
}

func TestParseListingReturnsNoEntriesWhenNothingMatches(t *testing.T) {
	entries, err := parseListing(bytes.NewReader(fixtureHTML(t)), fixtureBase(t), "kata-yang-tidak-ada")
	if err != nil {
		t.Fatalf("parseListing() error = %v", err)
	}
	if entries == nil {
		t.Fatal("parseListing() = nil, want an empty slice")
	}
	if len(entries) != 0 {
		t.Fatalf("len(entries) = %d, want 0", len(entries))
	}
}

func TestParseListingFailsOnPageWithoutArticles(t *testing.T) {
	page := "<html><body><p>kosong</p></body></html>"
	if _, err := parseListing(strings.NewReader(page), fixtureBase(t), defaultFilter); err == nil {
		t.Fatal("parseListing() error = nil, want a failure")
	}
}

func TestParseListingSkipsCardsWithoutRequiredFields(t *testing.T) {
	page := `
	<article>
		<h2><a href="/blog/lengkap">AI Tools Digest #99 — lengkap</a></h2>
		<time dateTime="2026-09-28">September 28, 2026</time>
		<p>Ringkasan lengkap.</p>
	</article>
	<article>
		<h2><a href="/blog/tanpa-tanggal">AI Tools Digest #98 — tanpa tanggal</a></h2>
		<p>Ringkasan.</p>
	</article>
	<article>
		<h2><a href="/blog/kosong"></a></h2>
		<time dateTime="2026-09-27">September 27, 2026</time>
	</article>
	<article>
		<h2><a href="/blog/ringkas">AI Tools Digest #97 — tanpa ringkasan</a></h2>
		<time dateTime="2026-09-26">September 26, 2026</time>
	</article>`
	base, err := url.Parse("https://example.com/blog")
	if err != nil {
		t.Fatalf("parse base: %v", err)
	}

	entries, err := parseListing(strings.NewReader(page), base, defaultFilter)
	if err != nil {
		t.Fatalf("parseListing() error = %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("len(entries) = %d, want the two complete cards: %+v", len(entries), entries)
	}
	if entries[0].URL != "https://example.com/blog/lengkap" || entries[0].Summary != "Ringkasan lengkap." {
		t.Fatalf("entries[0] = %+v, want the complete card", entries[0])
	}
	if entries[1].URL != "https://example.com/blog/ringkas" || entries[1].Summary != "" {
		t.Fatalf("entries[1] = %+v, want the card without a summary", entries[1])
	}
	if want := time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC); !entries[0].PublishedAt.Equal(want) {
		t.Fatalf("entries[0].PublishedAt = %v, want %v", entries[0].PublishedAt, want)
	}
}
