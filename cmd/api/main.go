// Package main boots the Linkup API server.
//
// Flow: Load config → Connect DB (migrate models) → Create Fiber app → Register routes → Listen
package main

import (
	"log"

	"github.com/gofiber/fiber/v2"
	"linkup-backend/internal/config"
	"linkup-backend/internal/database"
	"linkup-backend/internal/handlers"
)

func main() {
	// 1. Load config from env (SERVER_ADDRESS, DATABASE_URL, JWT_SECRET)
	cfg := config.Load()

	// 2. Connect to SQLite and auto-migrate all models
	db := database.Connect(cfg)

	// 3. Create Fiber HTTP app
	app := fiber.New()

	// 4. Wire handlers to routes (auth, events, availability, invitations)
	handlers.SetupRoutes(app, db, cfg.JwtSecret)

	// 5. Start HTTP server
	log.Println("API running on", cfg.ServerAddress)
	if err := app.Listen(cfg.ServerAddress); err != nil {
		log.Fatal(err)
	}
}
