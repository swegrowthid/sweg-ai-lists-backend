package health

import (
	"context"
	"fmt"
	"time"

	"github.com/swegrowthid/sweg-ai-lists-backend/internal/platform/db"
)

// Service owns health logic. No HTTP here, only data.
// R: version string, db Pinger. Swap db in tests with a fake.
type Service struct {
	version string
	started time.Time
	db      db.Pinger
	now     func() time.Time
}

// NewService wires dependencies. Nil db falls back to NoopPinger.
func NewService(version string, checker db.Pinger) *Service {
	if checker == nil {
		checker = db.NoopPinger{}
	}
	if version == "" {
		version = "dev"
	}
	return &Service{version: version, started: time.Now(), db: checker, now: time.Now}
}

// Live never fails. Liveness means the process answers.
func (s *Service) Live() Response {
	return Response{Status: StatusOK, Version: s.version, UptimeS: int64(s.now().Sub(s.started).Seconds())}
}

// Ready pings dependencies. DbDown stays in E channel as error value.
func (s *Service) Ready(ctx context.Context) (Response, error) {
	pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := s.db.Ping(pingCtx); err != nil {
		resp := Response{
			Status:  StatusFail,
			Version: s.version,
			UptimeS: int64(s.now().Sub(s.started).Seconds()),
			Checks:  map[string]string{"db": "fail"},
		}
		return resp, fmt.Errorf("health: db ping: %w", err)
	}
	resp := s.Live()
	resp.Checks = map[string]string{"db": StatusOK}
	return resp, nil
}
