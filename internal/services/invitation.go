package services

import (
	"gorm.io/gorm"

	"linkup-backend/internal/models"
)

// InvitationService resolves invitation tokens to events (participant flow).
type InvitationService struct {
	db *gorm.DB
}

func NewInvitationService(db *gorm.DB) *InvitationService {
	return &InvitationService{db: db}
}

func (s *InvitationService) GetEventByToken(token string) (*models.Event, *models.User, error) {
	var inv models.Invitation
	if err := s.db.Where("token = ?", token).First(&inv).Error; err != nil {
		return nil, nil, err
	}
	var event models.Event
	if err := s.db.First(&event, inv.EventID).Error; err != nil {
		return nil, nil, err
	}
	var creator models.User
	if err := s.db.First(&creator, event.CreatorID).Error; err != nil {
		return nil, nil, err
	}
	return &event, &creator, nil
}

func (s *InvitationService) GetEventIDByToken(token string) (uint, error) {
	var inv models.Invitation
	if err := s.db.Where("token = ?", token).First(&inv).Error; err != nil {
		return 0, err
	}
	return inv.EventID, nil
}
