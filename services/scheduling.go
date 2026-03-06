package services

import (
	"sort"
	"time"

	"gorm.io/gorm"

	"linkup-backend/models"
)

// SlotStepMinutes is the step between candidate meeting slots when there are large overlapping windows.
// TODO: Make this configurable
const SlotStepMinutes = 30

// SchedulingService computes overlapping availability slots for best meeting times.
type SchedulingService struct {
	db *gorm.DB
}

func NewSchedulingService(db *gorm.DB) *SchedulingService {
	return &SchedulingService{db: db}
}

// BestTimeResult is a single candidate meeting slot with availability count.
type BestTimeResult struct {
	SlotStart      time.Time `json:"slot_start"`
	SlotEnd        time.Time `json:"slot_end"`
	AvailableCount int      `json:"available_count"` // number of participants who can make this slot
	Total          int      `json:"total"`          // total number of participants (with availability)
}

// BestTimesResponse is the result of finding best times, ranked by most participants then earliest.
type BestTimesResponse struct {
	Slots                  []BestTimeResult `json:"best_times"`
	Note                   string          `json:"note,omitempty"`
	ExcludedParticipantIDs []uint          `json:"excluded_participant_ids,omitempty"`
}

// GetBestTimesResponse finds candidate slots (duration-sized, 30-min step), scores each by number of
// participants available, tie-breaks by earliest time, and returns a ranked list. Also sets Note and
// ExcludedParticipantIDs when the top slot does not include everyone.
func (s *SchedulingService) GetBestTimesResponse(eventID uint, durationMinutes int) (BestTimesResponse, error) {
	var event models.Event
	if err := s.db.First(&event, eventID).Error; err != nil {
		return BestTimesResponse{}, err
	}

	duration := durationMinutes
	if duration <= 0 {
		duration = event.DurationMinutes
	}
	if duration <= 0 {
		duration = 60
	}

	var slots []models.Availability
	if err := s.db.Where("event_id = ?", eventID).Find(&slots).Error; err != nil {
		return BestTimesResponse{}, err
	}

	if len(slots) == 0 {
		return BestTimesResponse{Slots: nil}, nil
	}

	byParticipant := groupAvailabilitiesByParticipant(slots)
	participantKeys := sortedParticipantKeys(byParticipant)
	if len(participantKeys) == 0 {
		return BestTimesResponse{Slots: nil}, nil
	}

	totalParticipants := len(participantKeys)
	participantUnions := make(map[uint][]interval)
	for _, key := range participantKeys {
		participantUnions[key] = mergeIntervals(byParticipant[key])
	}

	// Time range: organizer frame or min/max across all availability
	rangeStart, rangeEnd := timeRange(slots, event.TimeFrameStart, event.TimeFrameEnd)
	if !rangeEnd.After(rangeStart) {
		return BestTimesResponse{Slots: nil}, nil
	}

	dur := time.Duration(duration) * time.Minute
	step := time.Duration(SlotStepMinutes) * time.Minute

	type scoredSlot struct {
		start          time.Time
		end            time.Time
		availableCount int
		availableKeys  []uint
	}

	var scored []scoredSlot
	for start := rangeStart; start.Add(dur).Before(rangeEnd) || start.Add(dur).Equal(rangeEnd); start = start.Add(step) {
		end := start.Add(dur)
		if event.TimeFrameEnd != nil && end.After(*event.TimeFrameEnd) {
			break
		}
		var availableKeys []uint
		for _, key := range participantKeys {
			if intervalCovers(start, end, participantUnions[key]) {
				availableKeys = append(availableKeys, key)
			}
		}
		scored = append(scored, scoredSlot{
			start:          start,
			end:            end,
			availableCount: len(availableKeys),
			availableKeys:  availableKeys,
		})
	}

	// Keep only slots where at least one participant is available
	var scoredFiltered []scoredSlot
	for _, sc := range scored {
		if sc.availableCount > 0 {
			scoredFiltered = append(scoredFiltered, sc)
		}
	}
	scored = scoredFiltered

	// Sort: most participants first, then earliest start
	sort.Slice(scored, func(i, j int) bool {
		if scored[i].availableCount != scored[j].availableCount {
			return scored[i].availableCount > scored[j].availableCount
		}
		return scored[i].start.Before(scored[j].start)
	})

	result := make([]BestTimeResult, len(scored))
	for i, sc := range scored {
		result[i] = BestTimeResult{
			SlotStart:      sc.start,
			SlotEnd:        sc.end,
			AvailableCount: sc.availableCount,
			Total:          totalParticipants,
		}
	}

	resp := BestTimesResponse{Slots: result}

	// Note and excluded when top slot doesn't include everyone
	if len(scored) > 0 && scored[0].availableCount < totalParticipants {
		availableSet := make(map[uint]bool)
		for _, k := range scored[0].availableKeys {
			availableSet[k] = true
		}
		var excludedIDs []uint
		for _, k := range participantKeys {
			if k != 0 && !availableSet[k] {
				excludedIDs = append(excludedIDs, k)
			}
		}
		resp.Note = formatExcludedNote(s.db, excludedIDs)
		resp.ExcludedParticipantIDs = excludedIDs
	}

	return resp, nil
}

// timeRange returns the window to generate candidates: frame if set, else min(start) to max(end) of slots.
func timeRange(slots []models.Availability, frameStart, frameEnd *time.Time) (start, end time.Time) {
	if frameStart != nil && frameEnd != nil {
		return *frameStart, *frameEnd
	}
	if len(slots) == 0 {
		return time.Time{}, time.Time{}
	}
	minT := slots[0].SlotStart
	maxT := slots[0].SlotEnd
	for _, s := range slots[1:] {
		if s.SlotStart.Before(minT) {
			minT = s.SlotStart
		}
		if s.SlotEnd.After(maxT) {
			maxT = s.SlotEnd
		}
	}
	if frameStart != nil && minT.Before(*frameStart) {
		minT = *frameStart
	}
	if frameEnd != nil && maxT.After(*frameEnd) {
		maxT = *frameEnd
	}
	return minT, maxT
}

// intervalCovers reports whether the window [windowStart, windowEnd] is fully inside one of the union intervals.
func intervalCovers(windowStart, windowEnd time.Time, union []interval) bool {
	for _, iv := range union {
		if !windowStart.Before(iv.start) && !windowEnd.After(iv.end) {
			return true
		}
	}
	return false
}

// formatExcludedNote returns a user-friendly note listing excluded participants by email.
func formatExcludedNote(db *gorm.DB, excludedParticipantIDs []uint) string {
	base := "Sorry, there is no time slot where everyone is available :( This is the best we could find"
	if len(excludedParticipantIDs) == 0 {
		return base + ", but one or more participants could not be included."
	}
	var participants []models.Participant
	if err := db.Where("id IN ?", excludedParticipantIDs).Find(&participants).Error; err != nil || len(participants) == 0 {
		return base + "."
	}
	emails := make([]string, len(participants))
	for i, p := range participants {
		emails[i] = p.Email
	}
	switch len(emails) {
	case 1:
		return base + ", but " + emails[0] + " is excluded."
	case 2:
		return base + ", but " + emails[0] + " and " + emails[1] + " are excluded."
	default:
		return base + ", but " + joinEmails(emails) + " are excluded."
	}
}

func joinEmails(emails []string) string {
	if len(emails) == 0 {
		return ""
	}
	if len(emails) == 1 {
		return emails[0]
	}
	s := emails[0]
	for i := 1; i < len(emails)-1; i++ {
		s += ", " + emails[i]
	}
	s += ", and " + emails[len(emails)-1]
	return s
}

type interval struct {
	start time.Time
	end   time.Time
}

func groupAvailabilitiesByParticipant(slots []models.Availability) map[uint][]interval {
	out := make(map[uint][]interval)
	for _, s := range slots {
		key := uint(0)
		if s.ParticipantID != nil {
			key = *s.ParticipantID
		}
		out[key] = append(out[key], interval{start: s.SlotStart, end: s.SlotEnd})
	}
	return out
}

func sortedParticipantKeys(byParticipant map[uint][]interval) []uint {
	keys := make([]uint, 0, len(byParticipant))
	for k := range byParticipant {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	return keys
}

// mergeIntervals returns the union of intervals, merged so no overlaps.
func mergeIntervals(intervals []interval) []interval {
	if len(intervals) == 0 {
		return nil
	}
	// Sort by start
	sorted := make([]interval, len(intervals))
	copy(sorted, intervals)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].start.Before(sorted[j].start) })

	var merged []interval
	cur := sorted[0]
	for i := 1; i < len(sorted); i++ {
		if sorted[i].start.Before(cur.end) || sorted[i].start.Equal(cur.end) {
			if sorted[i].end.After(cur.end) {
				cur.end = sorted[i].end
			}
		} else {
			merged = append(merged, cur)
			cur = sorted[i]
		}
	}
	merged = append(merged, cur)
	return merged
}

// intersectTwo returns intervals that are in both a and b.
func intersectTwo(a, b []interval) []interval {
	var out []interval
	for _, ai := range a {
		for _, bi := range b {
			start := ai.start
			if bi.start.After(start) {
				start = bi.start
			}
			end := ai.end
			if bi.end.Before(end) {
				end = bi.end
			}
			if start.Before(end) {
				out = append(out, interval{start: start, end: end})
			}
		}
	}
	return mergeIntervals(out)
}

func intersectIntervalLists(lists [][]interval) []interval {
	if len(lists) == 0 {
		return nil
	}
	result := lists[0]
	for i := 1; i < len(lists); i++ {
		result = intersectTwo(result, lists[i])
		if len(result) == 0 {
			return nil
		}
	}
	return result
}

func clipToFrame(intervals []interval, frameStart, frameEnd *time.Time) []interval {
	if frameStart == nil && frameEnd == nil {
		return intervals
	}
	var out []interval
	for _, iv := range intervals {
		start, end := iv.start, iv.end
		if frameStart != nil && start.Before(*frameStart) {
			start = *frameStart
		}
		if frameEnd != nil && end.After(*frameEnd) {
			end = *frameEnd
		}
		if start.Before(end) {
			out = append(out, interval{start: start, end: end})
		}
	}
	return out
}

// emitWindows emits all windows of length durationMinutes within intervals, stepping by stepMinutes.
func emitWindows(intervals []interval, durationMinutes int, stepMinutes int, frameStart, frameEnd *time.Time) []BestTimeResult {
	duration := time.Duration(durationMinutes) * time.Minute
	step := time.Duration(stepMinutes) * time.Minute
	var result []BestTimeResult
	for _, iv := range intervals {
		start := iv.start
		if frameStart != nil && start.Before(*frameStart) {
			start = *frameStart
		}
		for {
			end := start.Add(duration)
			if end.After(iv.end) {
				break
			}
			if frameEnd != nil && end.After(*frameEnd) {
				break
			}
			result = append(result, BestTimeResult{SlotStart: start, SlotEnd: end})
			start = start.Add(step)
		}
	}
	return result
}
