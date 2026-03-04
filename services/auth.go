package services

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"

	"gorm.io/gorm"

	"linkup-backend/models"
)

// AuthService handles email+code auth (no passwords). RequestCode creates/finds user and stores
// a one-time code in memory. VerifyCode checks the code and returns userID+email for JWT issuance.
type AuthService struct {
	db         *gorm.DB
	jwtSecret  string
	codeStore  map[string]verificationEntry
	codeStoreMu sync.RWMutex
}

// TODO: Persist verification codes in DB or Redis for multi-instance and restart resilience.
type verificationEntry struct {
	UserID uint
	Email  string
}

func NewAuthService(db *gorm.DB, jwtSecret string) *AuthService {
	return &AuthService{
		db:        db,
		jwtSecret: jwtSecret,
		codeStore: make(map[string]verificationEntry),
	}
}

// RequestCode finds or creates user, generates one-time code, stores in memory, returns code.
// Handlers call VerifyCode with the code; handler then generates JWT.
func (s *AuthService) RequestCode(email, name string) (string, error) {
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
	s.codeStoreMu.Lock()
	s.codeStore[code] = verificationEntry{UserID: user.ID, Email: user.Email}
	s.codeStoreMu.Unlock()

	// TODO: Send email. For now, log the code for development.
	fmt.Printf("[DEV] Verification code for %s: %s\n", email, code)
	return code, nil
}

// VerifyCode validates email+code, deletes code from store, returns userID and email.
func (s *AuthService) VerifyCode(email, code string) (uint, string, error) {
	s.codeStoreMu.RLock()
	entry, ok := s.codeStore[code]
	s.codeStoreMu.RUnlock()

	if !ok || entry.Email != email {
		return 0, "", ErrInvalidCode
	}

	s.codeStoreMu.Lock()
	delete(s.codeStore, code)
	s.codeStoreMu.Unlock()

	return entry.UserID, entry.Email, nil
}

func generateCode() string {
	b := make([]byte, 4)
	rand.Read(b)
	return hex.EncodeToString(b)
}
