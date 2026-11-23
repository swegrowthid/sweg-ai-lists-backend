package news

import (
	"net/url"
	"strings"
	"testing"
	"time"
)

// pubDate is the date line every well-formed RSS fixture item carries.
const pubDate = `<pubDate>Mon, 02 Jan 2006 15:04:05 -0700</pubDate>`

// mustParseURL builds a fixture base URL, failing the test on an error.
func mustParseURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("url.Parse(%q) error = %v", raw, err)
	}
	return parsed
}

func rssFixture(items ...string) string {
	return `<?xml version="1.0" encoding="UTF-8"?><rss version="2.0"><channel><title>Test Feed</title>` +
		strings.Join(items, "") + `</channel></rss>`
}

func atomFixture(entries ...string) string {
	return `<?xml version="1.0" encoding="UTF-8"?><feed xmlns="http://www.w3.org/2005/Atom">` +
		strings.Join(entries, "") + `</feed>`
}

func TestParseFeedRSS(t *testing.T) {
	body := rssFixture(
		`<item><title>First post</title><link>https://example.com/first</link><pubDate>Mon, 02 Jan 2006 15:04:05 -0700</pubDate><description>&lt;p&gt;Hello &lt;b&gt;world&lt;/b&gt;&lt;/p&gt;</description></item>`,
		`<item><title>Relative link</title><link>/second</link><pubDate>Tue, 03 Jan 2006 15:04:05 MST</pubDate><description>   spaced   out   </description></item>`,
	)
	base := mustParseURL(t, "https://example.com/feed.xml")
	entries, err := parseFeed(strings.NewReader(body), base)
	if err != nil {
		t.Fatalf("parseFeed() error = %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2", len(entries))
	}
	first := entries[0]
	if first.Title != "First post" || first.URL != "https://example.com/first" {
		t.Fatalf("first = %+v", first)
	}
	if first.Summary != "Hello world" {
		t.Fatalf("first.Summary = %q, want the stripped HTML", first.Summary)
	}
	wantTime := time.Date(2006, 1, 2, 15, 4, 5, 0, time.FixedZone("", -7*60*60)).UTC()
	if !first.PublishedAt.Equal(wantTime) {
		t.Fatalf("first.PublishedAt = %v, want %v", first.PublishedAt, wantTime)
	}
	second := entries[1]
	if second.URL != "https://example.com/second" {
		t.Fatalf("second.URL = %q, want the resolved relative link", second.URL)
	}
	if second.Summary != "spaced out" {
		t.Fatalf("second.Summary = %q, want collapsed whitespace", second.Summary)
	}
}

func TestParseFeedAtom(t *testing.T) {
	body := atomFixture(
		`<entry><title><![CDATA[Coded title]]></title><link rel="self" href="https://example.com/self"/><link rel="alternate" href="https://example.com/entry-1"/><updated>2026-01-02T15:04:05Z</updated><summary>&lt;em&gt;Summary&lt;/em&gt; text</summary></entry>`,
		`<entry><title>Plain link</title><link href="/entry-2"/><updated>2026-01-03T00:00:00+02:00</updated><content>Content beats an empty summary</content></entry>`,
	)
	base := mustParseURL(t, "https://example.com/atom.xml")
	entries, err := parseFeed(strings.NewReader(body), base)
	if err != nil {
		t.Fatalf("parseFeed() error = %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2", len(entries))
	}
	first := entries[0]
	if first.Title != "Coded title" || first.URL != "https://example.com/entry-1" {
		t.Fatalf("first = %+v", first)
	}
	if first.Summary != "Summary text" {
		t.Fatalf("first.Summary = %q, want the stripped HTML", first.Summary)
	}
	wantTime := time.Date(2026, 1, 2, 15, 4, 5, 0, time.UTC)
	if !first.PublishedAt.Equal(wantTime) {
		t.Fatalf("first.PublishedAt = %v, want %v", first.PublishedAt, wantTime)
	}
	second := entries[1]
	if second.URL != "https://example.com/entry-2" {
		t.Fatalf("second.URL = %q, want the first href resolved", second.URL)
	}
	if second.Summary != "Content beats an empty summary" {
		t.Fatalf("second.Summary = %q", second.Summary)
	}
}

func TestParseFeedAtomPrefersPublishedOverUpdated(t *testing.T) {
	body := atomFixture(
		`<entry><title>Edited post</title><link href="https://example.com/edited"/><published>2026-01-02T10:00:00Z</published><updated>2026-02-20T10:00:00Z</updated></entry>`,
		`<entry><title>Updated only</title><link href="https://example.com/updated"/><updated>2026-01-03T10:00:00Z</updated></entry>`,
	)
	base := mustParseURL(t, "https://example.com/atom.xml")
	entries, err := parseFeed(strings.NewReader(body), base)
	if err != nil {
		t.Fatalf("parseFeed() error = %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2", len(entries))
	}
	// An edit bumps <updated>; <published> keeps the original post date.
	if want := time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC); !entries[0].PublishedAt.Equal(want) {
		t.Fatalf("entries[0].PublishedAt = %v, want published %v not updated", entries[0].PublishedAt, want)
	}
	if want := time.Date(2026, 1, 3, 10, 0, 0, 0, time.UTC); !entries[1].PublishedAt.Equal(want) {
		t.Fatalf("entries[1].PublishedAt = %v, want the updated fallback %v", entries[1].PublishedAt, want)
	}
}

func TestParseFeedStripsTrackingParams(t *testing.T) {
	body := rssFixture(
		`<item><title>Tracked</title><link>https://example.com/post?utm_source=newsletter&amp;utm_medium=email&amp;id=42&amp;fbclid=abc</link>` + pubDate + `<description>x</description></item>`,
	)
	base := mustParseURL(t, "https://example.com/feed.xml")
	entries, err := parseFeed(strings.NewReader(body), base)
	if err != nil {
		t.Fatalf("parseFeed() error = %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("len(entries) = %d, want 1", len(entries))
	}
	if entries[0].URL != "https://example.com/post?id=42" {
		t.Fatalf("entries[0].URL = %q, want tracking params stripped and id kept", entries[0].URL)
	}
}

func TestTruncateSummaryCutsAtWordBoundary(t *testing.T) {
	text := strings.Repeat("word ", 300) // 1500 runes, spaces every 5
	got := truncateSummary(text, 100)
	if len([]rune(got)) > 100 {
		t.Fatalf("len = %d runes, want <= 100", len([]rune(got)))
	}
	if !strings.HasSuffix(got, "...") {
		t.Fatalf("summary = %q, want ellipsis tail", got[len(got)-10:])
	}
	if strings.HasSuffix(strings.TrimSuffix(got, "..."), "wor") {
		t.Fatal("summary cut mid-word")
	}
	// Short text passes through untouched.
	if truncateSummary("short", 100) != "short" {
		t.Fatal("truncateSummary changed a short string")
	}
}

func TestParseFeedSkipsBrokenItems(t *testing.T) {
	body := rssFixture(
		`<item><title>Missing link</title><pubDate>Mon, 02 Jan 2006 15:04:05 -0700</pubDate><description>x</description></item>`,
		`<item><title></title><link>https://example.com/empty-title</link><pubDate>Mon, 02 Jan 2006 15:04:05 -0700</pubDate></item>`,
		`<item><title>Missing date</title><link>https://example.com/no-date</link><description>x</description></item>`,
		`<item><title>Bad date</title><link>https://example.com/bad-date</link><pubDate>not a date</pubDate></item>`,
		`<item><title>Kept</title><link>https://example.com/kept</link><pubDate>Mon, 02 Jan 2006 15:04:05 -0700</pubDate></item>`,
	)
	base := mustParseURL(t, "https://example.com/feed.xml")
	entries, err := parseFeed(strings.NewReader(body), base)
	if err != nil {
		t.Fatalf("parseFeed() error = %v", err)
	}
	if len(entries) != 1 || entries[0].Title != "Kept" {
		t.Fatalf("entries = %+v, want only the kept item", entries)
	}
}

func TestParseFeedCaps(t *testing.T) {
	base := mustParseURL(t, "https://example.com/feed.xml")
	longTitle := `<item><title>` + strings.Repeat("x", 301) + `</title><link>https://example.com/t</link>` + pubDate + `</item>`
	longSummary := `<item><title>Ok</title><link>https://example.com/s</link>` + pubDate + `<description>` + strings.Repeat("s", 1001) + `</description></item>`
	longURL := `<item><title>Ok</title><link>https://example.com/` + strings.Repeat("u", 490) + `-way-over</link>` + pubDate + `</item>`

	for name, body := range map[string]string{
		"301-rune title": rssFixture(longTitle),
		"501-rune url":   rssFixture(longURL),
	} {
		t.Run(name, func(t *testing.T) {
			entries, err := parseFeed(strings.NewReader(body), base)
			if err != nil {
				t.Fatalf("parseFeed() error = %v", err)
			}
			if len(entries) != 0 {
				t.Fatalf("entries = %+v, want none", entries)
			}
		})
	}

	// A long summary truncates instead of dropping the whole item.
	entries, err := parseFeed(strings.NewReader(rssFixture(longSummary)), base)
	if err != nil {
		t.Fatalf("parseFeed() error = %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("len(entries) = %d, want the truncated item kept", len(entries))
	}
	if got := len([]rune(entries[0].Summary)); got > maxSummaryLen {
		t.Fatalf("summary runes = %d, want <= %d", got, maxSummaryLen)
	}
	if !strings.HasSuffix(entries[0].Summary, "...") {
		t.Fatalf("summary = %q, want a truncated tail", entries[0].Summary[len(entries[0].Summary)-10:])
	}
}

func TestParseFeedSniffsFormat(t *testing.T) {
	base := mustParseURL(t, "https://example.com/f")
	if _, err := parseFeed(strings.NewReader(`<html><body>nope</body></html>`), base); err == nil {
		t.Fatal("parseFeed() error = nil for an unknown root, want a format error")
	}
	// An Atom feed inside an envelope that is not <rss> parses as Atom.
	entries, err := parseFeed(strings.NewReader(atomFixture(
		`<entry><title>A</title><link href="https://example.com/a"/><updated>2026-01-02T15:04:05Z</updated></entry>`,
	)), base)
	if err != nil {
		t.Fatalf("parseFeed() error = %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("len(entries) = %d, want 1", len(entries))
	}
}

func TestFeedUpsertInputRejectsNUL(t *testing.T) {
	base := mustParseURL(t, "https://example.com/feed.xml")
	when := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	if _, ok := feedUpsertInput("title\x00", "https://example.com/nul", "", when, base); ok {
		t.Fatal("feedUpsertInput accepted a NUL in the title")
	}
	if _, ok := feedUpsertInput("title", "https://example.com/n\x00ul", "", when, base); ok {
		t.Fatal("feedUpsertInput accepted a NUL in the URL")
	}
	if _, ok := feedUpsertInput("title", "https://example.com/nul", "summary\x00", when, base); ok {
		t.Fatal("feedUpsertInput accepted a NUL in the summary")
	}
}

func TestParseFeedEmptyDescriptionIsAllowed(t *testing.T) {
	base := mustParseURL(t, "https://example.com/feed.xml")
	entries, err := parseFeed(strings.NewReader(rssFixture(
		`<item><title>Empty summary</title><link>https://example.com/e</link>`+pubDate+`</item>`,
	)), base)
	if err != nil {
		t.Fatalf("parseFeed() error = %v", err)
	}
	if len(entries) != 1 || entries[0].Summary != "" {
		t.Fatalf("entries = %+v, want one entry with an empty summary", entries)
	}
}
