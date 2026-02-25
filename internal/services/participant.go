package services

import (
	"time"

	"gorm.io/gorm"

	"linkup-backend/internal/models"
)

// ParticipantService manages invitees (participant_emails) and marks who has responded.
type ParticipantService struct {
	db *gorm.DB
}

func NewParticipantService(db *gorm.DB) *ParticipantService {
	return &ParticipantService{db: db}
}

func (s *ParticipantService) AddParticipants(eventID uint, emails []string) error {
	for _, email := range emails {
		if email == "" {
			continue
		}
		p := models.Participant{EventID: eventID, Email: email}
		if err := s.db.Create(&p).Error; err != nil {
			return err
		}
	}
	return nil
}

func (s *ParticipantService) MarkResponded(participantID uint) error {
	now := time.Now()
	return s.db.Model(&models.Participant{}).Where("id = ?", participantID).Update("responded_at", now).Error
}

func (s *ParticipantService) MarkRespondedByEmail(eventID uint, email string) error {
	now := time.Now()
	return s.db.Model(&models.Participant{}).
		Where("event_id = ? AND email = ?", eventID, email).
		Update("responded_at", now).Error
}

func (s *ParticipantService) GetParticipantStatus(eventID uint) ([]ParticipantStatus, error) {
	var participants []models.Participant
	if err := s.db.Where("event_id = ?", eventID).Find(&participants).Error; err != nil {
		return nil, err
	}

	var result []ParticipantStatus
	for _, p := range participants {
		responded := p.RespondedAt != nil
		result = append(result, ParticipantStatus{
			ID:         p.ID,
			Email:      p.Email,
			Responded:  responded,
			RespondedAt: p.RespondedAt,
		})
	}
	return result, nil
}

type ParticipantStatus struct {
	ID          uint       `json:"id"`
	Email       string     `json:"email"`
	Responded   bool       `json:"responded"`
	RespondedAt *time.Time `json:"responded_at,omitempty"`
}
