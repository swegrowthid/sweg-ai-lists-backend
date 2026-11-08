package tools

import (
	"context"
	"errors"
	"sync"
	"testing"
)

// seededVoteService syncs the fixture catalog and returns a service whose
// vote store is the in-memory default from newServiceWithSource.
func seededVoteService(t *testing.T) *Service {
	t.Helper()
	svc, _ := newTestService(t, fixtureServer(t))
	if _, err := svc.Sync(context.Background()); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	return svc
}

// Vote on an unknown id answers ErrNotFound, never touching the vote store.
func TestServiceVoteUnknownTool(t *testing.T) {
	svc := seededVoteService(t)

	if _, _, err := svc.Vote(context.Background(), "user-1", "XX-999"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Vote() error = %v, want ErrNotFound", err)
	}
	if _, err := svc.Unvote(context.Background(), "user-1", "XX-999"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Unvote() error = %v, want ErrNotFound", err)
	}
}

// Vote without a user id answers ErrInvalidInput: the caller id always comes
// from the auth principal, never from the request body.
func TestServiceVoteEmptyUser(t *testing.T) {
	svc := seededVoteService(t)

	if _, _, err := svc.Vote(context.Background(), "", "P-001"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("Vote() error = %v, want ErrInvalidInput", err)
	}
	if _, err := svc.Unvote(context.Background(), "", "P-001"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("Unvote() error = %v, want ErrInvalidInput", err)
	}
}

// ListFor fills votes for every row and voted only for the viewer; an
// anonymous reader gets counts with voted false everywhere.
func TestServiceListForFillsVotesAndVoted(t *testing.T) {
	svc := seededVoteService(t)
	ctx := context.Background()

	if _, _, err := svc.Vote(ctx, "user-1", "P-001"); err != nil {
		t.Fatalf("Vote() error = %v", err)
	}
	if _, _, err := svc.Vote(ctx, "user-2", "P-001"); err != nil {
		t.Fatalf("Vote() error = %v", err)
	}

	got, err := svc.ListFor(ctx, ListFilter{}, "user-1")
	if err != nil {
		t.Fatalf("ListFor() error = %v", err)
	}
	byID := map[string]Tool{}
	for _, row := range got.Data {
		byID[row.ID] = row
	}
	if byID["P-001"].Votes != 2 || !byID["P-001"].Voted {
		t.Fatalf("viewer P-001 = %+v, want {Votes:2 Voted:true}", byID["P-001"])
	}
	if byID["P-005"].Votes != 0 || byID["P-005"].Voted {
		t.Fatalf("viewer P-005 = %+v, want {Votes:0 Voted:false}", byID["P-005"])
	}

	anon, err := svc.ListFor(ctx, ListFilter{}, "")
	if err != nil {
		t.Fatalf("ListFor(anon) error = %v", err)
	}
	for _, row := range anon.Data {
		if row.Voted {
			t.Fatalf("anon %s voted = true, want false", row.ID)
		}
		if row.ID == "P-001" && row.Votes != 2 {
			t.Fatalf("anon P-001 votes = %d, want 2", row.Votes)
		}
	}

	other, err := svc.GetFor(ctx, "P-001", "user-2")
	if err != nil {
		t.Fatalf("GetFor() error = %v", err)
	}
	if other.Votes != 2 || !other.Voted {
		t.Fatalf("GetFor(user-2) = %+v, want {Votes:2 Voted:true}", other)
	}
	anonOne, err := svc.GetFor(ctx, "P-001", "")
	if err != nil {
		t.Fatalf("GetFor(anon) error = %v", err)
	}
	if anonOne.Votes != 2 || anonOne.Voted {
		t.Fatalf("GetFor(anon) = %+v, want {Votes:2 Voted:false}", anonOne)
	}
}

// sort=votes defaults to desc and tie-breaks by id ascending; asc reverses
// the counts but keeps the same tie-break. Paging still cuts the ordered rows
// and meta.total still counts catalog rows, not votes.
func TestServiceListForSortVotes(t *testing.T) {
	svc := seededVoteService(t)
	ctx := context.Background()

	vote := func(user, tool string) {
		t.Helper()
		if _, _, err := svc.Vote(ctx, user, tool); err != nil {
			t.Fatalf("Vote(%s, %s) error = %v", user, tool, err)
		}
	}
	vote("user-1", "P-001")
	vote("user-2", "P-001")
	vote("user-3", "ADE-001")
	vote("user-4", "ADE-001")
	vote("user-1", "CA-001")

	desc, err := svc.ListFor(ctx, ListFilter{Sort: SortVotes}, "user-1")
	if err != nil {
		t.Fatalf("ListFor(sort=votes) error = %v", err)
	}
	// ADE-001 and P-001 both hold 2 votes; ADE-001 wins the tie-break.
	if ids := idsCSV(desc.Data); ids[:len("ADE-001,P-001,CA-001")] != "ADE-001,P-001,CA-001" {
		t.Fatalf("desc ids = %s, want ADE-001,P-001,CA-001 first", ids)
	}
	if desc.Meta.Total != 10 {
		t.Fatalf("desc total = %d, want 10", desc.Meta.Total)
	}

	asc, err := svc.ListFor(ctx, ListFilter{Sort: SortVotes, Order: OrderAsc}, "")
	if err != nil {
		t.Fatalf("ListFor(sort=votes,asc) error = %v", err)
	}
	// Zero-vote rows come first in asc, still tie-broken by id ascending.
	if ids := idsCSV(asc.Data); ids[:len("ADE-012")] != "ADE-012" {
		t.Fatalf("asc first id = %s, want ADE-012 (0 votes, lowest id)", ids)
	}
	last := asc.Data[len(asc.Data)-1]
	if last.ID != "P-001" {
		t.Fatalf("asc last id = %s, want P-001 (2 votes, highest id)", last.ID)
	}
	if asc.Meta.Total != 10 || asc.Meta.TotalPages != 1 {
		t.Fatalf("asc meta = %+v, want total 10 across 1 page", asc.Meta)
	}

	paged, err := svc.ListFor(ctx, ListFilter{Sort: SortVotes, Page: 2, PerPage: 4}, "user-1")
	if err != nil {
		t.Fatalf("ListFor(paged) error = %v", err)
	}
	if paged.Meta != (PageMeta{Page: 2, PerPage: 4, Total: 10, TotalPages: 3}) {
		t.Fatalf("paged meta = %+v, want 2/4/10/3", paged.Meta)
	}
	if len(paged.Data) != 4 {
		t.Fatalf("paged rows = %d, want 4", len(paged.Data))
	}
}

// Two concurrent votes for one pair keep one row: exactly one reports
// created true. Run with -race to catch store races through the service.
func TestServiceVoteConcurrentSamePair(t *testing.T) {
	svc := seededVoteService(t)
	ctx := context.Background()

	const writers = 2
	created := make([]bool, writers)
	var wg sync.WaitGroup
	for i := range writers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, c, err := svc.Vote(ctx, "user-1", "P-001")
			if err != nil {
				t.Errorf("Vote() error = %v", err)
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
	got, err := svc.GetFor(ctx, "P-001", "user-1")
	if err != nil {
		t.Fatalf("GetFor() error = %v", err)
	}
	if got.Votes != 1 || !got.Voted {
		t.Fatalf("GetFor() = %+v, want {Votes:1 Voted:true}", got)
	}
}
