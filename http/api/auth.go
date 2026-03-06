// AuthHandler: POST /auth/request-code, POST /auth/verify-code.
// Flow: RequestCode → AuthService stores code in memory, logs to stdout in dev.
//       VerifyCode → AuthService validates, returns userID/email → handler issues JWT.
package api

import (
	"github.com/gofiber/fiber/v2"

	"linkup-backend/services"
	"linkup-backend/utils"
)

type AuthHandler struct {
	authService *services.AuthService
	jwtSecret   string
}

func NewAuthHandler(authService *services.AuthService, jwtSecret string) *AuthHandler {
	return &AuthHandler{authService: authService, jwtSecret: jwtSecret}
}

type RequestCodeRequest struct {
	Email string `json:"email"`
	Name  string `json:"name"`
}

type RequestCodeResponse struct {
	Message string `json:"message"`
}

// RequestCode creates/finds user, generates one-time code. Code is logged in dev (TODO: send email).
func (h *AuthHandler) RequestCode(c *fiber.Ctx) error {
	var req RequestCodeRequest
	if err := c.BodyParser(&req); err != nil {
		return respondError(c, fiber.StatusBadRequest, "invalid request")
	}
	if req.Email == "" {
		return respondError(c, fiber.StatusBadRequest, "email required")
	}

	_, err := h.authService.RequestCode(req.Email, req.Name)
	if err != nil {
		if err == services.ErrRateLimited {
			return respondError(c, fiber.StatusTooManyRequests, "too many code requests; try again later")
		}
		return respondError(c, fiber.StatusInternalServerError, err.Error())
	}

	return c.JSON(RequestCodeResponse{Message: "verification code sent"})
}

type VerifyCodeRequest struct {
	Email string `json:"email"`
	Code  string `json:"code"`
}

type VerifyCodeResponse struct {
	Token string `json:"token"`
}

// VerifyCode validates email+code, returns JWT for use in Authorization: Bearer header.
func (h *AuthHandler) VerifyCode(c *fiber.Ctx) error {
	var req VerifyCodeRequest
	if err := c.BodyParser(&req); err != nil {
		return respondError(c, fiber.StatusBadRequest, "invalid request")
	}
	if req.Email == "" || req.Code == "" {
		return respondError(c, fiber.StatusBadRequest, "email and code required")
	}

	userID, email, err := h.authService.VerifyCode(req.Email, req.Code)
	if err != nil {
		if err == services.ErrInvalidCode {
			return respondError(c, fiber.StatusUnauthorized, "invalid or expired code")
		}
		return respondError(c, fiber.StatusInternalServerError, err.Error())
	}

	token, err := utils.GenerateToken(userID, email, h.jwtSecret)
	if err != nil {
		return respondError(c, fiber.StatusInternalServerError, "failed to generate token")
	}

	return c.JSON(VerifyCodeResponse{Token: token})
}
