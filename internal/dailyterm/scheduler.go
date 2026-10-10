package dailyterm

import (
	"context"
	"log/slog"
	"time"
)

// Scheduler keeps the daily term snapshot fresh: one sync now, then every day
// at midnight server time, the same slot news and tools use. It blocks, so main
// runs it in its own goroutine.
type Scheduler struct {
	svc *Service
	log *slog.Logger
}

// NewScheduler wires the service. Nil log falls back to slog.Default.
func NewScheduler(svc *Service, log *slog.Logger) *Scheduler {
	if svc == nil {
		panic("dailyterm: nil Service")
	}
	if log == nil {
		log = slog.Default()
	}
	return &Scheduler{svc: svc, log: log}
}

// Run syncs once at startup and then on every local midnight until ctx ends. A
// failed sync is logged, never fatal: the snapshot from the last good run keeps
// serving and the next run retries.
func (s *Scheduler) Run(ctx context.Context) {
	s.syncOnce(ctx)
	for {
		timer := time.NewTimer(time.Until(nextMidnight(time.Now())))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			s.syncOnce(ctx)
		}
	}
}

// syncOnce runs one refresh and logs the outcome.
func (s *Scheduler) syncOnce(ctx context.Context) {
	stored, err := s.svc.Sync(ctx)
	if err != nil {
		s.log.Warn("daily term sync failed", "error", err)
		return
	}
	s.log.Info("daily term sync done", "stored", stored)
}

// nextMidnight returns the first local midnight strictly after now.
func nextMidnight(now time.Time) time.Time {
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	return midnight.AddDate(0, 0, 1)
}
