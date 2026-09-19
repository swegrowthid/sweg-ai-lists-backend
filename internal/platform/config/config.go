package config

import (
	"os"
	"strings"
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
	return Config{Addr: addr, Env: env, Version: version, Service: service, LogLevel: level, DBURL: dbURL}
}

// IsProd reports production mode for logger and handler behavior.
func (c Config) IsProd() bool { return c.Env == "prod" }
