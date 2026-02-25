package models

import "time"

// Event is created by an organizer. Holds meeting metadata and optional time frame.
type Event struct {
	ID               uint       `gorm:"primaryKey"`
	CreatorID        uint
	Title            string
	Description      string
	Location         string
	DurationMinutes  int         // Meeting duration in minutes
	TimeFrameStart   *time.Time  // Optional: organizer's date range start
	TimeFrameEnd     *time.Time  // Optional: organizer's date range end
	CreatedAt        time.Time
	UpdatedAt        time.Time
}
