// InvitationHandler: participant flow (no auth).
// GET /inv/:token → event details for share link. POST /inv/:token/availability → submit slots.
// If email is provided in POST body, participant is marked as responded.
package api

import (
	"fmt"

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
		s := event.TimeFrameStart.Format(timeFormatISO8601)
		resp.TimeFrameStart = &s
	}
	if event.TimeFrameEnd != nil {
		s := event.TimeFrameEnd.Format(timeFormatISO8601)
		resp.TimeFrameEnd = &s
	}
	return c.JSON(resp)
}

type InvSubmitAvailabilityRequest struct {
	Email     string       `json:"email"`      // optional; when set, participant is identified and existing slots are replaced
	SlotStart string       `json:"slot_start"` // required if slots not provided
	SlotEnd   string       `json:"slot_end"`   // required if slots not provided
	Slots     []InvSlot    `json:"slots"`     // optional; if present, replace participant's availability with this set (email required)
}

type InvSlot struct {
	SlotStart string `json:"slot_start"`
	SlotEnd   string `json:"slot_end"`
}

// SubmitAvailability: create or replace availability for the participant.
// When email is provided: deletes existing slots for that participant for this event, then inserts the new one(s).
// Single slot: use slot_start/slot_end. Multiple (or replace-with-none): use slots array.
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

	// Decide payload: slots array vs single slot
	var toInsert []struct{ start, end string }
	if len(req.Slots) > 0 {
		if req.Email == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "email required when using slots array"})
		}
		for i, s := range req.Slots {
			if s.SlotStart == "" || s.SlotEnd == "" {
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "slot_start and slot_end required in each slot"})
			}
			start, err := parseISO8601(s.SlotStart)
			if err != nil {
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": fmt.Sprintf("invalid slot_start in slot %d", i+1)})
			}
			end, err := parseISO8601(s.SlotEnd)
			if err != nil {
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": fmt.Sprintf("invalid slot_end in slot %d", i+1)})
			}
			if !end.After(start) {
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": fmt.Sprintf("slot_end must be after slot_start in slot %d", i+1)})
			}
			toInsert = append(toInsert, struct{ start, end string }{s.SlotStart, s.SlotEnd})
		}
	} else {
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
		toInsert = append(toInsert, struct{ start, end string }{req.SlotStart, req.SlotEnd})
	}

	var participantID *uint
	if req.Email != "" {
		participant, err := h.participantService.FindOrCreateParticipant(eventID, req.Email)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
		}
		participantID = &participant.ID
		// Replace-on-submit: remove existing slots for this participant for this event
		h.db.Where("event_id = ? AND participant_id = ?", eventID, participant.ID).Delete(&models.Availability{})
	}

	var created []uint
	for _, s := range toInsert {
		start, _ := parseISO8601(s.start)
		end, _ := parseISO8601(s.end)
		avail := models.Availability{
			EventID:       eventID,
			ParticipantID: participantID,
			SlotStart:     start,
			SlotEnd:       end,
		}
		if err := h.db.Create(&avail).Error; err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
		}
		created = append(created, avail.ID)
	}

	if req.Email != "" {
		_ = h.participantService.MarkRespondedByEmail(eventID, req.Email)
	}

	msg := "availability submitted"
	if req.Email != "" {
		msg = "availability replaced" // we always delete existing then insert when email is present
	}
	out := fiber.Map{"message": msg}
	if len(created) == 1 {
		out["id"] = created[0]
	}
	if len(created) > 1 {
		out["ids"] = created
	}
	return c.Status(fiber.StatusCreated).JSON(out)
}
