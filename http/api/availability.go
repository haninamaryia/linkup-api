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
	SlotStart string `json:"slot_start"`
	SlotEnd   string `json:"slot_end"`
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
		"slot_start": avail.SlotStart.Format("2006-01-02T15:04:05Z07:00"),
		"slot_end":   avail.SlotEnd.Format("2006-01-02T15:04:05Z07:00"),
	})
}

// GetBestTimes: all overlapping slots within event time frame.
func (h *AvailabilityHandler) GetBestTimes(c *fiber.Ctx) error {
	eventID, err := c.ParamsInt("id")
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid event id"})
	}

	results, err := h.schedulingService.GetBestTimes(uint(eventID), 0)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	if len(results) == 0 {
		return c.JSON(fiber.Map{"best_times": []interface{}{}, "message": "no overlapping availability found"})
	}

	slots := make([]BestTimeResponse, len(results))
	for i, r := range results {
		slots[i] = BestTimeResponse{
			SlotStart: r.SlotStart.Format("2006-01-02T15:04:05Z07:00"),
			SlotEnd:   r.SlotEnd.Format("2006-01-02T15:04:05Z07:00"),
		}
	}
	return c.JSON(fiber.Map{"best_times": slots})
}

// GetBestTime: first overlapping slot (backwards compat).
func (h *AvailabilityHandler) GetBestTime(c *fiber.Ctx) error {
	eventID, err := c.ParamsInt("id")
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid event id"})
	}

	result, err := h.schedulingService.GetBestTime(uint(eventID), 0)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	if result == nil {
		return c.JSON(fiber.Map{"message": "no overlapping availability found"})
	}

	return c.JSON(BestTimeResponse{
		SlotStart: result.SlotStart.Format("2006-01-02T15:04:05Z07:00"),
		SlotEnd:   result.SlotEnd.Format("2006-01-02T15:04:05Z07:00"),
	})
}
