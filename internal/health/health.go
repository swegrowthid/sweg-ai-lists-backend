package health

// Response is the JSON shape for liveness and readiness.
// Keep one shape for both endpoints. Clients parse one struct.
type Response struct {
	Status  string            `json:"status"`
	Version string            `json:"version"`
	UptimeS int64             `json:"uptime_s"`
	Checks  map[string]string `json:"checks,omitempty"`
}

// Statuses. Use constants, never raw strings in handlers.
const (
	StatusOK   = "ok"
	StatusFail = "fail"
)
