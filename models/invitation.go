package models

import "time"

// Invitation holds the shareable token for an event. One per event.
// GET /inv/:token returns event details; POST /inv/:token/availability submits slots.
// ExpiresAt: optional; when set, token is rejected after that time (410 Gone).
type Invitation struct {
	ID        uint       `gorm:"primaryKey"`
	EventID   uint
	Token     string     `gorm:"uniqueIndex"`
	ExpiresAt *time.Time // optional; nil = never expires
	CreatedAt time.Time
}
