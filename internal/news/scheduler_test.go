package news

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"
)

func TestNextMidnight(t *testing.T) {
	zone := time.FixedZone("test", 7*60*60)
	cases := []struct {
		name string
		now  time.Time
		want time.Time
	}{
		{
			"midday",
			time.Date(2026, 9, 30, 10, 0, 0, 0, zone),
			time.Date(2026, 10, 1, 0, 0, 0, 0, zone),
		},
		{
			"one minute before midnight",
			time.Date(2026, 9, 30, 23, 59, 0, 0, zone),
			time.Date(2026, 10, 1, 0, 0, 0, 0, zone),
		},
		{
			"exactly midnight",
			time.Date(2026, 9, 30, 0, 0, 0, 0, zone),
			time.Date(2026, 10, 1, 0, 0, 0, 0, zone),
		},
		{
			"end of year",
			time.Date(2026, 12, 31, 12, 0, 0, 0, zone),
			time.Date(2027, 1, 1, 0, 0, 0, 0, zone),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := nextMidnight(tc.now); !got.Equal(tc.want) {
				t.Fatalf("nextMidnight(%v) = %v, want %v", tc.now, got, tc.want)
			}
		})
	}
}

func TestSchedulerRunSyncsAtStartupAndStopsWithContext(t *testing.T) {
	svc, store := newTestService(t, fixtureServer(t))
	scheduler := NewScheduler(svc, slog.New(slog.NewTextHandler(io.Discard, nil)))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		scheduler.Run(ctx)
		close(done)
	}()

	// Run must fill the store before its first midnight.
	deadline := time.Now().Add(5 * time.Second)
	for len(listEntries(t, store)) != 4 {
		if time.Now().After(deadline) {
			t.Fatal("Run() did not store the startup sync result")
		}
		time.Sleep(10 * time.Millisecond)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run() did not stop after the context ended")
	}
}
