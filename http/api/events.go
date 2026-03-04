// EventsHandler: organizer CRUD for events. All except GetEvent require JWT (creator only for update/delete).
// CreateEvent also creates Invitation, adds Participants, and triggers email invites (stub).
package api

import (
	"crypto/rand"
	"encoding/hex"
	"time"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"

	"linkup-backend/models"
	"linkup-backend/services"
	"linkup-backend/utils"
)

type EventsHandler struct {
	db                 *gorm.DB
	jwtSecret          string
	participantService *services.ParticipantService
	emailSender        services.EmailSender
}

func NewEventsHandler(db *gorm.DB, jwtSecret string, participantService *services.ParticipantService, emailSender services.EmailSender) *EventsHandler {
	return &EventsHandler{db: db, jwtSecret: jwtSecret, participantService: participantService, emailSender: emailSender}
}

type CreateEventRequest struct {
	Title             string   `json:"title"`
	Description       string   `json:"description"`
	Location          string   `json:"location"`
	DurationMinutes   int      `json:"duration_minutes"`
	TimeFrameStart    string   `json:"time_frame_start"` // ISO8601
	TimeFrameEnd      string   `json:"time_frame_end"`   // ISO8601
	ParticipantEmails []string `json:"participant_emails"`
}

type EventResponse struct {
	ID              uint    `json:"id"`
	CreatorID       uint    `json:"creator_id"`
	Title           string  `json:"title"`
	Description     string  `json:"description"`
	Location        string  `json:"location"`
	DurationMinutes int     `json:"duration_minutes"`
	TimeFrameStart  *string `json:"time_frame_start,omitempty"`
	TimeFrameEnd    *string `json:"time_frame_end,omitempty"`
	ShareLink       string  `json:"share_link"`
	CreatedAt       string  `json:"created_at"`
}

// CreateEvent: parse JWT → create Event + Invitation → add Participants → (optionally) SendInvite via EmailSender.
func (h *EventsHandler) CreateEvent(c *fiber.Ctx) error {
	userID, err := getUserIDFromContext(c, h.jwtSecret)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
	}

	var req CreateEventRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid request"})
	}
	if req.Title == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "title required"})
	}

	event := models.Event{
		CreatorID:       userID,
		Title:           req.Title,
		Description:     req.Description,
		Location:        req.Location,
		DurationMinutes: req.DurationMinutes,
	}
	if event.DurationMinutes <= 0 {
		event.DurationMinutes = 60
	}
	if req.TimeFrameStart != "" {
		if t, err := time.Parse(time.RFC3339, req.TimeFrameStart); err == nil {
			event.TimeFrameStart = &t
		}
	}
	if req.TimeFrameEnd != "" {
		if t, err := time.Parse(time.RFC3339, req.TimeFrameEnd); err == nil {
			event.TimeFrameEnd = &t
		}
	}

	if err := h.db.Create(&event).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	token := generateInvitationToken()
	inv := models.Invitation{EventID: event.ID, Token: token}
	if err := h.db.Create(&inv).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	// Add participants and trigger invite emails (TODO: real email in production)
	if h.participantService != nil && len(req.ParticipantEmails) > 0 {
		_ = h.participantService.AddParticipants(event.ID, req.ParticipantEmails)
		if h.emailSender != nil {
			var creator models.User
			if err := h.db.First(&creator, userID).Error; err == nil {
				shareLink := "/inv/" + token
				for _, email := range req.ParticipantEmails {
					if email != "" {
						_ = h.emailSender.SendInvite(email, event.Title, creator.Name, shareLink)
					}
				}
			}
		}
	}

	return c.Status(fiber.StatusCreated).JSON(toEventResponse(&event, token))
}

// GetEvent: public, no auth. Returns event + share_link.
func (h *EventsHandler) GetEvent(c *fiber.Ctx) error {
	id, err := c.ParamsInt("id")
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid event id"})
	}

	var event models.Event
	if err := h.db.First(&event, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "event not found"})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	var inv models.Invitation
	if err := h.db.Where("event_id = ?", event.ID).First(&inv).Error; err == nil {
		return c.JSON(toEventResponse(&event, inv.Token))
	}
	return c.JSON(toEventResponse(&event, ""))
}

// ListEvents: organizer's events only (from JWT creator_id).
func (h *EventsHandler) ListEvents(c *fiber.Ctx) error {
	userID, err := getUserIDFromContext(c, h.jwtSecret)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
	}

	var events []models.Event
	if err := h.db.Where("creator_id = ?", userID).Find(&events).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	result := make([]EventResponse, len(events))
	for i := range events {
		var inv models.Invitation
		token := ""
		if err := h.db.Where("event_id = ?", events[i].ID).First(&inv).Error; err == nil {
			token = inv.Token
		}
		result[i] = toEventResponse(&events[i], token)
	}
	return c.JSON(result)
}

// UpdateEvent: organizer only. Supports partial update (title, description, participants, etc.).
// Only updates the existing event in place; never creates a new event.
func (h *EventsHandler) UpdateEvent(c *fiber.Ctx) error {
	userID, err := getUserIDFromContext(c, h.jwtSecret)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
	}

	id, err := c.ParamsInt("id")
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid event id"})
	}

	var event models.Event
	if err := h.db.First(&event, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "event not found"})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	if event.CreatorID != userID {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "not event creator"})
	}

	var req struct {
		Title             *string  `json:"title"`
		Description       *string  `json:"description"`
		Location          *string  `json:"location"`
		DurationMinutes   *int     `json:"duration_minutes"`
		TimeFrameStart    *string  `json:"time_frame_start"`
		TimeFrameEnd      *string  `json:"time_frame_end"`
		ParticipantEmails []string `json:"participant_emails"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid request"})
	}

	updates := make(map[string]interface{})
	if req.Title != nil {
		updates["title"] = *req.Title
	}
	if req.Description != nil {
		updates["description"] = *req.Description
	}
	if req.Location != nil {
		updates["location"] = *req.Location
	}
	if req.DurationMinutes != nil {
		updates["duration_minutes"] = *req.DurationMinutes
	}
	if req.TimeFrameStart != nil {
		if t, err := time.Parse(time.RFC3339, *req.TimeFrameStart); err == nil {
			updates["time_frame_start"] = t
		}
	}
	if req.TimeFrameEnd != nil {
		if t, err := time.Parse(time.RFC3339, *req.TimeFrameEnd); err == nil {
			updates["time_frame_end"] = t
		}
	}
	if len(updates) > 0 {
		if err := h.db.Model(&event).Updates(updates).Error; err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
		}
	}
	if req.ParticipantEmails != nil && h.participantService != nil {
		// TODO: Regenerate invites / resend emails when participants change
		// Replace participants - delete existing and add new (simple approach)
		h.db.Where("event_id = ?", event.ID).Delete(&models.Participant{})
		_ = h.participantService.AddParticipants(event.ID, req.ParticipantEmails)
	}

	h.db.First(&event, id)
	var inv models.Invitation
	token := ""
	if err := h.db.Where("event_id = ?", event.ID).First(&inv).Error; err == nil {
		token = inv.Token
	}
	return c.JSON(toEventResponse(&event, token))
}

// GetParticipantStatus: organizer only. Returns participants with their submitted slots, responded_count, percent_responded.
func (h *EventsHandler) GetParticipantStatus(c *fiber.Ctx) error {
	userID, err := getUserIDFromContext(c, h.jwtSecret)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
	}

	id, err := c.ParamsInt("id")
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid event id"})
	}

	var event models.Event
	if err := h.db.First(&event, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "event not found"})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	if event.CreatorID != userID {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "not event creator"})
	}

	if h.participantService == nil {
		return c.JSON(fiber.Map{"participants": []interface{}{}, "responded_count": 0, "total_count": 0, "percent_responded": 0})
	}
	withSlots, err := h.participantService.GetParticipantStatusWithSlots(uint(id))
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	responded := 0
	for _, p := range withSlots {
		if p.Responded {
			responded++
		}
	}
	total := len(withSlots)
	percent := 0
	if total > 0 {
		percent = (responded * 100) / total
	}
	return c.JSON(fiber.Map{
		"participants":      withSlots,
		"responded_count":   responded,
		"total_count":       total,
		"percent_responded": percent,
	})
}

// DeleteEvent: organizer only. Cascades: availabilities, participants, invitations.
func (h *EventsHandler) DeleteEvent(c *fiber.Ctx) error {
	userID, err := getUserIDFromContext(c, h.jwtSecret)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
	}

	id, err := c.ParamsInt("id")
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid event id"})
	}

	var event models.Event
	if err := h.db.First(&event, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "event not found"})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	if event.CreatorID != userID {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "not event creator"})
	}

	h.db.Where("event_id = ?", id).Delete(&models.Availability{})
	h.db.Where("event_id = ?", id).Delete(&models.Participant{})
	h.db.Where("event_id = ?", id).Delete(&models.Invitation{})
	if err := h.db.Delete(&event).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.Status(fiber.StatusNoContent).Send(nil)
}

// getUserIDFromContext extracts and validates JWT from Authorization: Bearer header.
func getUserIDFromContext(c *fiber.Ctx, secret string) (uint, error) {
	auth := c.Get("Authorization")
	if len(auth) < 8 || auth[:7] != "Bearer " {
		return 0, fiber.ErrUnauthorized
	}
	tokenStr := auth[7:]
	claims, err := utils.ParseToken(tokenStr, secret)
	if err != nil {
		return 0, err
	}
	return claims.UserID, nil
}

func toEventResponse(e *models.Event, token string) EventResponse {
	shareLink := ""
	if token != "" {
		shareLink = "/inv/" + token
	}
	r := EventResponse{
		ID:              e.ID,
		CreatorID:       e.CreatorID,
		Title:           e.Title,
		Description:     e.Description,
		Location:        e.Location,
		DurationMinutes: e.DurationMinutes,
		ShareLink:       shareLink,
		CreatedAt:       e.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}
	if e.TimeFrameStart != nil {
		s := e.TimeFrameStart.Format("2006-01-02T15:04:05Z07:00")
		r.TimeFrameStart = &s
	}
	if e.TimeFrameEnd != nil {
		s := e.TimeFrameEnd.Format("2006-01-02T15:04:05Z07:00")
		r.TimeFrameEnd = &s
	}
	return r
}

func generateInvitationToken() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}
