package services

import (
	"time"

	"gorm.io/gorm"

	"linkup-backend/models"
)

// ParticipantService manages invitees (participant_emails) and marks who has responded.
type ParticipantService struct {
	db *gorm.DB
}

func NewParticipantService(db *gorm.DB) *ParticipantService {
	return &ParticipantService{db: db}
}

// FindOrCreateParticipant finds a participant by event ID and email, or creates one if not found.
// Used when a respondent submits availability with an email (invitation flow).
func (s *ParticipantService) FindOrCreateParticipant(eventID uint, email string) (*models.Participant, error) {
	var p models.Participant
	err := s.db.Where("event_id = ? AND email = ?", eventID, email).First(&p).Error
	if err == nil {
		return &p, nil
	}
	if err != gorm.ErrRecordNotFound {
		return nil, err
	}
	p = models.Participant{EventID: eventID, Email: email}
	if err := s.db.Create(&p).Error; err != nil {
		return nil, err
	}
	return &p, nil
}

func (s *ParticipantService) AddParticipants(eventID uint, emails []string) error {
	for _, email := range emails {
		if email == "" {
			continue
		}
		// Find or create: do not create a second participant for the same event+email
		p := models.Participant{EventID: eventID, Email: email}
		err := s.db.Where("event_id = ? AND email = ?", eventID, email).FirstOrCreate(&p).Error
		if err != nil {
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
			ID:          p.ID,
			Email:       p.Email,
			Responded:   responded,
			RespondedAt: p.RespondedAt,
		})
	}
	return result, nil
}

// ParticipantWithSlots is participant status plus their submitted availability slots.
type ParticipantWithSlots struct {
	ID          uint       `json:"id"`
	Email       string     `json:"email"`
	Responded   bool       `json:"responded"`
	RespondedAt *time.Time `json:"responded_at,omitempty"`
	Slots       []SlotInfo `json:"slots"`
}

// SlotInfo is a single availability slot for JSON output.
type SlotInfo struct {
	SlotStart string `json:"slot_start"`
	SlotEnd   string `json:"slot_end"`
}

const timeFormatISO8601 = "2006-01-02T15:04:05Z07:00"

// GetParticipantStatusWithSlots returns all participants for an event with their submitted slots.
func (s *ParticipantService) GetParticipantStatusWithSlots(eventID uint) ([]ParticipantWithSlots, error) {
	var participants []models.Participant
	if err := s.db.Where("event_id = ?", eventID).Find(&participants).Error; err != nil {
		return nil, err
	}

	var availabilities []models.Availability
	if err := s.db.Where("event_id = ?", eventID).Find(&availabilities).Error; err != nil {
		return nil, err
	}

	// Group availabilities by participant_id (nil = anonymous, we don't attach to any participant in response)
	slotsByParticipant := make(map[uint][]SlotInfo)
	for _, a := range availabilities {
		if a.ParticipantID == nil {
			continue
		}
		slotsByParticipant[*a.ParticipantID] = append(slotsByParticipant[*a.ParticipantID], SlotInfo{
			SlotStart: a.SlotStart.Format(timeFormatISO8601),
			SlotEnd:   a.SlotEnd.Format(timeFormatISO8601),
		})
	}

	result := make([]ParticipantWithSlots, 0, len(participants))
	for _, p := range participants {
		slots := slotsByParticipant[p.ID]
		if slots == nil {
			slots = []SlotInfo{}
		}
		responded := p.RespondedAt != nil
		result = append(result, ParticipantWithSlots{
			ID:          p.ID,
			Email:       p.Email,
			Responded:   responded,
			RespondedAt: p.RespondedAt,
			Slots:       slots,
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
