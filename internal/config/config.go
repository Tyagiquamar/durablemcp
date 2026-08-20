package config

import (
	"os"
	"strconv"
	"time"
)

// Config holds runtime configuration for every DurableMCP process. Values are
// resolved from environment variables with sane local-development defaults.
type Config struct {
	Addr         string
	DatabaseURL  string
	ReaderAPIKey string
	MCPTransport string
	LogLevel     string
	WorkerID     string
	PollInterval time.Duration
	LeaseSeconds int
	TickSeconds  int
}

// Load reads configuration from the environment.
func Load() Config {
	return Config{
		Addr:         listenAddr(),
		DatabaseURL:  env("DATABASE_URL", "postgres://durablemcp:durablemcp@localhost:5432/durablemcp?sslmode=disable"),
		ReaderAPIKey: os.Getenv("READER_API_KEY"),
		MCPTransport: env("MCP_TRANSPORT", "http"),
		LogLevel:     env("LOG_LEVEL", "info"),
		WorkerID:     env("WORKER_ID", "worker-1"),
		PollInterval: time.Duration(envInt("POLL_INTERVAL_MS", 500)) * time.Millisecond,
		LeaseSeconds: envInt("LEASE_SECONDS", 30),
		TickSeconds:  envInt("TICK_SECONDS", 5),
	}
}

func listenAddr() string {
	if addr := os.Getenv("DURABLEMCP_ADDR"); addr != "" {
		return addr
	}
	if port := os.Getenv("PORT"); port != "" {
		return ":" + port
	}
	return ":8080"
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}
