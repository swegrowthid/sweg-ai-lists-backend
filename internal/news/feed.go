package news

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/net/html"
)

// maxFeedBytes caps one feed download.
const maxFeedBytes = 2 << 20

// rssDoc and atomDoc are the root element names that pick a parser.
const (
	rssDoc  = "rss"
	atomDoc = "feed"
)

// parseFeed parses one RSS 2.0 or Atom document and returns one UpsertInput
// per usable item, in feed order. Items that miss title, link, or date, or
// that carry a value a stored column cannot hold, are skipped: one bad item
// never blocks the rest.
func parseFeed(r io.Reader, base *url.URL) ([]UpsertInput, error) {
	body, err := io.ReadAll(io.LimitReader(r, maxFeedBytes+1))
	if err != nil {
		return nil, fmt.Errorf("news: read feed: %w", err)
	}
	if len(body) > maxFeedBytes {
		return nil, errors.New("news: feed exceeds the size limit")
	}

	var probe struct {
		XMLName xml.Name
	}
	if err := xml.Unmarshal(body, &probe); err != nil {
		return nil, fmt.Errorf("news: parse feed: %w", err)
	}
	switch probe.XMLName.Local {
	case rssDoc:
		return parseRSSFeed(bytes.NewReader(body), base)
	case atomDoc:
		return parseAtomFeed(bytes.NewReader(body), base)
	default:
		return nil, fmt.Errorf("news: unknown feed format %q", probe.XMLName.Local)
	}
}

// rssItem mirrors one RSS <item>.
type rssItem struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	PubDate     string `xml:"pubDate"`
	Description string `xml:"description"`
}

// rssDoc mirrors the RSS 2.0 envelope.
type rssEnvelope struct {
	Channel struct {
		Items []rssItem `xml:"item"`
	} `xml:"channel"`
}

// parseRSSFeed extracts one input per <item>. pubDate follows RFC 1123, with
// a numeric-zone variant and named-zone fallbacks, as real feeds vary.
func parseRSSFeed(r io.Reader, base *url.URL) ([]UpsertInput, error) {
	var envelope rssEnvelope
	if err := xml.NewDecoder(r).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("news: parse rss: %w", err)
	}
	inputs := []UpsertInput{}
	for _, item := range envelope.Channel.Items {
		publishedAt, ok := parseRSSDate(item.PubDate)
		if !ok {
			continue
		}
		input, ok := feedUpsertInput(item.Title, item.Link, stripHTML(item.Description), publishedAt, base)
		if !ok {
			continue
		}
		inputs = append(inputs, input)
	}
	return inputs, nil
}

// parseRSSDate accepts RFC 1123Z, RFC 1123, and RFC 1123 with a named zone.
func parseRSSDate(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, false
	}
	layouts := []string{time.RFC1123Z, time.RFC1123, "Mon, 2 Jan 2006 15:04:05 -0700", "Mon, 2 Jan 2006 15:04:05 MST"}
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UTC(), true
		}
	}
	return time.Time{}, false
}

// atomLink mirrors one Atom <link>.
type atomLink struct {
	Rel  string `xml:"rel,attr"`
	Href string `xml:"href,attr"`
}

// atomEntry mirrors one Atom <entry>. Published is the original post date;
// Updated moves on every edit, so it is only the fallback.
type atomEntry struct {
	Title     string     `xml:"title"`
	Published string     `xml:"published"`
	Updated   string     `xml:"updated"`
	Summary   string     `xml:"summary"`
	Content   string     `xml:"content"`
	Links     []atomLink `xml:"link"`
}

// atomEnvelope mirrors the Atom envelope.
type atomEnvelope struct {
	Entries []atomEntry `xml:"entry"`
}

// parseAtomFeed extracts one input per <entry>. The link is the
// rel="alternate" href, else the first href. updated (RFC 3339) is required.
func parseAtomFeed(r io.Reader, base *url.URL) ([]UpsertInput, error) {
	var envelope atomEnvelope
	if err := xml.NewDecoder(r).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("news: parse atom: %w", err)
	}
	inputs := []UpsertInput{}
	for _, entry := range envelope.Entries {
		stamp := firstNonEmpty(entry.Published, entry.Updated)
		publishedAt, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(stamp))
		if err != nil {
			continue
		}
		href := atomAlternateHref(entry.Links)
		input, ok := feedUpsertInput(entry.Title, href, stripHTML(firstNonEmpty(entry.Summary, entry.Content)), publishedAt, base)
		if !ok {
			continue
		}
		inputs = append(inputs, input)
	}
	return inputs, nil
}

// atomAlternateHref picks the rel="alternate" link, else the first link.
func atomAlternateHref(links []atomLink) string {
	for _, link := range links {
		if link.Rel == "alternate" {
			return link.Href
		}
	}
	if len(links) > 0 {
		return links[0].Href
	}
	return ""
}

// feedUpsertInput validates one feed item into an UpsertInput. ok is false
// when a required field is missing or a value would not fit a stored column.
// Real feeds double-escape entities ("&amp;#8217;"), so text gets one
// UnescapeString pass before the caps apply.
func feedUpsertInput(title, rawLink, summary string, publishedAt time.Time, base *url.URL) (UpsertInput, bool) {
	title = strings.TrimSpace(html.UnescapeString(title))
	if title == "" || utf8.RuneCountInString(title) > maxTitleLen || strings.ContainsRune(title, 0) {
		return UpsertInput{}, false
	}
	link, ok := resolveLink(rawLink, base)
	if !ok {
		return UpsertInput{}, false
	}
	summary = strings.TrimSpace(html.UnescapeString(summary))
	if strings.ContainsRune(summary, 0) {
		return UpsertInput{}, false
	}
	summary = truncateSummary(summary, maxSummaryLen)
	return UpsertInput{
		Title:       title,
		URL:         link,
		Summary:     summary,
		PublishedAt: publishedAt.UTC(),
	}, true
}

// htmlTagPattern matches one HTML or XML tag, for summary stripping.
var htmlTagPattern = regexp.MustCompile(`<[^>]*>`)

// whitespacePattern collapses runs of whitespace to one space.
var whitespacePattern = regexp.MustCompile(`\s+`)

// stripHTML removes every tag from s and collapses whitespace.
func stripHTML(s string) string {
	return strings.TrimSpace(whitespacePattern.ReplaceAllString(htmlTagPattern.ReplaceAllString(s, " "), " "))
}

// firstNonEmpty returns the first non-empty string, or "".
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
