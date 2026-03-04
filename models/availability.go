package models

import "time"

// Availability is a time slot submitted by a participant (or organizer).
// UserID for logged-in users; ParticipantID when linked to Participant; both nullable for anonymous.
type Availability struct {
	ID            uint      `gorm:"primaryKey"`
	EventID       uint
	UserID        *uint
	ParticipantID *uint
	SlotStart     time.Time
	SlotEnd       time.Time
}
