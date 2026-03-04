// InvitationHandler: participant flow (no auth).
// GET /inv/:token → event details for share link. POST /inv/:token/availability → submit slots.
// If email is provided in POST body, participant is marked as responded.
package api

import (
	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"

	"linkup-backend/models"
	"linkup-backend/services"
)

type InvitationHandler struct {
	invService         *services.InvitationService
	participantService *services.ParticipantService
	db                 *gorm.DB
}

func NewInvitationHandler(invService *services.InvitationService, participantService *services.ParticipantService, db *gorm.DB) *InvitationHandler {
	return &InvitationHandler{invService: invService, participantService: participantService, db: db}
}

type InvEventResponse struct {
	ID               uint    `json:"id"`
	Title            string  `json:"title"`
	Description      string  `json:"description"`
	Location         string  `json:"location"`
	DurationMinutes  int     `json:"duration_minutes"`
	TimeFrameStart   *string `json:"time_frame_start,omitempty"`
	TimeFrameEnd     *string `json:"time_frame_end,omitempty"`
	OrganizerName    string  `json:"organizer_name"`
}

// GetByToken: resolves token to event + organizer name for participant landing page.
func (h *InvitationHandler) GetByToken(c *fiber.Ctx) error {
	token := c.Params("token")
	if token == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "token required"})
	}

	event, creator, err := h.invService.GetEventByToken(token)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "invitation not found"})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	resp := InvEventResponse{
		ID:              event.ID,
		Title:           event.Title,
		Description:     event.Description,
		Location:        event.Location,
		DurationMinutes: event.DurationMinutes,
		OrganizerName:   creator.Name,
	}
	if event.TimeFrameStart != nil {
		s := event.TimeFrameStart.Format("2006-01-02T15:04:05Z07:00")
		resp.TimeFrameStart = &s
	}
	if event.TimeFrameEnd != nil {
		s := event.TimeFrameEnd.Format("2006-01-02T15:04:05Z07:00")
		resp.TimeFrameEnd = &s
	}
	return c.JSON(resp)
}

type InvSubmitAvailabilityRequest struct {
	Email     string `json:"email"`      // optional, to mark participant as responded
	SlotStart string `json:"slot_start"`
	SlotEnd   string `json:"slot_end"`
}

// SubmitAvailability: create Availability, optionally mark participant responded if email matches.
func (h *InvitationHandler) SubmitAvailability(c *fiber.Ctx) error {
	token := c.Params("token")
	if token == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "token required"})
	}

	eventID, err := h.invService.GetEventIDByToken(token)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "invitation not found"})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	var req InvSubmitAvailabilityRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid request"})
	}
	if req.SlotStart == "" || req.SlotEnd == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "slot_start and slot_end required"})
	}

	slotStart, err := parseISO8601(req.SlotStart)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid slot_start format"})
	}
	slotEnd, err := parseISO8601(req.SlotEnd)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid slot_end format"})
	}
	if !slotEnd.After(slotStart) {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "slot_end must be after slot_start"})
	}

	var participantID *uint
	if req.Email != "" {
		participant, err := h.participantService.FindOrCreateParticipant(eventID, req.Email)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
		}
		participantID = &participant.ID
	}

	avail := models.Availability{
		EventID:       eventID,
		ParticipantID: participantID,
		SlotStart:     slotStart,
		SlotEnd:       slotEnd,
	}
	if err := h.db.Create(&avail).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	if req.Email != "" {
		_ = h.participantService.MarkRespondedByEmail(eventID, req.Email)
	}

	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"message": "availability submitted",
		"id":      avail.ID,
	})
}
