package api

import (
	"time"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"

	"linkup-backend/services"
)

// SetupRoutes wires all API routes to handlers.
// authCodeRateLimitMax: max code requests per email per window (0 = no limit). authCodeRateLimitWindow: e.g. 15m.
// emailSender: use stub for dev; pass production sender from main when configured.
func SetupRoutes(app *fiber.App, db *gorm.DB, jwtSecret string, authCodeTTL time.Duration, authCodeRateLimitMax int, authCodeRateLimitWindow time.Duration, emailSender services.EmailSender) {
	authService := services.NewAuthService(db, jwtSecret, authCodeTTL)
	if authCodeRateLimitMax > 0 && authCodeRateLimitWindow > 0 {
		authService.SetRateLimiter(services.NewAuthCodeRateLimiter(authCodeRateLimitMax, authCodeRateLimitWindow))
	}
	schedulingService := services.NewSchedulingService(db)
	participantService := services.NewParticipantService(db)
	invitationService := services.NewInvitationService(db)
	if emailSender == nil {
		emailSender = services.NewStubEmailSender()
	}

	authHandler := NewAuthHandler(authService, jwtSecret)
	eventsHandler := NewEventsHandler(db, jwtSecret, participantService, emailSender, schedulingService)
	availHandler := NewAvailabilityHandler(db, schedulingService)
	invHandler := NewInvitationHandler(invitationService, participantService, db)

	app.Post("/auth/request-code", authHandler.RequestCode)
	app.Post("/auth/verify-code", authHandler.VerifyCode)

	app.Post("/events", eventsHandler.CreateEvent)
	app.Get("/events", eventsHandler.ListEvents)
	app.Get("/events/:id", eventsHandler.GetEvent)
	app.Patch("/events/:id", eventsHandler.UpdateEvent)
	app.Delete("/events/:id", eventsHandler.DeleteEvent)
	app.Get("/events/:id/participant-status", eventsHandler.GetParticipantStatus)
	app.Get("/events/:id/summary", eventsHandler.GetEventSummary)

	app.Post("/events/:id/availability", availHandler.SubmitAvailability)
	app.Get("/events/:id/best-time", availHandler.GetBestTime)
	app.Get("/events/:id/best-times", availHandler.GetBestTimes)

	app.Get("/inv/:token", invHandler.GetByToken)
	app.Post("/inv/:token/availability", invHandler.SubmitAvailability)
}
