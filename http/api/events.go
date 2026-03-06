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
	schedulingService  *services.SchedulingService
}

func NewEventsHandler(db *gorm.DB, jwtSecret string, participantService *services.ParticipantService, emailSender services.EmailSender, schedulingService *services.SchedulingService) *EventsHandler {
	return &EventsHandler{db: db, jwtSecret: jwtSecret, participantService: participantService, emailSender: emailSender, schedulingService: schedulingService}
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
		return respondError(c, fiber.StatusUnauthorized, "unauthorized")
	}

	var req CreateEventRequest
	if err := c.BodyParser(&req); err != nil {
		return respondError(c, fiber.StatusBadRequest, "invalid request")
	}
	if req.Title == "" {
		return respondError(c, fiber.StatusBadRequest, "title required")
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
	if err := validateTimeFrame(event.TimeFrameStart, event.TimeFrameEnd); err != nil {
		return respondError(c, fiber.StatusBadRequest, err.Error())
	}

	if err := h.db.Create(&event).Error; err != nil {
		return respondError(c, fiber.StatusInternalServerError, err.Error())
	}

	token := generateInvitationToken()
	expiresAt := time.Now().Add(30 * 24 * time.Hour) // 30 days
	inv := models.Invitation{EventID: event.ID, Token: token, ExpiresAt: &expiresAt}
	if err := h.db.Create(&inv).Error; err != nil {
		return respondError(c, fiber.StatusInternalServerError, err.Error())
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
		return respondError(c, fiber.StatusBadRequest, "invalid event id")
	}

	var event models.Event
	if err := h.db.First(&event, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return respondError(c, fiber.StatusNotFound, "event not found")
		}
		return respondError(c, fiber.StatusInternalServerError, err.Error())
	}
	token := h.getInvitationToken(event.ID)
	return c.JSON(toEventResponse(&event, token))
}

// ListEvents: organizer's events only (from JWT creator_id).
func (h *EventsHandler) ListEvents(c *fiber.Ctx) error {
	userID, err := getUserIDFromContext(c, h.jwtSecret)
	if err != nil {
		return respondError(c, fiber.StatusUnauthorized, "unauthorized")
	}

	var events []models.Event
	if err := h.db.Where("creator_id = ?", userID).Find(&events).Error; err != nil {
		return respondError(c, fiber.StatusInternalServerError, err.Error())
	}

	result := make([]EventResponse, len(events))
	for i := range events {
		token := h.getInvitationToken(events[i].ID)
		result[i] = toEventResponse(&events[i], token)
	}
	return c.JSON(result)
}

// UpdateEvent: organizer only. Supports partial update (title, description, participants, etc.).
// Only updates the existing event in place; never creates a new event.
func (h *EventsHandler) UpdateEvent(c *fiber.Ctx) error {
	event, id, ok := h.getEventForOrganizer(c)
	if !ok {
		return nil
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
		return respondError(c, fiber.StatusBadRequest, "invalid request")
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
	// Validate time frame consistency after applying updates (reject end before start)
	if req.TimeFrameStart != nil || req.TimeFrameEnd != nil {
		var start, end *time.Time
		if req.TimeFrameStart != nil {
			if t, err := time.Parse(time.RFC3339, *req.TimeFrameStart); err == nil {
				start = &t
			}
		} else if event.TimeFrameStart != nil {
			start = event.TimeFrameStart
		}
		if req.TimeFrameEnd != nil {
			if t, err := time.Parse(time.RFC3339, *req.TimeFrameEnd); err == nil {
				end = &t
			}
		} else if event.TimeFrameEnd != nil {
			end = event.TimeFrameEnd
		}
		if err := validateTimeFrame(start, end); err != nil {
			return respondError(c, fiber.StatusBadRequest, err.Error())
		}
	}
	if len(updates) > 0 {
		if err := h.db.Model(&event).Updates(updates).Error; err != nil {
			return respondError(c, fiber.StatusInternalServerError, err.Error())
		}
	}
	if req.ParticipantEmails != nil && h.participantService != nil {
		newSet := make(map[string]bool)
		for _, e := range req.ParticipantEmails {
			if e != "" {
				newSet[e] = true
			}
		}
		var current []models.Participant
		h.db.Where("event_id = ?", event.ID).Find(&current)
		for _, p := range current {
			if !newSet[p.Email] {
				h.db.Where("event_id = ? AND participant_id = ?", event.ID, p.ID).Delete(&models.Availability{})
				h.db.Delete(&p)
			}
		}
		_ = h.participantService.AddParticipants(event.ID, req.ParticipantEmails)
	}

	h.db.First(event, id)
	token := h.getInvitationToken(event.ID)
	return c.JSON(toEventResponse(event, token))
}

// participantStatusData returns participants with slots and aggregated counts for the event. Used by GetParticipantStatus and GetEventSummary.
func (h *EventsHandler) participantStatusData(eventID uint) (participants []services.ParticipantWithSlots, respondedCount, totalCount, percentResponded int) {
	if h.participantService == nil {
		return []services.ParticipantWithSlots{}, 0, 0, 0
	}
	participants, _ = h.participantService.GetParticipantStatusWithSlots(eventID)
	if participants == nil {
		participants = []services.ParticipantWithSlots{}
	}
	totalCount = len(participants)
	for _, p := range participants {
		if p.Responded {
			respondedCount++
		}
	}
	if totalCount > 0 {
		percentResponded = (respondedCount * 100) / totalCount
	}
	return participants, respondedCount, totalCount, percentResponded
}

// GetParticipantStatus: organizer only. Returns participants with their submitted slots, responded_count, percent_responded.
func (h *EventsHandler) GetParticipantStatus(c *fiber.Ctx) error {
	_, id, ok := h.getEventForOrganizer(c)
	if !ok {
		return nil
	}

	if h.participantService == nil {
		return c.JSON(fiber.Map{"participants": []interface{}{}, "responded_count": 0, "total_count": 0, "percent_responded": 0})
	}
	participants, responded, total, percent := h.participantStatusData(uint(id))
	return c.JSON(fiber.Map{
		"participants":      participants,
		"responded_count":   responded,
		"total_count":       total,
		"percent_responded": percent,
	})
}

// EventSummaryResponse is the stable response shape for GET /events/:id/summary.
type EventSummaryResponse struct {
	Event                  EventResponse                   `json:"event"`
	Participants           []services.ParticipantWithSlots `json:"participants"`
	RespondedCount         int                             `json:"responded_count"`
	TotalCount             int                             `json:"total_count"`
	PercentResponded       int                             `json:"percent_responded"`
	BestTimes              []BestTimeResponse              `json:"best_times"` // top 1-3 recommendations
	Note                   string                          `json:"note,omitempty"`
	ExcludedParticipantIDs []uint                          `json:"excluded_participant_ids,omitempty"`
}

const summaryBestTimesLimit = 3

// GetEventSummary: organizer only. Returns event details, participants with slots, and top 1-3 best-time recommendations in one response.
func (h *EventsHandler) GetEventSummary(c *fiber.Ctx) error {
	event, id, ok := h.getEventForOrganizer(c)
	if !ok {
		return nil
	}

	token := h.getInvitationToken(event.ID)
	eventResp := toEventResponse(event, token)
	participants, respondedCount, totalCount, percentResponded := h.participantStatusData(id)

	bestTimes := []BestTimeResponse{}
	var note string
	var excludedIDs []uint
	if h.schedulingService != nil {
		resp, err := h.schedulingService.GetBestTimesResponse(id, 0)
		if err == nil {
			n := len(resp.Slots)
			if n > summaryBestTimesLimit {
				n = summaryBestTimesLimit
			}
			for i := 0; i < n; i++ {
				bestTimes = append(bestTimes, formatBestTimeResult(resp.Slots[i]))
			}
			note = resp.Note
			excludedIDs = resp.ExcludedParticipantIDs
		}
	}

	return c.JSON(EventSummaryResponse{
		Event:                  eventResp,
		Participants:           participants,
		RespondedCount:         respondedCount,
		TotalCount:             totalCount,
		PercentResponded:       percentResponded,
		BestTimes:              bestTimes,
		Note:                   note,
		ExcludedParticipantIDs: excludedIDs,
	})
}

// DeleteEvent: organizer only. Cascades: availabilities, participants, invitations.
func (h *EventsHandler) DeleteEvent(c *fiber.Ctx) error {
	event, id, ok := h.getEventForOrganizer(c)
	if !ok {
		return nil
	}

	h.db.Where("event_id = ?", id).Delete(&models.Availability{})
	h.db.Where("event_id = ?", id).Delete(&models.Participant{})
	h.db.Where("event_id = ?", id).Delete(&models.Invitation{})
	if err := h.db.Delete(event).Error; err != nil {
		return respondError(c, fiber.StatusInternalServerError, err.Error())
	}
	return c.Status(fiber.StatusNoContent).Send(nil)
}

// getEventForOrganizer validates JWT, loads the event by :id, and verifies the user is the creator.
// Returns (event, id, true) on success. On failure writes the response to c and returns (nil, 0, false); the handler should then return nil.
func (h *EventsHandler) getEventForOrganizer(c *fiber.Ctx) (*models.Event, uint, bool) {
	userID, err := getUserIDFromContext(c, h.jwtSecret)
	if err != nil {
		_ = respondError(c, fiber.StatusUnauthorized, "unauthorized")
		return nil, 0, false
	}
	id, err := c.ParamsInt("id")
	if err != nil {
		_ = respondError(c, fiber.StatusBadRequest, "invalid event id")
		return nil, 0, false
	}
	var event models.Event
	if err := h.db.First(&event, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			_ = respondError(c, fiber.StatusNotFound, "event not found")
		} else {
			_ = respondError(c, fiber.StatusInternalServerError, err.Error())
		}
		return nil, 0, false
	}
	if event.CreatorID != userID {
		_ = respondError(c, fiber.StatusForbidden, "not event creator")
		return nil, 0, false
	}
	return &event, uint(id), true
}

// getInvitationToken returns the invitation token for the event, or "" if none.
func (h *EventsHandler) getInvitationToken(eventID uint) string {
	var inv models.Invitation
	if err := h.db.Where("event_id = ?", eventID).First(&inv).Error; err == nil {
		return inv.Token
	}
	return ""
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
		CreatedAt:       e.CreatedAt.Format(timeFormatISO8601),
	}
	if e.TimeFrameStart != nil {
		s := e.TimeFrameStart.Format(timeFormatISO8601)
		r.TimeFrameStart = &s
	}
	if e.TimeFrameEnd != nil {
		s := e.TimeFrameEnd.Format(timeFormatISO8601)
		r.TimeFrameEnd = &s
	}
	return r
}

func generateInvitationToken() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}
