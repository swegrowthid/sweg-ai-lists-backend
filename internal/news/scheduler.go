package news

import (
	"context"
	"log/slog"
	"time"
)

// newsSyncInterval is how often the news list refreshes after the startup
// sync.
const newsSyncInterval = 6 * time.Hour

// Scheduler keeps the news list fresh: one sync now, then every
// newsSyncInterval. It blocks, so main runs it in its own goroutine.
type Scheduler struct {
	svc *Service
	log *slog.Logger
}

// NewScheduler wires the service. Nil log falls back to slog.Default.
func NewScheduler(svc *Service, log *slog.Logger) *Scheduler {
	if svc == nil {
		panic("news: nil Service")
	}
	if log == nil {
		log = slog.Default()
	}
	return &Scheduler{svc: svc, log: log}
}

// Run syncs once at startup and then on every newsSyncInterval tick until
// ctx ends. A failed or partial sync is logged, never fatal: the next run
// retries.
func (s *Scheduler) Run(ctx context.Context) {
	s.syncOnce(ctx)
	for {
		timer := time.NewTimer(time.Until(nextTick(time.Now())))
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
		s.log.Warn("news sync incomplete", "error", err, "stored", stored)
		return
	}
	s.log.Info("news sync done", "stored", stored)
}

// nextTick returns the first point on the fixed newsSyncInterval grid
// strictly after now, measured from the Unix epoch so the interval is stable
// across process restarts.
func nextTick(now time.Time) time.Time {
	next := now.Add(newsSyncInterval)
	remainder := next.UnixNano() % int64(newsSyncInterval)
	if remainder == 0 {
		return next
	}
	return next.Add(-time.Duration(remainder))
}
