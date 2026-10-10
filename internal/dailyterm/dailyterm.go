// Package dailyterm serves the daily term journal from the
// swegrowthid/daily-term-SE-Growth repository. The markdown files there are the
// database: one file per month under a year directory. A scheduler fetches and
// parses them once at startup and then every day at midnight server time, and
// keeps the result as an in-memory snapshot. No SQL table mirrors the journal;
// the markdown stays the single source of truth.
//
// Definition and Example stay raw markdown: this backend carries no markdown
// renderer, and the frontend renders them.
package dailyterm

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// Term is one journal entry: one term written on one day of one month file.
// Day is the source's "day - N" ordinal, not a calendar date: the journal
// numbers the working days of a month and skips weekends, so a month can open
// at day 28 (2025-06) and the numbering restarts every month. Day is 0 when the
// file carries no marker above the term.
type Term struct {
	ID         string `json:"id"`
	Year       int    `json:"year"`
	Month      int    `json:"month"`
	MonthKey   string `json:"month_key"`
	Day        int    `json:"day"`
	Slug       string `json:"slug"`
	Title      string `json:"title"`
	Definition string `json:"definition"`
	Example    string `json:"example"`
	SourceFile string `json:"source_file"`
	SourceURL  string `json:"source_url"`
}

// MonthSummary describes one source month file and how many terms it yields.
type MonthSummary struct {
	MonthKey   string `json:"month_key"`
	Name       string `json:"name"`
	Year       int    `json:"year"`
	Month      int    `json:"month"`
	SourceFile string `json:"source_file"`
	SourceURL  string `json:"source_url"`
	Count      int    `json:"count"`
}

// ListMeta describes the month a list response carries.
type ListMeta struct {
	// MonthKey is the month actually served, YYYY-MM. It is the requested month,
	// or the newest available month when the client sent none.
	MonthKey   string `json:"month_key"`
	Name       string `json:"name"`
	Year       int    `json:"year"`
	Month      int    `json:"month"`
	SourceFile string `json:"source_file"`
	SourceURL  string `json:"source_url"`
	Count      int    `json:"count"`
}

// TermList is the GET /daily-terms body: one month of terms plus the metadata
// that says which month answered. The envelope is always present, even for an
// empty month, so the client never has to guess what it received.
type TermList struct {
	Data []Term   `json:"data"`
	Meta ListMeta `json:"meta"`
}

// Sentinel errors for the E channel. Handlers map them to status codes.
var (
	// ErrNotFound: no term or month matches the request.
	ErrNotFound = errors.New("dailyterm: not found")
	// ErrInvalidInput: a request carries a byte the journal cannot hold.
	ErrInvalidInput = errors.New("dailyterm: invalid input")
)

// monthNames is the display name of each month, indexed by time.Month.
var monthNames = map[time.Month]string{
	time.January:   "January",
	time.February:  "February",
	time.March:     "March",
	time.April:     "April",
	time.May:       "May",
	time.June:      "June",
	time.July:      "July",
	time.August:    "August",
	time.September: "September",
	time.October:   "October",
	time.November:  "November",
	time.December:  "December",
}

// monthKey renders a year and month as the storage key and the wire value,
// YYYY-MM.
func monthKey(year int, month time.Month) string {
	return fmt.Sprintf("%04d-%02d", year, int(month))
}

// parseMonthKey reads a YYYY-MM wire value. It rejects a month outside 1..12 so
// a typo like 2026-13 answers with a 400 instead of an empty month.
func parseMonthKey(raw string) (int, time.Month, error) {
	year, month, err := parseMonthKeyParts(raw)
	if err != nil {
		return 0, 0, err
	}
	return year, month, nil
}

// parseMonthKeyParts does the work behind parseMonthKey. It is split out so the
// service can tell a malformed key from a well-formed but missing one.
func parseMonthKeyParts(raw string) (int, time.Month, error) {
	if len(raw) != 7 || raw[4] != '-' {
		return 0, 0, ErrInvalidInput
	}
	year, err := parseDigits(raw[:4])
	if err != nil {
		return 0, 0, ErrInvalidInput
	}
	month, err := parseDigits(raw[5:])
	if err != nil || month < 1 || month > 12 {
		return 0, 0, ErrInvalidInput
	}
	return year, time.Month(month), nil
}

// parseDigits reads a run of ASCII digits. It rejects signs, spaces, and an
// empty run, so "20e6-01" and "2026-0 " never reach the store.
func parseDigits(raw string) (int, error) {
	if raw == "" {
		return 0, ErrInvalidInput
	}
	value := 0
	for _, r := range raw {
		if r < '0' || r > '9' {
			return 0, ErrInvalidInput
		}
		value = value*10 + int(r-'0')
	}
	return value, nil
}

// containsNUL reports whether a client value carries a NUL byte. Postgres and
// JSON both refuse it, so it is rejected at the boundary.
func containsNUL(raw string) bool { return strings.ContainsRune(raw, 0) }
