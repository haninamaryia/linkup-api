// Package handlers contains HTTP handlers for the Linkup API.
package handlers

import (
	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"

	"linkup-backend/internal/services"
)

// SetupRoutes wires all API routes to handlers.
// Creates services (auth, scheduling, participants, invitations, email stub), then registers:
// - Auth: request-code, verify-code
// - Events: CRUD, participant-status
// - Availability: submit, best-time, best-times
// - Invitations: GET by token, POST availability (participant flow, no auth)
func SetupRoutes(app *fiber.App, db *gorm.DB, jwtSecret string) {
	authService := services.NewAuthService(db, jwtSecret)
	schedulingService := services.NewSchedulingService(db)
	participantService := services.NewParticipantService(db)
	invitationService := services.NewInvitationService(db)
	// TODO: Swap for real email provider (Resend, SendGrid) when ready
	emailSender := services.NewStubEmailSender()

	authHandler := NewAuthHandler(authService, jwtSecret)
	eventsHandler := NewEventsHandler(db, jwtSecret, participantService, emailSender)
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

	app.Post("/events/:id/availability", availHandler.SubmitAvailability)
	app.Get("/events/:id/best-time", availHandler.GetBestTime)
	app.Get("/events/:id/best-times", availHandler.GetBestTimes)

	app.Get("/inv/:token", invHandler.GetByToken)
	app.Post("/inv/:token/availability", invHandler.SubmitAvailability)
}
