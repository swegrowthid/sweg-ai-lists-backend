package news

import (
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/net/html"
)

const (
	// listingDateLayout is the day format the blog cards carry in datetime.
	listingDateLayout = "2006-01-02"

	// Stored value caps. They mirror the news table CHECK constraints, so the
	// parser never hands the store a value Postgres would reject.
	maxTitleLen   = 300
	maxSummaryLen = 1000
	maxURLLen     = 500
)

// parseListing reads the blog listing page and returns one entry per card
// whose title contains the filter, case-insensitive, in page order. A card
// that misses its title, link, or date is skipped: one malformed card never
// blocks the rest of the list. An error means the page itself is unusable.
func parseListing(r io.Reader, base *url.URL, filter string) ([]Entry, error) {
	doc, err := html.Parse(r)
	if err != nil {
		return nil, fmt.Errorf("news: parse listing: %w", err)
	}
	articles := findAll(doc, elementNamed("article"))
	if len(articles) == 0 {
		return nil, errors.New("news: listing page has no articles")
	}
	needle := strings.ToLower(filter)
	entries := []Entry{}
	for _, article := range articles {
		entry, ok := parseArticle(article, base)
		if !ok || !strings.Contains(strings.ToLower(entry.Title), needle) {
			continue
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// parseArticle extracts one card. ok is false when the card misses a required
// field or carries a value a stored column cannot hold.
func parseArticle(article *html.Node, base *url.URL) (Entry, bool) {
	anchor := findFirst(findFirst(article, elementNamed("h2")), elementNamed("a"))
	if anchor == nil {
		return Entry{}, false
	}
	title := strings.TrimSpace(textContent(anchor))
	if title == "" || utf8.RuneCountInString(title) > maxTitleLen || strings.ContainsRune(title, 0) {
		return Entry{}, false
	}
	link, ok := resolveLink(attr(anchor, "href"), base)
	if !ok {
		return Entry{}, false
	}
	timeNode := findFirst(article, elementNamed("time"))
	if timeNode == nil {
		return Entry{}, false
	}
	// The HTML parser lowercases attribute names, so dateTime reads back as
	// datetime.
	publishedAt, err := time.Parse(listingDateLayout, strings.TrimSpace(attr(timeNode, "datetime")))
	if err != nil {
		return Entry{}, false
	}
	summary := ""
	if paragraph := findFirst(article, elementNamed("p")); paragraph != nil {
		text := strings.TrimSpace(textContent(paragraph))
		if utf8.RuneCountInString(text) > maxSummaryLen || strings.ContainsRune(text, 0) {
			return Entry{}, false
		}
		summary = text
	}
	return Entry{
		Title:       title,
		URL:         link,
		Summary:     summary,
		PublishedAt: publishedAt.UTC(),
	}, true
}

// resolveLink turns one card href into an absolute http(s) URL under the
// listing base, or reports false when the value is not a usable link.
func resolveLink(href string, base *url.URL) (string, bool) {
	if strings.TrimSpace(href) == "" || strings.ContainsRune(href, 0) {
		return "", false
	}
	ref, err := url.Parse(href)
	if err != nil {
		return "", false
	}
	absolute := base.ResolveReference(ref)
	if (absolute.Scheme != "http" && absolute.Scheme != "https") || absolute.Host == "" {
		return "", false
	}
	link := absolute.String()
	if utf8.RuneCountInString(link) > maxURLLen {
		return "", false
	}
	return link, true
}

// elementNamed matches one element node by tag name.
func elementNamed(name string) func(*html.Node) bool {
	return func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == name
	}
}

// findFirst returns the first node in document order that satisfies match,
// or nil when none does. A nil root yields nil.
func findFirst(root *html.Node, match func(*html.Node) bool) *html.Node {
	if root == nil {
		return nil
	}
	if match(root) {
		return root
	}
	for child := root.FirstChild; child != nil; child = child.NextSibling {
		if found := findFirst(child, match); found != nil {
			return found
		}
	}
	return nil
}

// findAll collects every node that satisfies match, in document order.
func findAll(root *html.Node, match func(*html.Node) bool) []*html.Node {
	var found []*html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if match(n) {
			found = append(found, n)
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return found
}

// textContent joins the text of n and its descendants, the way a browser
// renders the element.
func textContent(n *html.Node) string {
	var builder strings.Builder
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.TextNode {
			builder.WriteString(node.Data)
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(n)
	return builder.String()
}

// attr returns the value of one attribute, or "" when the key is absent.
func attr(n *html.Node, key string) string {
	for _, attribute := range n.Attr {
		if attribute.Key == key {
			return attribute.Val
		}
	}
	return ""
}
