// Package config loads application settings from environment variables.
package config

import (
	"os"
	"time"
)

// Config holds server, database, auth, and CORS settings.
type Config struct {
	ServerAddress  string
	DatabaseURL    string
	JwtSecret      string
	CORSOrigins    string // Comma-separated origins for CORS (e.g. https://app.example.com,http://localhost:3000)
	AuthCodeTTL    string // Duration for verification code expiry (e.g. 10m, 1h). Env: AUTH_CODE_TTL
}

func (c *Config) GetDatabaseURL() string {
	return c.DatabaseURL
}

// GetAuthCodeTTL returns the verification code TTL. Parses AuthCodeTTL (e.g. "10m", "1h"); on parse error returns 10m.
func (c *Config) GetAuthCodeTTL() time.Duration {
	d, err := time.ParseDuration(c.AuthCodeTTL)
	if err != nil || d <= 0 {
		return 10 * time.Minute
	}
	return d
}

// Load reads env vars (SERVER_ADDRESS, DATABASE_URL, JWT_SECRET, CORS_ORIGINS, AUTH_CODE_TTL) with defaults.
func Load() *Config {
	return &Config{
		ServerAddress: getEnv("SERVER_ADDRESS", ":8181"),
		DatabaseURL:   getEnv("DATABASE_URL", "linkup.db"),
		JwtSecret:     getEnv("JWT_SECRET", "dev-secret"),
		CORSOrigins:   getEnv("CORS_ORIGINS", "http://localhost:3000,http://localhost:5173,http://127.0.0.1:3000,http://127.0.0.1:5173"),
		AuthCodeTTL:   getEnv("AUTH_CODE_TTL", "10m"),
	}
}

func getEnv(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}
