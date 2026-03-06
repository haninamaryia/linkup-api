package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"linkup-backend/models"
	"linkup-backend/services"
	"linkup-backend/utils"
)

func mustParseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func setupTestApp(t *testing.T) (*fiber.App, *gorm.DB, *services.AuthService, string) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	db.AutoMigrate(&models.User{}, &models.Event{}, &models.Availability{}, &models.Invitation{}, &models.Participant{})

	jwtSecret := "test-secret"
	authService := services.NewAuthService(db, jwtSecret)
	schedulingService := services.NewSchedulingService(db)
	participantService := services.NewParticipantService(db)
	invitationService := services.NewInvitationService(db)
	emailSender := services.NewStubEmailSender()

	authHandler := NewAuthHandler(authService, jwtSecret)
	eventsHandler := NewEventsHandler(db, jwtSecret, participantService, emailSender, schedulingService)
	availHandler := NewAvailabilityHandler(db, schedulingService)
	invHandler := NewInvitationHandler(invitationService, participantService, db)

	app := fiber.New()
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

	return app, db, authService, jwtSecret
}

func getJWT(t *testing.T, app *fiber.App, authService *services.AuthService) string {
	t.Helper()
	code, err := authService.RequestCode("organizer@test.com", "Organizer")
	if err != nil {
		t.Fatalf("request code: %v", err)
	}
	body, _ := json.Marshal(map[string]string{"email": "organizer@test.com", "code": code})
	req := httptest.NewRequest("POST", "/auth/verify-code", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("verify code: %v", err)
	}
	defer resp.Body.Close()
	var out struct {
		Token string `json:"token"`
	}
	json.NewDecoder(resp.Body).Decode(&out)
	return out.Token
}

func TestAuthFlow(t *testing.T) {
	app, _, authService, _ := setupTestApp(t)

	// Request code
	body, _ := json.Marshal(map[string]string{"email": "user@test.com", "name": "User"})
	req := httptest.NewRequest("POST", "/auth/request-code", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("request code: status=%d body=%s", resp.StatusCode, string(b))
	}

	code, _ := authService.RequestCode("user2@test.com", "User2") // get code for user2
	_ = code // verify uses this below - but RequestCode consumes and stores, so we need to call it once for user2
	code, _ = authService.RequestCode("user2@test.com", "User2")  // second call creates/finds user, new code

	verifyBody, _ := json.Marshal(map[string]string{"email": "user2@test.com", "code": code})
	req2 := httptest.NewRequest("POST", "/auth/verify-code", bytes.NewReader(verifyBody))
	req2.Header.Set("Content-Type", "application/json")
	resp2, _ := app.Test(req2)
	if resp2.StatusCode != 200 {
		b, _ := io.ReadAll(resp2.Body)
		t.Fatalf("verify code: status=%d body=%s", resp2.StatusCode, string(b))
	}
	var out struct {
		Token string `json:"token"`
	}
	json.NewDecoder(resp2.Body).Decode(&out)
	if out.Token == "" {
		t.Fatal("expected token")
	}
}

func TestCreateEventWithTimeFrameAndParticipants(t *testing.T) {
	app, _, authService, _ := setupTestApp(t)
	token := getJWT(t, app, authService)

	body, _ := json.Marshal(map[string]interface{}{
		"title":               "Meeting",
		"duration_minutes":    30,
		"time_frame_start":    "2025-03-01T09:00:00Z",
		"time_frame_end":      "2025-03-01T17:00:00Z",
		"participant_emails":  []string{"p1@test.com", "p2@test.com"},
	})
	req := httptest.NewRequest("POST", "/events", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, _ := app.Test(req)
	if resp.StatusCode != 201 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("create event: status=%d body=%s", resp.StatusCode, string(b))
	}
	var event map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&event)
	if event["share_link"] == "" {
		t.Fatal("expected share_link")
	}
	if event["time_frame_start"] == nil {
		t.Fatal("expected time_frame_start")
	}
}

func TestInvitationFlow(t *testing.T) {
	app, db, authService, _ := setupTestApp(t)
	token := getJWT(t, app, authService)

	// Create event
	createBody, _ := json.Marshal(map[string]interface{}{
		"title":            "Standup",
		"duration_minutes": 30,
		"participant_emails": []string{"p@test.com"},
	})
	req := httptest.NewRequest("POST", "/events", bytes.NewReader(createBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, _ := app.Test(req)
	if resp.StatusCode != 201 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("create: status=%d body=%s", resp.StatusCode, string(b))
	}
	var createOut map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&createOut)
	shareLink := createOut["share_link"].(string)
	tokenStr := shareLink[len("/inv/"):]

	// GET /inv/:token
	req2 := httptest.NewRequest("GET", "/inv/"+tokenStr, nil)
	resp2, _ := app.Test(req2)
	if resp2.StatusCode != 200 {
		b, _ := io.ReadAll(resp2.Body)
		t.Fatalf("get inv: status=%d body=%s", resp2.StatusCode, string(b))
	}
	var invEvent map[string]interface{}
	json.NewDecoder(resp2.Body).Decode(&invEvent)
	if invEvent["title"] != "Standup" {
		t.Fatalf("expected title Standup, got %v", invEvent["title"])
	}

	// POST /inv/:token/availability
	availBody, _ := json.Marshal(map[string]string{
		"email":      "p@test.com",
		"slot_start": "2025-03-01T10:00:00Z",
		"slot_end":   "2025-03-01T11:00:00Z",
	})
	req3 := httptest.NewRequest("POST", "/inv/"+tokenStr+"/availability", bytes.NewReader(availBody))
	req3.Header.Set("Content-Type", "application/json")
	resp3, _ := app.Test(req3)
	if resp3.StatusCode != 201 {
		b, _ := io.ReadAll(resp3.Body)
		t.Fatalf("submit availability: status=%d body=%s", resp3.StatusCode, string(b))
	}

	// Participant marked responded
	eventID := int(createOut["id"].(float64))
	var p models.Participant
	if err := db.Where("event_id = ? AND email = ?", eventID, "p@test.com").First(&p).Error; err != nil {
		t.Fatalf("participant not found: %v", err)
	}
	if p.RespondedAt == nil {
		t.Fatal("expected participant to be marked responded")
	}
}

func TestBestTimes(t *testing.T) {
	app, db, _, jwtSecret := setupTestApp(t)
	var creator models.User
	db.Create(&creator)
	tok, _ := utils.GenerateToken(creator.ID, "c@test.com", jwtSecret)

	var event models.Event
	event.CreatorID = creator.ID
	event.Title = "Meet"
	event.DurationMinutes = 30
	db.Create(&event)

	db.Create(&models.Availability{
		EventID:   event.ID,
		SlotStart: mustParseTime("2025-03-01T10:00:00Z"),
		SlotEnd:   mustParseTime("2025-03-01T12:00:00Z"),
	})
	db.Create(&models.Availability{
		EventID:   event.ID,
		SlotStart: mustParseTime("2025-03-01T10:30:00Z"),
		SlotEnd:   mustParseTime("2025-03-01T11:30:00Z"),
	})

	req := httptest.NewRequest("GET", "/events/"+fmt.Sprintf("%d", event.ID)+"/best-times", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, _ := app.Test(req)
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("best-times: status=%d body=%s", resp.StatusCode, string(b))
	}
	var out map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&out)
	bestTimes := out["best_times"].([]interface{})
	if len(bestTimes) == 0 {
		t.Fatal("expected at least one best time")
	}
}

func TestListUpdateDeleteEvent(t *testing.T) {
	app, _, authService, _ := setupTestApp(t)
	token := getJWT(t, app, authService)

	// Create
	createBody, _ := json.Marshal(map[string]interface{}{"title": "Event", "duration_minutes": 30})
	req := httptest.NewRequest("POST", "/events", bytes.NewReader(createBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, _ := app.Test(req)
	if resp.StatusCode != 201 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("create: status=%d body=%s", resp.StatusCode, string(b))
	}
	var createOut map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&createOut)
	eventID := int(createOut["id"].(float64))

	// List
	req2 := httptest.NewRequest("GET", "/events", nil)
	req2.Header.Set("Authorization", "Bearer "+token)
	resp2, _ := app.Test(req2)
	if resp2.StatusCode != 200 {
		t.Fatalf("list: status=%d", resp2.StatusCode)
	}
	var list []map[string]interface{}
	json.NewDecoder(resp2.Body).Decode(&list)
	if len(list) == 0 {
		t.Fatal("expected at least one event")
	}

	// Update
	patchBody, _ := json.Marshal(map[string]string{"title": "Updated Title"})
	req3 := httptest.NewRequest("PATCH", fmt.Sprintf("/events/%d", eventID), bytes.NewReader(patchBody))
	req3.Header.Set("Content-Type", "application/json")
	req3.Header.Set("Authorization", "Bearer "+token)
	resp3, _ := app.Test(req3)
	if resp3.StatusCode != 200 {
		b, _ := io.ReadAll(resp3.Body)
		t.Fatalf("update: status=%d body=%s", resp3.StatusCode, string(b))
	}

	// Delete
	req4 := httptest.NewRequest("DELETE", fmt.Sprintf("/events/%d", eventID), nil)
	req4.Header.Set("Authorization", "Bearer "+token)
	resp4, _ := app.Test(req4)
	if resp4.StatusCode != 204 {
		t.Fatalf("delete: status=%d", resp4.StatusCode)
	}

}

func TestParticipantStatus(t *testing.T) {
	app, _, authService, _ := setupTestApp(t)
	token := getJWT(t, app, authService)

	createBody, _ := json.Marshal(map[string]interface{}{
		"title":              "Event",
		"duration_minutes":   30,
		"participant_emails": []string{"a@test.com", "b@test.com"},
	})
	req := httptest.NewRequest("POST", "/events", bytes.NewReader(createBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, _ := app.Test(req)
	if resp.StatusCode != 201 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("create: status=%d body=%s", resp.StatusCode, string(b))
	}
	var createOut map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&createOut)
	eventID := int(createOut["id"].(float64))

	req2 := httptest.NewRequest("GET", fmt.Sprintf("/events/%d/participant-status", eventID), nil)
	req2.Header.Set("Authorization", "Bearer "+token)
	resp2, _ := app.Test(req2)
	if resp2.StatusCode != 200 {
		b, _ := io.ReadAll(resp2.Body)
		t.Fatalf("participant-status: status=%d body=%s", resp2.StatusCode, string(b))
	}
	var out map[string]interface{}
	json.NewDecoder(resp2.Body).Decode(&out)
	if out["total_count"].(float64) != 2 {
		t.Fatalf("expected 2 participants, got %v", out["total_count"])
	}
	participants := out["participants"].([]interface{})
	if len(participants) != 2 {
		t.Fatalf("expected 2 participants in list, got %d", len(participants))
	}
	for i, p := range participants {
		pm := p.(map[string]interface{})
		if _, ok := pm["slots"]; !ok {
			t.Fatalf("participant %d missing slots key", i)
		}
	}
}

func TestParticipantStatusWithSlots(t *testing.T) {
	app, _, authService, _ := setupTestApp(t)
	token := getJWT(t, app, authService)

	createBody, _ := json.Marshal(map[string]interface{}{
		"title":              "Event",
		"duration_minutes":   30,
		"participant_emails": []string{"p@test.com"},
	})
	req := httptest.NewRequest("POST", "/events", bytes.NewReader(createBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, _ := app.Test(req)
	if resp.StatusCode != 201 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("create: status=%d body=%s", resp.StatusCode, string(b))
	}
	var createOut map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&createOut)
	shareLink := createOut["share_link"].(string)
	tokenStr := shareLink[len("/inv/"):]
	eventID := int(createOut["id"].(float64))

	availBody, _ := json.Marshal(map[string]string{
		"email":      "p@test.com",
		"slot_start": "2025-03-01T10:00:00Z",
		"slot_end":   "2025-03-01T11:00:00Z",
	})
	req2 := httptest.NewRequest("POST", "/inv/"+tokenStr+"/availability", bytes.NewReader(availBody))
	req2.Header.Set("Content-Type", "application/json")
	resp2, _ := app.Test(req2)
	if resp2.StatusCode != 201 {
		b, _ := io.ReadAll(resp2.Body)
		t.Fatalf("submit availability: status=%d body=%s", resp2.StatusCode, string(b))
	}

	req3 := httptest.NewRequest("GET", fmt.Sprintf("/events/%d/participant-status", eventID), nil)
	req3.Header.Set("Authorization", "Bearer "+token)
	resp3, _ := app.Test(req3)
	if resp3.StatusCode != 200 {
		b, _ := io.ReadAll(resp3.Body)
		t.Fatalf("participant-status: status=%d body=%s", resp3.StatusCode, string(b))
	}
	var out map[string]interface{}
	json.NewDecoder(resp3.Body).Decode(&out)
	participants := out["participants"].([]interface{})
	if len(participants) != 1 {
		t.Fatalf("expected 1 participant, got %d", len(participants))
	}
	p := participants[0].(map[string]interface{})
	if p["email"] != "p@test.com" {
		t.Fatalf("expected email p@test.com, got %v", p["email"])
	}
	slots := p["slots"].([]interface{})
	if len(slots) != 1 {
		t.Fatalf("expected 1 slot for participant, got %d", len(slots))
	}
	slot := slots[0].(map[string]interface{})
	if slot["slot_start"].(string) != "2025-03-01T10:00:00Z" || slot["slot_end"].(string) != "2025-03-01T11:00:00Z" {
		t.Fatalf("unexpected slot: %v", slot)
	}
}

func TestEventSummary(t *testing.T) {
	app, _, authService, _ := setupTestApp(t)
	token := getJWT(t, app, authService)

	createBody, _ := json.Marshal(map[string]interface{}{
		"title":              "Summary Event",
		"duration_minutes":   30,
		"participant_emails": []string{"a@test.com", "b@test.com"},
	})
	req := httptest.NewRequest("POST", "/events", bytes.NewReader(createBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, _ := app.Test(req)
	if resp.StatusCode != 201 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("create: status=%d body=%s", resp.StatusCode, string(b))
	}
	var createOut map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&createOut)
	eventID := int(createOut["id"].(float64))

	req2 := httptest.NewRequest("GET", fmt.Sprintf("/events/%d/summary", eventID), nil)
	req2.Header.Set("Authorization", "Bearer "+token)
	resp2, _ := app.Test(req2)
	if resp2.StatusCode != 200 {
		b, _ := io.ReadAll(resp2.Body)
		t.Fatalf("summary: status=%d body=%s", resp2.StatusCode, string(b))
	}
	var out map[string]interface{}
	json.NewDecoder(resp2.Body).Decode(&out)
	if _, ok := out["event"]; !ok {
		t.Fatalf("summary missing event")
	}
	if _, ok := out["participants"]; !ok {
		t.Fatalf("summary missing participants")
	}
	if _, ok := out["best_times"]; !ok {
		t.Fatalf("summary missing best_times")
	}
	if out["total_count"].(float64) != 2 {
		t.Fatalf("expected total_count 2, got %v", out["total_count"])
	}
	participants := out["participants"].([]interface{})
	if len(participants) != 2 {
		t.Fatalf("expected 2 participants, got %d", len(participants))
	}
}
