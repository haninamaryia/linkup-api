package services

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"gorm.io/gorm"

	"linkup-backend/models"
)

const defaultCodeTTL = 10 * time.Minute

// AuthService handles email+code auth (no passwords). RequestCode creates/finds user and stores
// a one-time code in the database with TTL. VerifyCode checks the code, deletes it, and returns userID+email.
type AuthService struct {
	db         *gorm.DB
	jwtSecret  string
	codeTTL    time.Duration
	rateLimiter AuthCodeRateLimiter // optional; if set, RequestCode checks before creating code
}

func NewAuthService(db *gorm.DB, jwtSecret string, codeTTL time.Duration) *AuthService {
	if codeTTL <= 0 {
		codeTTL = defaultCodeTTL
	}
	return &AuthService{
		db:        db,
		jwtSecret: jwtSecret,
		codeTTL:   codeTTL,
	}
}

// SetRateLimiter sets an optional rate limiter for RequestCode (per-email).
func (s *AuthService) SetRateLimiter(r AuthCodeRateLimiter) {
	s.rateLimiter = r
}

func (s *AuthService) RequestCode(email, name string) (string, error) {
	if s.rateLimiter != nil && !s.rateLimiter.Allow(email) {
		return "", ErrRateLimited
	}
	var user models.User
	err := s.db.Where("email = ?", email).First(&user).Error
	if err == gorm.ErrRecordNotFound {
		user = models.User{Email: email, Name: name}
		if err := s.db.Create(&user).Error; err != nil {
			return "", err
		}
	} else if err != nil {
		return "", err
	}

	code := generateCode()
	expiresAt := time.Now().Add(s.codeTTL)
	row := models.AuthCode{
		Code:      code,
		Email:     user.Email,
		UserID:    user.ID,
		ExpiresAt: expiresAt,
	}
	if err := s.db.Create(&row).Error; err != nil {
		return "", err
	}

	// Opportunistic cleanup of expired codes to keep table small
	go s.cleanupExpiredCodes()

	fmt.Printf("[DEV] Verification code for %s: %s\n", email, code)
	return code, nil
}

// VerifyCode validates email+code, deletes code from DB, returns userID and email.
func (s *AuthService) VerifyCode(email, code string) (uint, string, error) {
	var row models.AuthCode
	err := s.db.Where("code = ? AND email = ?", code, email).First(&row).Error
	if err == gorm.ErrRecordNotFound {
		return 0, "", ErrInvalidCode
	}
	if err != nil {
		return 0, "", err
	}
	if time.Now().After(row.ExpiresAt) {
		_ = s.db.Delete(&row).Error
		return 0, "", ErrInvalidCode
	}

	if err := s.db.Delete(&row).Error; err != nil {
		return 0, "", err
	}
	// Cleanup expired codes when we're already in DB
	s.cleanupExpiredCodes()

	return row.UserID, row.Email, nil
}

func (s *AuthService) cleanupExpiredCodes() {
	s.db.Where("expires_at < ?", time.Now()).Delete(&models.AuthCode{})
}

func generateCode() string {
	b := make([]byte, 4)
	rand.Read(b)
	return hex.EncodeToString(b)
}
