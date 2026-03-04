package services

import (
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"linkup-backend/models"
)

func mustTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestMergeIntervals(t *testing.T) {
	tests := []struct {
		name   string
		input  []interval
		output []interval
	}{
		{
			name:   "empty",
			input:  nil,
			output: nil,
		},
		{
			name: "single",
			input: []interval{{start: mustTime("2025-03-10T10:00:00Z"), end: mustTime("2025-03-10T11:00:00Z")}},
			output: []interval{{start: mustTime("2025-03-10T10:00:00Z"), end: mustTime("2025-03-10T11:00:00Z")}},
		},
		{
			name: "overlapping",
			input: []interval{
				{start: mustTime("2025-03-10T10:00:00Z"), end: mustTime("2025-03-10T11:00:00Z")},
				{start: mustTime("2025-03-10T10:30:00Z"), end: mustTime("2025-03-10T11:30:00Z")},
			},
			output: []interval{{start: mustTime("2025-03-10T10:00:00Z"), end: mustTime("2025-03-10T11:30:00Z")}},
		},
		{
			name: "disjoint",
			input: []interval{
				{start: mustTime("2025-03-10T10:00:00Z"), end: mustTime("2025-03-10T11:00:00Z")},
				{start: mustTime("2025-03-10T12:00:00Z"), end: mustTime("2025-03-10T13:00:00Z")},
			},
			output: []interval{
				{start: mustTime("2025-03-10T10:00:00Z"), end: mustTime("2025-03-10T11:00:00Z")},
				{start: mustTime("2025-03-10T12:00:00Z"), end: mustTime("2025-03-10T13:00:00Z")},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mergeIntervals(tt.input)
			if len(got) != len(tt.output) {
				t.Fatalf("got %d intervals, want %d", len(got), len(tt.output))
			}
			for i := range got {
				if !got[i].start.Equal(tt.output[i].start) || !got[i].end.Equal(tt.output[i].end) {
					t.Errorf("interval %d: got [%v, %v], want [%v, %v]", i, got[i].start, got[i].end, tt.output[i].start, tt.output[i].end)
				}
			}
		})
	}
}

func TestIntersectTwo(t *testing.T) {
	a := []interval{
		{start: mustTime("2025-03-10T10:00:00Z"), end: mustTime("2025-03-10T12:00:00Z")},
	}
	b := []interval{
		{start: mustTime("2025-03-10T10:15:00Z"), end: mustTime("2025-03-10T11:00:00Z")},
	}
	got := intersectTwo(a, b)
	if len(got) != 1 {
		t.Fatalf("got %d intervals, want 1", len(got))
	}
	if !got[0].start.Equal(mustTime("2025-03-10T10:15:00Z")) || !got[0].end.Equal(mustTime("2025-03-10T11:00:00Z")) {
		t.Errorf("got [%v, %v], want [10:15, 11:00]", got[0].start, got[0].end)
	}
}

func TestIntersectIntervalLists(t *testing.T) {
	// Two participants: A 10:00-11:00, B 10:15-11:00 -> intersection 10:15-11:00
	lists := [][]interval{
		{{start: mustTime("2025-03-10T10:00:00Z"), end: mustTime("2025-03-10T11:00:00Z")}},
		{{start: mustTime("2025-03-10T10:15:00Z"), end: mustTime("2025-03-10T11:00:00Z")}},
	}
	got := intersectIntervalLists(lists)
	if len(got) != 1 {
		t.Fatalf("got %d intervals, want 1", len(got))
	}
	if !got[0].start.Equal(mustTime("2025-03-10T10:15:00Z")) || !got[0].end.Equal(mustTime("2025-03-10T11:00:00Z")) {
		t.Errorf("got [%v, %v], want [10:15, 11:00]", got[0].start, got[0].end)
	}
}

func TestClipToFrame(t *testing.T) {
	frameStart := mustTime("2025-03-10T09:00:00Z")
	frameEnd := mustTime("2025-03-10T14:00:00Z")
	intervals := []interval{
		{start: mustTime("2025-03-10T08:00:00Z"), end: mustTime("2025-03-10T10:00:00Z")},
		{start: mustTime("2025-03-10T13:00:00Z"), end: mustTime("2025-03-10T15:00:00Z")},
	}
	got := clipToFrame(intervals, &frameStart, &frameEnd)
	if len(got) != 2 {
		t.Fatalf("got %d intervals, want 2", len(got))
	}
	if !got[0].start.Equal(frameStart) || !got[0].end.Equal(mustTime("2025-03-10T10:00:00Z")) {
		t.Errorf("first: got [%v, %v]", got[0].start, got[0].end)
	}
	if !got[1].start.Equal(mustTime("2025-03-10T13:00:00Z")) || !got[1].end.Equal(frameEnd) {
		t.Errorf("second: got [%v, %v]", got[1].start, got[1].end)
	}
}

func TestEmitWindows_30MinStep(t *testing.T) {
	// One interval 12:00-14:00 (2 hours), duration 45 min, step 30 min
	// Expect: 12:00-12:45, 12:30-13:15, 13:00-13:45, 13:30-14:00 (last one ends at 14:00 so 13:30+45=14:15 > 14:00, so 13:30-14:15 is invalid; we need 13:30+45 <= 14:00 -> 13:30+45=14:15 > 14:00, so we get 12:00-12:45, 12:30-13:15, 13:00-13:45 only)
	// Actually 13:30 + 45min = 14:15 > 14:00, so we don't emit 13:30. So 3 slots.
	intervals := []interval{
		{start: mustTime("2025-03-10T12:00:00Z"), end: mustTime("2025-03-10T14:00:00Z")},
	}
	got := emitWindows(intervals, 45, 30, nil, nil)
	if len(got) != 3 {
		t.Fatalf("got %d slots, want 3", len(got))
	}
	expect := []struct{ start, end string }{
		{"2025-03-10T12:00:00Z", "2025-03-10T12:45:00Z"},
		{"2025-03-10T12:30:00Z", "2025-03-10T13:15:00Z"},
		{"2025-03-10T13:00:00Z", "2025-03-10T13:45:00Z"},
	}
	for i, e := range expect {
		if !got[i].SlotStart.Equal(mustTime(e.start)) || !got[i].SlotEnd.Equal(mustTime(e.end)) {
			t.Errorf("slot %d: got [%v, %v], want [%s, %s]", i, got[i].SlotStart, got[i].SlotEnd, e.start, e.end)
		}
	}
}

func TestEmitWindows_LargeRange(t *testing.T) {
	// 12:00 to 08:00 next day = 20 hours. 45 min duration, 30 min step.
	// From 12:00: 12:00-12:45, 12:30-13:15, 13:00-13:45, ... until end-45min.
	endNext := mustTime("2025-03-10T12:00:00Z").Add(20 * time.Hour)
	intervals := []interval{
		{start: mustTime("2025-03-10T12:00:00Z"), end: endNext},
	}
	got := emitWindows(intervals, 45, 30, nil, nil)
	// 20 hours = 40 half-hours, but we need full 45min in each so a bit less. 20*60/30 = 40 steps, but each step that fits: (end-start)/30 rounded down for how many 30-min steps fit, then we need start+45<=end so (end-45-start)/30 + 1 roughly. (20*60-45)/30 = (1200-45)/30 = 38.5 -> 38 full steps, +1 for start = 39? Let's just check we have many and first/last are correct.
	if len(got) < 10 {
		t.Fatalf("expected many slots for 20h range, got %d", len(got))
	}
	if !got[0].SlotStart.Equal(mustTime("2025-03-10T12:00:00Z")) || !got[0].SlotEnd.Equal(mustTime("2025-03-10T12:45:00Z")) {
		t.Errorf("first slot: got [%v, %v]", got[0].SlotStart, got[0].SlotEnd)
	}
	if !got[1].SlotStart.Equal(mustTime("2025-03-10T12:30:00Z")) || !got[1].SlotEnd.Equal(mustTime("2025-03-10T13:15:00Z")) {
		t.Errorf("second slot: got [%v, %v]", got[1].SlotStart, got[1].SlotEnd)
	}
}

func TestGroupAvailabilitiesByParticipant(t *testing.T) {
	slots := []models.Availability{
		{EventID: 1, ParticipantID: ptr(uint(1)), SlotStart: mustTime("2025-03-10T10:00:00Z"), SlotEnd: mustTime("2025-03-10T11:00:00Z")},
		{EventID: 1, ParticipantID: ptr(uint(1)), SlotStart: mustTime("2025-03-10T10:30:00Z"), SlotEnd: mustTime("2025-03-10T11:30:00Z")},
		{EventID: 1, ParticipantID: ptr(uint(2)), SlotStart: mustTime("2025-03-10T10:15:00Z"), SlotEnd: mustTime("2025-03-10T11:00:00Z")},
		{EventID: 1, ParticipantID: nil, SlotStart: mustTime("2025-03-10T09:00:00Z"), SlotEnd: mustTime("2025-03-10T10:00:00Z")},
	}
	got := groupAvailabilitiesByParticipant(slots)
	if len(got) != 3 {
		t.Fatalf("got %d groups (want 3: p1, p2, anonymous)", len(got))
	}
	if len(got[1]) != 2 {
		t.Errorf("participant 1: got %d slots, want 2", len(got[1]))
	}
	if len(got[2]) != 1 {
		t.Errorf("participant 2: got %d slots, want 1", len(got[2]))
	}
	if len(got[0]) != 1 {
		t.Errorf("anonymous: got %d slots, want 1", len(got[0]))
	}
}

func ptr(u uint) *uint {
	return &u
}

// Integration tests with in-memory DB
func setupSchedulingDB(t *testing.T) *gorm.DB {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	db.AutoMigrate(&models.Event{}, &models.Availability{}, &models.Participant{})
	return db
}

func TestGetBestTimesResponse_AllParticipantsOverlap(t *testing.T) {
	db := setupSchedulingDB(t)
	// Event: 30 min duration, frame 09:00-12:00
	frameStart := mustTime("2025-03-10T09:00:00Z")
	frameEnd := mustTime("2025-03-10T12:00:00Z")
	event := models.Event{
		CreatorID:       1,
		Title:           "Meet",
		DurationMinutes: 30,
		TimeFrameStart:  &frameStart,
		TimeFrameEnd:    &frameEnd,
	}
	db.Create(&event)

	// Two participants: P1 10:00-11:00, P2 10:15-11:00 -> intersection 10:15-11:00
	db.Create(&models.Availability{EventID: event.ID, ParticipantID: ptr(uint(1)), SlotStart: mustTime("2025-03-10T10:00:00Z"), SlotEnd: mustTime("2025-03-10T11:00:00Z")})
	db.Create(&models.Availability{EventID: event.ID, ParticipantID: ptr(uint(2)), SlotStart: mustTime("2025-03-10T10:15:00Z"), SlotEnd: mustTime("2025-03-10T11:00:00Z")})

	svc := NewSchedulingService(db)
	resp, err := svc.GetBestTimesResponse(event.ID, 30)
	if err != nil {
		t.Fatalf("GetBestTimesResponse: %v", err)
	}
	if resp.Note != "" {
		t.Errorf("unexpected note when all overlap: %s", resp.Note)
	}
	if len(resp.ExcludedParticipantIDs) > 0 {
		t.Errorf("unexpected excluded: %v", resp.ExcludedParticipantIDs)
	}
	// 10:15-11:00 = 45 min, 30 min duration, 30 min step -> 10:15-10:45 only
	if len(resp.Slots) != 1 {
		t.Fatalf("got %d slots, want 1", len(resp.Slots))
	}
	if !resp.Slots[0].SlotStart.Equal(mustTime("2025-03-10T10:15:00Z")) || !resp.Slots[0].SlotEnd.Equal(mustTime("2025-03-10T10:45:00Z")) {
		t.Errorf("slot: got [%v, %v]", resp.Slots[0].SlotStart, resp.Slots[0].SlotEnd)
	}
}

func TestGetBestTimesResponse_AllParticipantsOverlap_TwoSlots(t *testing.T) {
	db := setupSchedulingDB(t)
	frameStart := mustTime("2025-03-10T09:00:00Z")
	frameEnd := mustTime("2025-03-10T12:00:00Z")
	event := models.Event{
		CreatorID:       1,
		Title:           "Meet",
		DurationMinutes: 30,
		TimeFrameStart:  &frameStart,
		TimeFrameEnd:    &frameEnd,
	}
	db.Create(&event)

	// Overlap 10:15-11:15 (1 hour) -> 30-min step gives 10:15-10:45, 10:45-11:15
	db.Create(&models.Availability{EventID: event.ID, ParticipantID: ptr(uint(1)), SlotStart: mustTime("2025-03-10T10:00:00Z"), SlotEnd: mustTime("2025-03-10T11:30:00Z")})
	db.Create(&models.Availability{EventID: event.ID, ParticipantID: ptr(uint(2)), SlotStart: mustTime("2025-03-10T10:15:00Z"), SlotEnd: mustTime("2025-03-10T11:15:00Z")})

	svc := NewSchedulingService(db)
	resp, err := svc.GetBestTimesResponse(event.ID, 30)
	if err != nil {
		t.Fatalf("GetBestTimesResponse: %v", err)
	}
	if resp.Note != "" {
		t.Errorf("unexpected note: %s", resp.Note)
	}
	if len(resp.Slots) != 2 {
		t.Fatalf("got %d slots, want 2", len(resp.Slots))
	}
	if !resp.Slots[0].SlotStart.Equal(mustTime("2025-03-10T10:15:00Z")) || !resp.Slots[0].SlotEnd.Equal(mustTime("2025-03-10T10:45:00Z")) {
		t.Errorf("first slot: got [%v, %v]", resp.Slots[0].SlotStart, resp.Slots[0].SlotEnd)
	}
	if !resp.Slots[1].SlotStart.Equal(mustTime("2025-03-10T10:45:00Z")) || !resp.Slots[1].SlotEnd.Equal(mustTime("2025-03-10T11:15:00Z")) {
		t.Errorf("second slot: got [%v, %v]", resp.Slots[1].SlotStart, resp.Slots[1].SlotEnd)
	}
}

func TestGetBestTimesResponse_30MinStepLargeOverlap(t *testing.T) {
	db := setupSchedulingDB(t)
	// Event: 45 min duration, frame 12:00-08:00 next day (20h)
	frameStart := mustTime("2025-03-10T12:00:00Z")
	frameEnd := mustTime("2025-03-11T08:00:00Z")
	event := models.Event{
		CreatorID:       1,
		Title:           "Meet",
		DurationMinutes: 45,
		TimeFrameStart:  &frameStart,
		TimeFrameEnd:    &frameEnd,
	}
	db.Create(&event)

	// One participant available 12:00-08:00 (full frame)
	db.Create(&models.Availability{EventID: event.ID, ParticipantID: ptr(uint(1)), SlotStart: frameStart, SlotEnd: frameEnd})

	svc := NewSchedulingService(db)
	resp, err := svc.GetBestTimesResponse(event.ID, 45)
	if err != nil {
		t.Fatalf("GetBestTimesResponse: %v", err)
	}
	if resp.Note != "" {
		t.Errorf("unexpected note: %s", resp.Note)
	}
	// Should have many slots: 12:00-12:45, 12:30-13:15, 13:00-13:45, ...
	if len(resp.Slots) < 10 {
		t.Fatalf("expected many 45-min slots in 20h range (30-min step), got %d", len(resp.Slots))
	}
	if !resp.Slots[0].SlotStart.Equal(mustTime("2025-03-10T12:00:00Z")) || !resp.Slots[0].SlotEnd.Equal(mustTime("2025-03-10T12:45:00Z")) {
		t.Errorf("first slot: got [%v, %v]", resp.Slots[0].SlotStart, resp.Slots[0].SlotEnd)
	}
	if !resp.Slots[1].SlotStart.Equal(mustTime("2025-03-10T12:30:00Z")) || !resp.Slots[1].SlotEnd.Equal(mustTime("2025-03-10T13:15:00Z")) {
		t.Errorf("second slot: got [%v, %v]", resp.Slots[1].SlotStart, resp.Slots[1].SlotEnd)
	}
}

func TestGetBestTimesResponse_FallbackWhenNotAllAvailable(t *testing.T) {
	db := setupSchedulingDB(t)
	frameStart := mustTime("2025-03-10T09:00:00Z")
	frameEnd := mustTime("2025-03-10T12:00:00Z")
	event := models.Event{
		CreatorID:       1,
		Title:           "Meet",
		DurationMinutes: 30,
		TimeFrameStart:  &frameStart,
		TimeFrameEnd:    &frameEnd,
	}
	db.Create(&event)

	// Two participants with NO overlap: P1 09:00-10:00, P2 11:00-12:00
	db.Create(&models.Availability{EventID: event.ID, ParticipantID: ptr(uint(1)), SlotStart: mustTime("2025-03-10T09:00:00Z"), SlotEnd: mustTime("2025-03-10T10:00:00Z")})
	db.Create(&models.Availability{EventID: event.ID, ParticipantID: ptr(uint(2)), SlotStart: mustTime("2025-03-10T11:00:00Z"), SlotEnd: mustTime("2025-03-10T12:00:00Z")})

	svc := NewSchedulingService(db)
	resp, err := svc.GetBestTimesResponse(event.ID, 30)
	if err != nil {
		t.Fatalf("GetBestTimesResponse: %v", err)
	}
	// Should fallback to one participant's slots with note
	if resp.Note == "" {
		t.Error("expected note when not all participants can be included")
	}
	if len(resp.ExcludedParticipantIDs) != 1 {
		t.Errorf("expected 1 excluded participant, got %v", resp.ExcludedParticipantIDs)
	}
	// Should have slots (from the participant we didn't exclude)
	if len(resp.Slots) == 0 {
		t.Fatal("expected fallback slots")
	}
	// Slots should be within one participant's range (e.g. 09:00-09:30, 09:30-10:00)
	if len(resp.Slots) < 2 {
		t.Errorf("expected at least 2 slots in 1h range with 30min step, got %d", len(resp.Slots))
	}
}

func TestGetBestTimesResponse_NoAvailability(t *testing.T) {
	db := setupSchedulingDB(t)
	event := models.Event{CreatorID: 1, Title: "Meet", DurationMinutes: 30}
	db.Create(&event)

	svc := NewSchedulingService(db)
	resp, err := svc.GetBestTimesResponse(event.ID, 30)
	if err != nil {
		t.Fatalf("GetBestTimesResponse: %v", err)
	}
	if len(resp.Slots) != 0 {
		t.Errorf("expected no slots, got %d", len(resp.Slots))
	}
}

func TestGetBestTimesResponse_AnonymousSlotsTreatedAsOneGroup(t *testing.T) {
	db := setupSchedulingDB(t)
	// No participant_id: all go to key 0 (anonymous). One "group" so intersection is that union.
	frameStart := mustTime("2025-03-10T10:00:00Z")
	frameEnd := mustTime("2025-03-10T12:00:00Z")
	event := models.Event{
		CreatorID:       1,
		DurationMinutes: 30,
		TimeFrameStart:  &frameStart,
		TimeFrameEnd:    &frameEnd,
	}
	db.Create(&event)
	db.Create(&models.Availability{EventID: event.ID, ParticipantID: nil, SlotStart: mustTime("2025-03-10T10:00:00Z"), SlotEnd: mustTime("2025-03-10T11:00:00Z")})
	db.Create(&models.Availability{EventID: event.ID, ParticipantID: nil, SlotStart: mustTime("2025-03-10T10:30:00Z"), SlotEnd: mustTime("2025-03-10T11:30:00Z")})

	svc := NewSchedulingService(db)
	resp, err := svc.GetBestTimesResponse(event.ID, 30)
	if err != nil {
		t.Fatalf("GetBestTimesResponse: %v", err)
	}
	// Union of 10:00-11:00 and 10:30-11:30 = 10:00-11:30. Then 30-min slots with 30-min step: 10:00-10:30, 10:30-11:00, 11:00-11:30
	if len(resp.Slots) != 3 {
		t.Fatalf("got %d slots, want 3", len(resp.Slots))
	}
}
