package dailyterm

import (
	"strings"
	"time"
)

// maxSlugLength caps the slug half of a term id. One upstream title runs to 84
// characters; the cap keeps an id bounded without disturbing any real title.
const maxSlugLength = 80

// slugify turns a term title into the id fragment a client can put in a URL:
// lowercase ASCII letters, digits, and single hyphens. "Sieve Algorithm or
// Sieve of Eratosthenes" becomes "sieve-algorithm-or-sieve-of-eratosthenes" and
// "CQRS (Command Query Responsibility Segregation)" becomes
// "cqrs-command-query-responsibility-segregation".
//
// Titles are decorative upstream, so the slug is deliberately lossy: an
// accented or non-Latin character is dropped rather than transliterated. The
// only consequence is a shorter id.
func slugify(title string) string {
	var builder strings.Builder
	builder.Grow(len(title))
	for _, r := range strings.ToLower(title) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			builder.WriteRune(r)
		default:
			builder.WriteByte('-')
		}
	}
	// Collapse runs of separators and drop the ends, so "SLIs/SLOs/SLAs" lands
	// as "slis-slos-slas" rather than carrying a run of hyphens.
	parts := make([]string, 0, 8)
	for _, part := range strings.Split(builder.String(), "-") {
		if part != "" {
			parts = append(parts, part)
		}
	}
	slug := strings.Join(parts, "-")
	if len(slug) > maxSlugLength {
		slug = strings.Trim(slug[:maxSlugLength], "-")
	}
	return slug
}

// termID builds the stable id a client deep-links with: the month key plus the
// title slug, e.g. "2026-01-sieve-algorithm-or-sieve-of-eratosthenes". The day
// marker is left out on purpose: it is an ordinal that upstream repeats inside
// a month, so it cannot disambiguate, and inserting a day later would change
// ids that clients already hold.
func termID(year int, month time.Month, title string) string {
	return monthKey(year, month) + "-" + slugify(title)
}
