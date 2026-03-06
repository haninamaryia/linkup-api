package api

import (
	"github.com/gofiber/fiber/v2"
)

// ErrorResponse is the single shape for all API error responses.
type ErrorResponse struct {
	Error string `json:"error"`
}

// respondError sends a JSON error response with status code. All handler errors use this shape.
func respondError(c *fiber.Ctx, status int, message string) error {
	return c.Status(status).JSON(ErrorResponse{Error: message})
}
