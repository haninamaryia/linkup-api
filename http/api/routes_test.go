package api

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
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
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get db: %v", err)
	}
	_ = sql.ErrNoRows // use database/sql so import is used
	sqlDB.SetMaxOpenConns(1) // single connection so :memory: is shared across handlers
	db.AutoMigrate(&models.User{}, &models.AuthCode{}, &models.Event{}, &models.Availability{}, &models.Invitation{}, &models.Participant{})

	jwtSecret := "test-secret"
	authService := services.NewAuthService(db, jwtSecret, 10*time.Minute)
	schedulingService := services.NewSchedulingService(db)
	participantService := services.NewParticipantService(db)
	invitationService := services.NewInvitationService(db)
	emailSender := services.NewStubEmailSender()

	authHandler := NewAuthHandler(authService, jwtSecret)
	eventsHandler := NewEventsHandler(db, jwtSecret, participantService, emailSender, schedulingService)
	availHandler := NewAvailabilityHandler(db, schedulingService)
	invHandler := NewInvitationHandler(invitationService, participantService, db)

	app := fiber.New()
	app.Use(cors.New(cors.Config{
		AllowOrigins: "http://localhost:3000,http://localhost:5173",
		AllowHeaders: "Origin, Content-Type, Accept, Authorization",
	}))
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

// TestAuthRateLimit ensures that exceeding the rate limit returns 429.
func TestAuthRateLimit(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	sqlDB, _ := db.DB()
	_ = sql.ErrNoRows
	sqlDB.SetMaxOpenConns(1)
	db.AutoMigrate(&models.User{}, &models.AuthCode{}, &models.Event{}, &models.Availability{}, &models.Invitation{}, &models.Participant{})

	authService := services.NewAuthService(db, "secret", 10*time.Minute)
	authService.SetRateLimiter(services.NewAuthCodeRateLimiter(2, time.Minute))

	app := fiber.New()
	app.Post("/auth/request-code", NewAuthHandler(authService, "secret").RequestCode)

	email := "ratelimit@test.com"
	body, _ := json.Marshal(map[string]string{"email": email, "name": "R"})
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest("POST", "/auth/request-code", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, _ := app.Test(req)
		if i < 2 && resp.StatusCode != 200 {
			b, _ := io.ReadAll(resp.Body)
			t.Fatalf("request %d: expected 200, got %d %s", i+1, resp.StatusCode, string(b))
		}
		if i == 2 && resp.StatusCode != 429 {
			b, _ := io.ReadAll(resp.Body)
			t.Fatalf("request 3: expected 429 (rate limited), got %d %s", resp.StatusCode, string(b))
		}
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

// TestAvailabilityValidation_slotEndBeforeStart ensures slot_end <= slot_start returns 400 with a clear error.
func TestAvailabilityValidation_slotEndBeforeStart(t *testing.T) {
	app, _, authService, _ := setupTestApp(t)
	token := getJWT(t, app, authService)
	eventID := createEvent(t, app, token, "Meet", 30, nil, nil)

	body, _ := json.Marshal(map[string]string{
		"slot_start": "2025-03-01T11:00:00Z",
		"slot_end":   "2025-03-01T10:00:00Z",
	})
	req := httptest.NewRequest("POST", fmt.Sprintf("/events/%d/availability", eventID), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, _ := app.Test(req)
	if resp.StatusCode != 400 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 400, got %d body=%s", resp.StatusCode, string(b))
	}
	var out map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&out)
	if out["error"].(string) != "slot_end must be after slot_start" {
		t.Fatalf("expected slot_end error, got %v", out["error"])
	}
}

// TestAvailabilityValidation_slotOutsideTimeFrame ensures a slot outside event timeframe returns 400.
func TestAvailabilityValidation_slotOutsideTimeFrame(t *testing.T) {
	app, _, authService, _ := setupTestApp(t)
	token := getJWT(t, app, authService)
	tfStart := "2025-03-01T09:00:00Z"
	tfEnd := "2025-03-01T17:00:00Z"
	eventID := createEvent(t, app, token, "Meet", 30, &tfStart, &tfEnd)

	body, _ := json.Marshal(map[string]string{
		"slot_start": "2025-03-01T08:00:00Z",
		"slot_end":   "2025-03-01T09:30:00Z",
	})
	req := httptest.NewRequest("POST", fmt.Sprintf("/events/%d/availability", eventID), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, _ := app.Test(req)
	if resp.StatusCode != 400 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 400 for slot before timeframe, got %d body=%s", resp.StatusCode, string(b))
	}
	var out map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&out)
	errMsg := out["error"].(string)
	if errMsg == "" || len(errMsg) < 10 {
		t.Fatalf("expected helpful error message, got %q", errMsg)
	}
}

// TestCreateEvent_reversedTimeFrame ensures time_frame_end before time_frame_start returns 400.
func TestCreateEvent_reversedTimeFrame(t *testing.T) {
	app, _, authService, _ := setupTestApp(t)
	token := getJWT(t, app, authService)

	body, _ := json.Marshal(map[string]interface{}{
		"title":            "Meet",
		"duration_minutes": 30,
		"time_frame_start": "2025-03-01T17:00:00Z",
		"time_frame_end":    "2025-03-01T09:00:00Z",
	})
	req := httptest.NewRequest("POST", "/events", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, _ := app.Test(req)
	if resp.StatusCode != 400 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 400 for reversed time frame, got %d body=%s", resp.StatusCode, string(b))
	}
	var out map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&out)
	if out["error"].(string) != "time_frame_end must be after time_frame_start" {
		t.Fatalf("unexpected error: %v", out["error"])
	}
}

// TestAvailabilityValidation_slotShorterThanDuration ensures slot shorter than event duration returns 400.
func TestAvailabilityValidation_slotShorterThanDuration(t *testing.T) {
	app, _, authService, _ := setupTestApp(t)
	token := getJWT(t, app, authService)
	eventID := createEvent(t, app, token, "Meet", 60, nil, nil) // 60 min duration

	body, _ := json.Marshal(map[string]string{
		"slot_start": "2025-03-01T10:00:00Z",
		"slot_end":   "2025-03-01T10:30:00Z", // 30 min slot
	})
	req := httptest.NewRequest("POST", fmt.Sprintf("/events/%d/availability", eventID), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, _ := app.Test(req)
	if resp.StatusCode != 400 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 400 for slot shorter than duration, got %d body=%s", resp.StatusCode, string(b))
	}
	var out map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&out)
	errMsg := out["error"].(string)
	if errMsg == "" || len(errMsg) < 5 {
		t.Fatalf("expected error about slot length, got %q", errMsg)
	}
}

// TestInvitation_overlappingSlotsRejected ensures overlapping slots in one request return 400.
func TestInvitation_overlappingSlotsRejected(t *testing.T) {
	app, db, authService, _ := setupTestApp(t)
	token := getJWT(t, app, authService)
	createBody, _ := json.Marshal(map[string]interface{}{
		"title":              "Meet",
		"duration_minutes":   30,
		"participant_emails": []string{"p@test.com"},
	})
	req := httptest.NewRequest("POST", "/events", bytes.NewReader(createBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, _ := app.Test(req)
	if resp.StatusCode != 201 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("create event: %d %s", resp.StatusCode, string(b))
	}
	var createOut map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&createOut)
	shareLink := createOut["share_link"].(string)
	invToken := shareLink[len("/inv/"):]

	// Submit two overlapping slots (10:00-11:00 and 10:30-11:30)
	availBody, _ := json.Marshal(map[string]interface{}{
		"email": "p@test.com",
		"slots": []map[string]string{
			{"slot_start": "2025-03-01T10:00:00Z", "slot_end": "2025-03-01T11:00:00Z"},
			{"slot_start": "2025-03-01T10:30:00Z", "slot_end": "2025-03-01T11:30:00Z"},
		},
	})
	req2 := httptest.NewRequest("POST", "/inv/"+invToken+"/availability", bytes.NewReader(availBody))
	req2.Header.Set("Content-Type", "application/json")
	resp2, _ := app.Test(req2)
	if resp2.StatusCode != 400 {
		b, _ := io.ReadAll(resp2.Body)
		t.Fatalf("expected 400 for overlapping slots, got %d body=%s", resp2.StatusCode, string(b))
	}
	var out map[string]interface{}
	json.NewDecoder(resp2.Body).Decode(&out)
	errMsg := out["error"].(string)
	if errMsg == "" || len(errMsg) < 5 {
		t.Fatalf("expected overlap error, got %q", errMsg)
	}
	// Ensure no availability was created
	var count int64
	db.Model(&models.Availability{}).Where("event_id = ?", int(createOut["id"].(float64))).Count(&count)
	if count != 0 {
		t.Fatalf("expected 0 availability rows, got %d", count)
	}
}

// TestUpdateEvent_participantRemovedAvailabilityDeleted ensures removing a participant deletes their availability.
func TestUpdateEvent_participantRemovedAvailabilityDeleted(t *testing.T) {
	app, db, authService, _ := setupTestApp(t)
	token := getJWT(t, app, authService)
	eventID := createEvent(t, app, token, "Meet", 30, nil, nil)

	// Get invitation token and submit as participant
	var inv models.Invitation
	db.Where("event_id = ?", eventID).First(&inv)
	availBody, _ := json.Marshal(map[string]string{
		"email":      "gone@test.com",
		"slot_start": "2025-03-01T10:00:00Z",
		"slot_end":   "2025-03-01T11:00:00Z",
	})
	req := httptest.NewRequest("POST", "/inv/"+inv.Token+"/availability", bytes.NewReader(availBody))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	if resp.StatusCode != 201 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("submit availability: %d %s", resp.StatusCode, string(b))
	}
	var p models.Participant
	db.Where("event_id = ? AND email = ?", eventID, "gone@test.com").First(&p)
	var availCount int64
	db.Model(&models.Availability{}).Where("event_id = ? AND participant_id = ?", eventID, p.ID).Count(&availCount)
	if availCount != 1 {
		t.Fatalf("expected 1 availability before update, got %d", availCount)
	}

	// Update event to remove gone@test.com (only keep other@test.com)
	patchBody, _ := json.Marshal(map[string]interface{}{
		"participant_emails": []string{"other@test.com"},
	})
	req2 := httptest.NewRequest("PATCH", fmt.Sprintf("/events/%d", eventID), bytes.NewReader(patchBody))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("Authorization", "Bearer "+token)
	resp2, _ := app.Test(req2)
	if resp2.StatusCode != 200 {
		b, _ := io.ReadAll(resp2.Body)
		t.Fatalf("update event: %d %s", resp2.StatusCode, string(b))
	}

	// Removed participant's availability should be deleted
	db.Model(&models.Availability{}).Where("event_id = ? AND participant_id = ?", eventID, p.ID).Count(&availCount)
	if availCount != 0 {
		t.Fatalf("expected 0 availability for removed participant, got %d", availCount)
	}
}

// TestUpdateEvent_duplicateEmailsDeduped ensures duplicate emails in participant_emails result in one row per email.
func TestUpdateEvent_duplicateEmailsDeduped(t *testing.T) {
	app, db, authService, _ := setupTestApp(t)
	token := getJWT(t, app, authService)
	eventID := createEvent(t, app, token, "Meet", 30, nil, nil)

	patchBody, _ := json.Marshal(map[string]interface{}{
		"participant_emails": []string{"a@test.com", "a@test.com", "b@test.com"},
	})
	req := httptest.NewRequest("PATCH", fmt.Sprintf("/events/%d", eventID), bytes.NewReader(patchBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, _ := app.Test(req)
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("update: %d %s", resp.StatusCode, string(b))
	}
	var count int64
	db.Model(&models.Participant{}).Where("event_id = ?", eventID).Count(&count)
	if count != 2 {
		t.Fatalf("expected 2 participants (deduped), got %d", count)
	}
}

// TestAuthCode_replayReturns401 ensures using the same code twice returns 401 on second verify.
func TestAuthCode_replayReturns401(t *testing.T) {
	app, _, authService, _ := setupTestApp(t)

	code, err := authService.RequestCode("replay@test.com", "Replay")
	if err != nil {
		t.Fatalf("request code: %v", err)
	}
	body, _ := json.Marshal(map[string]string{"email": "replay@test.com", "code": code})

	// First verify succeeds
	req := httptest.NewRequest("POST", "/auth/verify-code", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("first verify: expected 200, got %d %s", resp.StatusCode, string(b))
	}

	// Second verify with same code must fail (replay)
	req2 := httptest.NewRequest("POST", "/auth/verify-code", bytes.NewReader(body))
	req2.Header.Set("Content-Type", "application/json")
	resp2, _ := app.Test(req2)
	if resp2.StatusCode != 401 {
		b, _ := io.ReadAll(resp2.Body)
		t.Fatalf("replay verify: expected 401, got %d %s", resp2.StatusCode, string(b))
	}
}

// TestInvitation_expiredTokenReturns410 ensures expired invite token returns 410.
func TestInvitation_expiredTokenReturns410(t *testing.T) {
	app, db, authService, _ := setupTestApp(t)
	token := getJWT(t, app, authService)
	eventID := createEvent(t, app, token, "Meet", 30, nil, nil)

	var inv models.Invitation
	db.Where("event_id = ?", eventID).First(&inv)
	past := time.Now().Add(-time.Hour)
	db.Model(&inv).Update("expires_at", past)

	req := httptest.NewRequest("GET", "/inv/"+inv.Token, nil)
	resp, _ := app.Test(req)
	if resp.StatusCode != 410 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 410 Gone for expired token, got %d body=%s", resp.StatusCode, string(b))
	}
}

// TestDeleteEvent_cascades ensures deleting an event removes availabilities and participants.
func TestDeleteEvent_cascades(t *testing.T) {
	app, db, authService, _ := setupTestApp(t)
	token := getJWT(t, app, authService)
	eventID := createEvent(t, app, token, "Meet", 30, nil, nil)

	var inv models.Invitation
	db.Where("event_id = ?", eventID).First(&inv)
	availBody, _ := json.Marshal(map[string]string{
		"email":      "p@test.com",
		"slot_start": "2025-03-01T10:00:00Z",
		"slot_end":   "2025-03-01T11:00:00Z",
	})
	req := httptest.NewRequest("POST", "/inv/"+inv.Token+"/availability", bytes.NewReader(availBody))
	req.Header.Set("Content-Type", "application/json")
	app.Test(req)

	// Delete event
	delReq := httptest.NewRequest("DELETE", fmt.Sprintf("/events/%d", eventID), nil)
	delReq.Header.Set("Authorization", "Bearer "+token)
	delResp, _ := app.Test(delReq)
	if delResp.StatusCode != 204 {
		t.Fatalf("delete: expected 204, got %d", delResp.StatusCode)
	}

	// Event and related data should be gone
	var event models.Event
	if err := db.First(&event, eventID).Error; err == nil {
		t.Fatal("event should be deleted")
	}
	var availCount int64
	db.Model(&models.Availability{}).Where("event_id = ?", eventID).Count(&availCount)
	if availCount != 0 {
		t.Fatalf("availabilities should be cascaded, got %d", availCount)
	}
	var partCount int64
	db.Model(&models.Participant{}).Where("event_id = ?", eventID).Count(&partCount)
	if partCount != 0 {
		t.Fatalf("participants should be cascaded, got %d", partCount)
	}
}

// TestCORS ensures an allowed Origin gets Access-Control-Allow-Origin in the response.
func TestCORS(t *testing.T) {
	app, _, _, _ := setupTestApp(t)

	req := httptest.NewRequest("GET", "/events", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	req.Header.Set("Authorization", "Bearer fake-token-will-401")
	resp, _ := app.Test(req)
	allowOrigin := resp.Header.Get("Access-Control-Allow-Origin")
	if allowOrigin != "http://localhost:3000" {
		t.Fatalf("expected Access-Control-Allow-Origin: http://localhost:3000, got %q", allowOrigin)
	}
}

// createEvent creates an event and returns its ID (helper for tests).
func createEvent(t *testing.T, app *fiber.App, token, title string, duration int, timeFrameStart, timeFrameEnd *string) int {
	t.Helper()
	body := map[string]interface{}{
		"title":             title,
		"duration_minutes":  duration,
	}
	if timeFrameStart != nil {
		body["time_frame_start"] = *timeFrameStart
	}
	if timeFrameEnd != nil {
		body["time_frame_end"] = *timeFrameEnd
	}
	reqBody, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/events", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, _ := app.Test(req)
	if resp.StatusCode != 201 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("create event: status=%d body=%s", resp.StatusCode, string(b))
	}
	var out map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&out)
	return int(out["id"].(float64))
}
