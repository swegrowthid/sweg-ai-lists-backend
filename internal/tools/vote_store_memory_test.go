package tools

import (
	"context"
	"sync"
	"testing"
)

// First add records the row and reports created true; a repeat add records
// nothing and reports created false.
func TestMemoryVoteStoreAddIsIdempotent(t *testing.T) {
	store := NewMemoryVoteStore()
	ctx := context.Background()

	created, err := store.Add(ctx, "user-1", "P-001")
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if !created {
		t.Fatal("first Add() created = false, want true")
	}

	created, err = store.Add(ctx, "user-1", "P-001")
	if err != nil {
		t.Fatalf("second Add() error = %v", err)
	}
	if created {
		t.Fatal("second Add() created = true, want false")
	}

	sums, err := store.Summaries(ctx, "user-1", []string{"P-001"})
	if err != nil {
		t.Fatalf("Summaries() error = %v", err)
	}
	if sums["P-001"] != (VoteSummary{Votes: 1, Voted: true}) {
		t.Fatalf("Summaries() = %+v, want {Votes:1 Voted:true}", sums["P-001"])
	}
}

// Two users on one tool count 2, and each viewer sees its own voted flag.
func TestMemoryVoteStoreCountsTwoUsers(t *testing.T) {
	store := NewMemoryVoteStore()
	ctx := context.Background()

	for _, user := range []string{"user-1", "user-2"} {
		if _, err := store.Add(ctx, user, "P-001"); err != nil {
			t.Fatalf("Add(%s) error = %v", user, err)
		}
	}

	sums, err := store.Summaries(ctx, "user-1", []string{"P-001", "P-005"})
	if err != nil {
		t.Fatalf("Summaries() error = %v", err)
	}
	if sums["P-001"] != (VoteSummary{Votes: 2, Voted: true}) {
		t.Fatalf("viewer user-1 P-001 = %+v, want {Votes:2 Voted:true}", sums["P-001"])
	}
	if _, ok := sums["P-005"]; ok {
		t.Fatalf("P-005 without votes is present: %+v", sums)
	}

	anon, err := store.Summaries(ctx, "", []string{"P-001"})
	if err != nil {
		t.Fatalf("Summaries(anon) error = %v", err)
	}
	if anon["P-001"] != (VoteSummary{Votes: 2, Voted: false}) {
		t.Fatalf("anon P-001 = %+v, want {Votes:2 Voted:false}", anon["P-001"])
	}

	other, err := store.Summaries(ctx, "user-3", []string{"P-001"})
	if err != nil {
		t.Fatalf("Summaries(user-3) error = %v", err)
	}
	if other["P-001"] != (VoteSummary{Votes: 2, Voted: false}) {
		t.Fatalf("viewer user-3 P-001 = %+v, want {Votes:2 Voted:false}", other["P-001"])
	}
}

// Remove drops the row and reports removed true; a repeat remove is a no-op
// that still succeeds and reports removed false.
func TestMemoryVoteStoreRemoveIsIdempotent(t *testing.T) {
	store := NewMemoryVoteStore()
	ctx := context.Background()

	if _, err := store.Add(ctx, "user-1", "P-001"); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	removed, err := store.Remove(ctx, "user-1", "P-001")
	if err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if !removed {
		t.Fatal("first Remove() removed = false, want true")
	}

	removed, err = store.Remove(ctx, "user-1", "P-001")
	if err != nil {
		t.Fatalf("second Remove() error = %v", err)
	}
	if removed {
		t.Fatal("second Remove() removed = true, want false")
	}

	sums, err := store.Summaries(ctx, "user-1", []string{"P-001"})
	if err != nil {
		t.Fatalf("Summaries() error = %v", err)
	}
	if len(sums) != 0 {
		t.Fatalf("Summaries() = %+v, want no rows after remove", sums)
	}
}

// Concurrent adds for one pair must keep exactly one row: only one writer
// reports created true. Run with -race to catch map races.
func TestMemoryVoteStoreConcurrentAddsKeepOneRow(t *testing.T) {
	store := NewMemoryVoteStore()
	ctx := context.Background()

	const writers = 8
	created := make([]bool, writers)
	var wg sync.WaitGroup
	for i := range writers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			c, err := store.Add(ctx, "user-1", "P-001")
			if err != nil {
				t.Errorf("Add() error = %v", err)
				return
			}
			created[i] = c
		}(i)
	}
	wg.Wait()

	ones := 0
	for _, c := range created {
		if c {
			ones++
		}
	}
	if ones != 1 {
		t.Fatalf("created count = %d, want exactly 1", ones)
	}
	sums, err := store.Summaries(ctx, "user-1", []string{"P-001"})
	if err != nil {
		t.Fatalf("Summaries() error = %v", err)
	}
	if sums["P-001"].Votes != 1 {
		t.Fatalf("votes = %d, want 1", sums["P-001"].Votes)
	}
}
