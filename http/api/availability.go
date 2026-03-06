// AvailabilityHandler: submit availability, get best time(s).
// POST /events/:id/availability (can be used with or without auth).
// GET /events/:id/best-time (first overlapping slot), GET /events/:id/best-times (all slots).
package api

import (
	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"

	"linkup-backend/models"
	"linkup-backend/services"
)

type AvailabilityHandler struct {
	db                *gorm.DB
	schedulingService *services.SchedulingService
}

func NewAvailabilityHandler(db *gorm.DB, schedulingService *services.SchedulingService) *AvailabilityHandler {
	return &AvailabilityHandler{db: db, schedulingService: schedulingService}
}

type SubmitAvailabilityRequest struct {
	UserID    *uint  `json:"user_id"`
	SlotStart string `json:"slot_start"` // ISO8601
	SlotEnd   string `json:"slot_end"`   // ISO8601
}

type BestTimeResponse struct {
	SlotStart      string `json:"slot_start"`
	SlotEnd        string `json:"slot_end"`
	AvailableCount int    `json:"available_count"`
	Total          int    `json:"total"`
}

// SubmitAvailability: parse slot_start/slot_end (ISO8601), create Availability row.
func (h *AvailabilityHandler) SubmitAvailability(c *fiber.Ctx) error {
	eventID, err := c.ParamsInt("id")
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid event id"})
	}

	var req SubmitAvailabilityRequest
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

	// Verify event exists
	var event models.Event
	if err := h.db.First(&event, eventID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "event not found"})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	avail := models.Availability{
		EventID:   uint(eventID),
		UserID:    req.UserID,
		SlotStart: slotStart,
		SlotEnd:   slotEnd,
	}
	if err := h.db.Create(&avail).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"id":         avail.ID,
		"event_id":   avail.EventID,
		"slot_start": avail.SlotStart.Format(timeFormatISO8601),
		"slot_end":   avail.SlotEnd.Format(timeFormatISO8601),
	})
}

// ISO8601 format for JSON date-times (RFC3339).
const timeFormatISO8601 = "2006-01-02T15:04:05Z07:00"

// formatBestTimeResult converts a scheduling result slot to the API response shape. Used by GetBestTimes, GetBestTime, and GetEventSummary.
func formatBestTimeResult(r services.BestTimeResult) BestTimeResponse {
	return BestTimeResponse{
		SlotStart:      r.SlotStart.Format(timeFormatISO8601),
		SlotEnd:        r.SlotEnd.Format(timeFormatISO8601),
		AvailableCount: r.AvailableCount,
		Total:          r.Total,
	}
}

// GetBestTimes: ranked list of candidate slots (30-min step), each with available_count/total.
func (h *AvailabilityHandler) GetBestTimes(c *fiber.Ctx) error {
	eventID, err := c.ParamsInt("id")
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid event id"})
	}

	resp, err := h.schedulingService.GetBestTimesResponse(uint(eventID), 0)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	if len(resp.Slots) == 0 {
		out := fiber.Map{"best_times": []interface{}{}, "message": "no overlapping availability found"}
		if resp.Note != "" {
			out["note"] = resp.Note
			out["excluded_participant_ids"] = resp.ExcludedParticipantIDs
		}
		return c.JSON(out)
	}

	slots := make([]BestTimeResponse, len(resp.Slots))
	for i := range resp.Slots {
		slots[i] = formatBestTimeResult(resp.Slots[i])
	}
	out := fiber.Map{"best_times": slots}
	if resp.Note != "" {
		out["note"] = resp.Note
		out["excluded_participant_ids"] = resp.ExcludedParticipantIDs
	}
	return c.JSON(out)
}

// GetBestTime: top-ranked slot plus available_count/total.
func (h *AvailabilityHandler) GetBestTime(c *fiber.Ctx) error {
	eventID, err := c.ParamsInt("id")
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid event id"})
	}

	resp, err := h.schedulingService.GetBestTimesResponse(uint(eventID), 0)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	if len(resp.Slots) == 0 {
		return c.JSON(fiber.Map{"message": "no overlapping availability found"})
	}

	r := resp.Slots[0]
	formatted := formatBestTimeResult(r)
	out := fiber.Map{
		"slot_start":      formatted.SlotStart,
		"slot_end":        formatted.SlotEnd,
		"available_count": formatted.AvailableCount,
		"total":           formatted.Total,
	}
	if resp.Note != "" {
		out["note"] = resp.Note
		out["excluded_participant_ids"] = resp.ExcludedParticipantIDs
	}
	return c.JSON(out)
}
