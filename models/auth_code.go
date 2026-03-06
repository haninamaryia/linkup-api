package models

import "time"

// AuthCode stores a one-time verification code for passwordless login.
// ExpiresAt is used for TTL; codes are deleted on successful verify or by cleanup.
type AuthCode struct {
	ID        uint      `gorm:"primaryKey"`
	Code      string    `gorm:"uniqueIndex;size:32"`
	Email     string    `gorm:"index;not null"`
	UserID    uint      `gorm:"not null"`
	ExpiresAt time.Time `gorm:"not null;index"`
	CreatedAt time.Time
}
