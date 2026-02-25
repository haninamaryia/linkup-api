package models

import "time"

// Participant is an invitee added by the organizer (email only, no account).
// RespondedAt is set when they submit availability via /inv/:token/availability.
type Participant struct {
	ID            uint       `gorm:"primaryKey"`
	EventID       uint
	Email         string
	InvitationSentAt *time.Time
	RespondedAt     *time.Time
	CreatedAt     time.Time
}
