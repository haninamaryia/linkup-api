// Package main boots the Linkup API server.
//
// Flow: loadConfig (viper + defaults) → Connect DB → Create Fiber app → Register routes → Listen
package main

import (
	"log"
	"os"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/spf13/viper"
	"linkup-backend/config"
	"linkup-backend/database"
	"linkup-backend/docs"
	"linkup-backend/http/api"
	"linkup-backend/services"
)

const (
	defaultServerAddress         = ":8181"
	defaultDatabaseURL           = "linkup.db"
	defaultJwtSecret             = "dev-secret"
	defaultCORSOrigins           = "http://localhost:3000,http://localhost:5173,http://127.0.0.1:3000,http://127.0.0.1:5173"
	defaultAuthCodeTTL           = "10m"
	defaultAuthCodeRateLimitMax  = 3
	defaultAuthCodeRateLimitWindow = "15m"
)

const (
	configServerAddress          = "server.address"
	configDatabaseURL            = "database.url"
	configJwtSecret              = "jwt.secret"
	configCORSOrigins            = "cors.origins"
	configAuthCodeTTL            = "auth.code_ttl"
	configAuthCodeRateLimitMax   = "auth.code_rate_limit_max"
	configAuthCodeRateLimitWindow = "auth.code_rate_limit_window"
	configEmailProvider          = "email.provider"
)

const (
	defaultConfigPath  = "cmd"
	configPathEnvVar  = "CONFIG_PATH"
)

// swaggerUIHTML is the Swagger UI page that loads the OpenAPI spec from /openapi.yaml.
const swaggerUIHTML = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <title>Linkup API</title>
  <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css">
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
  <script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-standalone-preset.js"></script>
  <script>
    window.onload = function() {
      window.ui = SwaggerUIBundle({
        url: "/openapi.yaml",
        dom_id: "#swagger-ui",
        presets: [
          SwaggerUIBundle.presets.apis,
          SwaggerUIStandalonePreset
        ],
        layout: "StandaloneLayout"
      });
    };
  </script>
</body>
</html>
`

// loadConfig creates a viper config with defaults, optional config file (cmd/config.yml), and env overrides.
func loadConfig() *viper.Viper {
	v := viper.New()

	v.SetDefault(configServerAddress, defaultServerAddress)
	v.SetDefault(configDatabaseURL, defaultDatabaseURL)
	v.SetDefault(configJwtSecret, defaultJwtSecret)
	v.SetDefault(configCORSOrigins, defaultCORSOrigins)
	v.SetDefault(configAuthCodeTTL, defaultAuthCodeTTL)
	v.SetDefault(configAuthCodeRateLimitMax, defaultAuthCodeRateLimitMax)
	v.SetDefault(configAuthCodeRateLimitWindow, defaultAuthCodeRateLimitWindow)
	v.SetDefault(configEmailProvider, "stub")

	configPath := os.Getenv(configPathEnvVar)
	if configPath == "" {
		configPath = defaultConfigPath
	}
	v.AddConfigPath(configPath)
	v.SetConfigName("config")

	v.AutomaticEnv()
	v.SetEnvPrefix("linkup")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			log.Println("No config file found, using defaults")
		} else {
			log.Fatalf("Error loading config file: %v", err)
		}
	}

	return v
}

func main() {
	v := loadConfig()

	cfg := &config.Config{
		ServerAddress: v.GetString(configServerAddress),
		DatabaseURL:   v.GetString(configDatabaseURL),
		JwtSecret:     v.GetString(configJwtSecret),
		CORSOrigins:   v.GetString(configCORSOrigins),
		AuthCodeTTL:   v.GetString(configAuthCodeTTL),
	}

	db := database.Connect(cfg)

	app := fiber.New()
	origins := strings.TrimSpace(cfg.CORSOrigins)
	if origins != "" {
		app.Use(cors.New(cors.Config{
			AllowOrigins: origins,
			AllowHeaders: "Origin, Content-Type, Accept, Authorization",
		}))
	}

	authCodeTTL := cfg.GetAuthCodeTTL()
	rateLimitMax := v.GetInt(configAuthCodeRateLimitMax)
	rateLimitWindow := 15 * time.Minute
	if w := v.GetString(configAuthCodeRateLimitWindow); w != "" {
		if d, err := time.ParseDuration(w); err == nil && d > 0 {
			rateLimitWindow = d
		}
	}
	emailSender := services.NewStubEmailSender()
	// When email.provider is set to a non-stub value, swap for production sender (Resend, etc.)

	api.SetupRoutes(app, db, cfg.JwtSecret, authCodeTTL, rateLimitMax, rateLimitWindow, emailSender)

	// API contract: OpenAPI spec and interactive docs (Swagger UI)
	app.Get("/openapi.yaml", func(c *fiber.Ctx) error {
		c.Set("Content-Type", "application/x-yaml")
		return c.Send(docs.OpenAPIYAML)
	})
	app.Get("/docs", func(c *fiber.Ctx) error {
		c.Set("Content-Type", "text/html; charset=utf-8")
		return c.SendString(swaggerUIHTML)
	})

	log.Println("API running on", cfg.ServerAddress)
	if err := app.Listen(cfg.ServerAddress); err != nil {
		log.Fatal(err)
	}
}
