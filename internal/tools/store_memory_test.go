package tools

import (
	"context"
	"testing"
)

// TestMemoryStoreQueryMatchesEverySearchableField pins the haystack at the
// store level with crafted rows, so each needle names exactly one field. The
// service tests only see the fixture catalog, where a name-only regression
// could hide behind label or feature text.
func TestMemoryStoreQueryMatchesEverySearchableField(t *testing.T) {
	store := NewMemoryStore()
	err := store.ReplaceAll(context.Background(), []Tool{
		{ID: "P-001", Name: "Alpha", WebsiteLabel: "alpha.example", Status: "Active", MinSpend: "$5"},
		{ID: "P-002", Name: "Beta", WebsiteLabel: "beta.example", Status: "Waitlist", MinSpend: "pay-as-you-go"},
		{ID: "ADE-001", Name: "Gamma", WebsiteLabel: "gamma.example", Status: "Active", AIFeatures: []string{"Autocomplete", "Agent Mode"}},
	})
	if err != nil {
		t.Fatalf("ReplaceAll() error = %v", err)
	}

	for _, tc := range []struct {
		query string
		want  string
	}{
		{"alpha", "P-001"},          // name
		{"beta.example", "P-002"},   // website label
		{"waitlist", "P-002"},       // status
		{"pay-as-you-go", "P-002"},  // minimum spend
		{"autocomplete", "ADE-001"}, // ADE feature list
		{"agent mode", "ADE-001"},   // multi-word feature
		{"  ALPHA  ", "P-001"},      // trim + case
	} {
		got, err := store.List(context.Background(), ListFilter{Query: tc.query})
		if err != nil {
			t.Fatalf("List(%q) error = %v", tc.query, err)
		}
		if ids := idsCSV(got); len(got) != 1 || ids != tc.want {
			t.Fatalf("List(%q) ids = %s, want %s", tc.query, ids, tc.want)
		}
	}
}

// TestMemoryStoreQueryWithoutMatchIsEmpty pins the zero result: no rows, no
// error, and the slice the service pages next stays empty rather than nil.
func TestMemoryStoreQueryWithoutMatchIsEmpty(t *testing.T) {
	store := NewMemoryStore()
	if err := store.ReplaceAll(context.Background(), []Tool{{ID: "P-001", Name: "Alpha"}}); err != nil {
		t.Fatalf("ReplaceAll() error = %v", err)
	}
	got, err := store.List(context.Background(), ListFilter{Query: "zzz-not-a-tool"})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("List() ids = %s, want no rows", idsCSV(got))
	}
}
