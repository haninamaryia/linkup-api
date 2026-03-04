package models

import "time"

// Participant is an invitee added by the organizer (email only, no account).
// RespondedAt is set when they submit availability via /inv/:token/availability.
// One row per (event_id, email); duplicate emails for the same event are not allowed.
type Participant struct {
	ID            uint       `gorm:"primaryKey"`
	EventID       uint       `gorm:"uniqueIndex:idx_event_participant_email"`
	Email         string     `gorm:"uniqueIndex:idx_event_participant_email"`
	InvitationSentAt *time.Time
	RespondedAt     *time.Time
	CreatedAt     time.Time
}
