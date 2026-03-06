// Seed populates the database with sample data for local development.
// Usage: go run ./cmd/seed [--reset]
// --reset drops and recreates tables (destructive). Without it, appends data.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"linkup-backend/models"
)

func main() {
	reset := flag.Bool("reset", false, "reset DB (drop and migrate) before seeding")
	flag.Parse()

	dbPath := os.Getenv("DATABASE_URL")
	if dbPath == "" {
		dbPath = "linkup.db"
	}

	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		log.Fatalf("open db: %v", err)
	}

	if *reset {
		if err := db.Migrator().DropTable(
			&models.AuthCode{}, &models.Availability{}, &models.Invitation{},
			&models.Participant{}, &models.Event{}, &models.User{},
		); err != nil {
			log.Fatalf("drop tables: %v", err)
		}
		log.Println("tables dropped")
	}

	if err := db.AutoMigrate(
		&models.User{}, &models.AuthCode{}, &models.Event{},
		&models.Availability{}, &models.Invitation{}, &models.Participant{},
	); err != nil {
		log.Fatalf("migrate: %v", err)
	}

	// Ensure seed user
	var user models.User
	if err := db.Where("email = ?", "dev@localhost").First(&user).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			user = models.User{Email: "dev@localhost", Name: "Dev User"}
			if err := db.Create(&user).Error; err != nil {
				log.Fatalf("create user: %v", err)
			}
			log.Println("created user dev@localhost")
		} else {
			log.Fatalf("find user: %v", err)
		}
	} else {
		log.Println("user dev@localhost exists")
	}

	// Ensure seed event with invitation and participants
	var event models.Event
	if err := db.Where("creator_id = ? AND title = ?", user.ID, "Seed Meeting").First(&event).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			event = models.Event{
				CreatorID:       user.ID,
				Title:           "Seed Meeting",
				Description:     "Sample event for local testing",
				DurationMinutes: 30,
			}
			if err := db.Create(&event).Error; err != nil {
				log.Fatalf("create event: %v", err)
			}
			token := "seed-invite-token-" + fmt.Sprintf("%d", event.ID)
			expiresAt := time.Now().Add(30 * 24 * time.Hour)
			inv := models.Invitation{EventID: event.ID, Token: token, ExpiresAt: &expiresAt}
			if err := db.Create(&inv).Error; err != nil {
				log.Fatalf("create invitation: %v", err)
			}
			for _, email := range []string{"alice@example.com", "bob@example.com"} {
				p := models.Participant{EventID: event.ID, Email: email}
				db.Where("event_id = ? AND email = ?", event.ID, email).FirstOrCreate(&p)
			}
			log.Printf("created event id=%d, share link /inv/%s\n", event.ID, token)
		} else {
			log.Fatalf("find event: %v", err)
		}
	} else {
		log.Printf("event \"Seed Meeting\" exists id=%d\n", event.ID)
	}

	log.Println("seed done")
}
