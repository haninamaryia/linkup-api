// Package database provides SQLite connection and schema migration.
package database

import (
	"log"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"linkup-backend/internal/models"
)

// DBConfig is implemented by config.Config so database stays decoupled.
type DBConfig interface {
	GetDatabaseURL() string
}

// Connect opens SQLite, runs AutoMigrate on all models, returns *gorm.DB.
func Connect(cfg DBConfig) *gorm.DB {
	db, err := gorm.Open(sqlite.Open(cfg.GetDatabaseURL()), &gorm.Config{})
	if err != nil {
		log.Fatal("failed to connect database:", err)
	}

	db.AutoMigrate(
		&models.User{},
		&models.Event{},
		&models.Availability{},
		&models.Invitation{},
		&models.Participant{},
	)

	return db
}
