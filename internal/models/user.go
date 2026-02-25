// Package models defines GORM entities for the Linkup database.
package models

import "time"

// User is an organizer (only organizers have accounts).
// Created via auth/request-code + verify-code flow.
type User struct {
	ID        uint      `gorm:"primaryKey"`
	Email     string    `gorm:"uniqueIndex"`
	Name      string
	CreatedAt time.Time
}
