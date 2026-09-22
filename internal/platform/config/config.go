package config

import (
	"os"
	"strings"
	"time"
)

// Config holds process-level settings.
// Load reads env once at the edge. Inside the graph, trust Config.
type Config struct {
	Addr     string
	Env      string
	Version  string
	Service  string
	LogLevel string
	DBURL    string
	DocsUI   bool

	JWTSecret     string
	JWTAccessTTL  time.Duration
	JWTRefreshTTL time.Duration
}

// Load builds Config from environment with safe defaults.
// It sources .env from the working dir first; real env always wins.
func Load() Config {
	loadDotEnv(".env")
	addr := strings.TrimSpace(os.Getenv("APP_ADDR"))
	if addr == "" {
		addr = ":8080"
	}
	env := strings.ToLower(strings.TrimSpace(os.Getenv("APP_ENV")))
	if env == "" {
		env = "dev"
	}
	version := strings.TrimSpace(os.Getenv("APP_VERSION"))
	if version == "" {
		version = "dev"
	}
	service := strings.TrimSpace(os.Getenv("APP_SERVICE"))
	if service == "" {
		service = "sweg-ai-lists-backend"
	}
	level := strings.ToLower(strings.TrimSpace(os.Getenv("APP_LOG_LEVEL")))
	dbURL := strings.TrimSpace(os.Getenv("DB_URL"))
	return Config{
		Addr:          addr,
		Env:           env,
		Version:       version,
		Service:       service,
		LogLevel:      level,
		DBURL:         dbURL,
		DocsUI:        boolEnv("DOCS_UI", env != "prod"),
		JWTSecret:     strings.TrimSpace(os.Getenv("JWT_SECRET")),
		JWTAccessTTL:  durationEnv("JWT_ACCESS_TTL", 15*time.Minute),
		JWTRefreshTTL: durationEnv("JWT_REFRESH_TTL", 7*24*time.Hour),
	}
}

// durationEnv reads a Go duration from env, falling back on empty or invalid.
func durationEnv(key string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return fallback
	}
	return d
}

// boolEnv reads a boolean from env, falling back on empty or invalid.
func boolEnv(key string, fallback bool) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "1", "true", "on", "yes":
		return true
	case "0", "false", "off", "no":
		return false
	}
	return fallback
}

// IsProd reports production mode for logger and handler behavior.
func (c Config) IsProd() bool { return c.Env == "prod" }
