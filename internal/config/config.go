// Package config loads application settings from environment variables.
package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

// Config holds the runtime configuration for the server.
type Config struct {
	AppEnv      string
	Port        string
	DatabaseURL string
}

// Load reads configuration from the environment, optionally seeding it from a
// local .env file outside of production.
func Load() (Config, error) {
	appEnv := getenv("APP_ENV", "development")
	if appEnv != "production" {
		// A missing .env is fine: environment variables are authoritative.
		_ = godotenv.Load()
	}

	cfg := Config{
		AppEnv:      appEnv,
		Port:        getenv("PORT", "8080"),
		DatabaseURL: getenv("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/tasks?sslmode=disable"),
	}

	port, err := strconv.Atoi(cfg.Port)
	if err != nil || port < 1 || port > 65535 {
		return Config{}, fmt.Errorf("invalid PORT %q: must be a number between 1 and 65535", cfg.Port)
	}

	parsed, err := url.Parse(cfg.DatabaseURL)
	if err != nil {
		return Config{}, fmt.Errorf("invalid DATABASE_URL: %w", err)
	}
	if parsed.Scheme != "postgres" && parsed.Scheme != "postgresql" {
		return Config{}, fmt.Errorf("invalid DATABASE_URL scheme %q: expected postgres or postgresql", parsed.Scheme)
	}

	return cfg, nil
}

// IsProduction reports whether the app is running with APP_ENV=production.
func (c Config) IsProduction() bool {
	return c.AppEnv == "production"
}

func getenv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok && value != "" {
		return value
	}
	return fallback
}
