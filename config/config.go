// Package config loads application settings from environment variables.
package config

import (
	"os"
)

// Config holds server, database, and auth settings.
type Config struct {
	ServerAddress string
	DatabaseURL   string
	JwtSecret     string
}

func (c *Config) GetDatabaseURL() string {
	return c.DatabaseURL
}

// Load reads env vars (SERVER_ADDRESS, DATABASE_URL, JWT_SECRET) with defaults.
func Load() *Config {
	return &Config{
		ServerAddress: getEnv("SERVER_ADDRESS", ":8181"),
		DatabaseURL:   getEnv("DATABASE_URL", "linkup.db"),
		JwtSecret:     getEnv("JWT_SECRET", "dev-secret"),
	}
}

func getEnv(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}
