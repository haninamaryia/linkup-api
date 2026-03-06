package api

import (
	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

// HealthHandler serves liveness and readiness endpoints (contract for Docker/K8s).
type HealthHandler struct {
	db *gorm.DB
}

// NewHealthHandler returns a handler that uses db for readiness checks.
func NewHealthHandler(db *gorm.DB) *HealthHandler {
	return &HealthHandler{db: db}
}

// Health returns 200 and {"status":"ok"}. No dependencies checked (liveness).
func (h *HealthHandler) Health(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{"status": "ok"})
}

// Ready returns 200 if the database is reachable, 503 otherwise (readiness).
func (h *HealthHandler) Ready(c *fiber.Ctx) error {
	sqlDB, err := h.db.DB()
	if err != nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"status": "unavailable", "error": "db connection"})
	}
	if err := sqlDB.Ping(); err != nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"status": "unavailable", "error": err.Error()})
	}
	return c.JSON(fiber.Map{"status": "ok"})
}
