package news

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"
)

func TestNextTick(t *testing.T) {
	epochGrid := time.Unix(0, 0).UTC().Add(3 * newsSyncInterval)
	cases := []struct {
		name string
		now  time.Time
		want time.Time
	}{
		{
			"just after a grid point",
			epochGrid.Add(time.Minute),
			epochGrid.Add(newsSyncInterval),
		},
		{
			"exactly on a grid point",
			epochGrid,
			epochGrid.Add(newsSyncInterval),
		},
		{
			"one nanosecond before a grid point",
			epochGrid.Add(-time.Nanosecond),
			epochGrid,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := nextTick(tc.now)
			if !got.Equal(tc.want) {
				t.Fatalf("nextTick(%v) = %v, want %v", tc.now, got, tc.want)
			}
			if got := got.Sub(tc.now); got <= 0 || got > newsSyncInterval {
				t.Fatalf("nextTick(%v) is %v away, want (0, %v]", tc.now, got, newsSyncInterval)
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

	// Run must fill the store before its first interval tick.
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
