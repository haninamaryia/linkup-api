package models

import "time"

// Invitation holds the shareable token for an event. One per event.
// GET /inv/:token returns event details; POST /inv/:token/availability submits slots.
type Invitation struct {
	ID        uint      `gorm:"primaryKey"`
	EventID   uint
	Token     string    `gorm:"uniqueIndex"`
	CreatedAt time.Time
}
