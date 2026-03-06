package models

import "time"

// Participant is an invitee for an event (email only). One row per (event_id, email); same email cannot appear twice for one event.
type Participant struct {
	ID                uint       `gorm:"primaryKey"`
	EventID           uint       `gorm:"uniqueIndex:idx_event_participant_email"`
	Email             string     `gorm:"uniqueIndex:idx_event_participant_email"`
	InvitationSentAt  *time.Time
	RespondedAt       *time.Time
	CreatedAt         time.Time
}
