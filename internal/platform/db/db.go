package db

import "context"

// Pinger is anything readiness can ping. Stores adapt to it.
type Pinger interface {
	Ping(ctx context.Context) error
}

// NoopPinger always reports healthy. Use until real DB lands.
type NoopPinger struct{}

// Ping implements Pinger.
func (NoopPinger) Ping(context.Context) error { return nil }
