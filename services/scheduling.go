package services

import (
	"fmt"
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

// BestTimeResult is a single candidate meeting slot.
type BestTimeResult struct {
	SlotStart time.Time `json:"slot_start"`
	SlotEnd   time.Time `json:"slot_end"`
}

// BestTimesResponse is the result of finding best times, with optional fallback note.
type BestTimesResponse struct {
	Slots                   []BestTimeResult `json:"best_times"`
	Note                    string          `json:"note,omitempty"`
	ExcludedParticipantIDs  []uint          `json:"excluded_participant_ids,omitempty"`
}

// GetBestTimesResponse finds all possible meeting slots of the given duration within the organizer's
// time frame where all participants are available. Uses 30-minute steps for large overlaps.
// If no slot exists where all are available, returns slots where most are available and sets Note
// and ExcludedParticipantIDs.
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

	// Group by participant: key 0 = anonymous, else participant ID
	byParticipant := groupAvailabilitiesByParticipant(slots)
	participantKeys := sortedParticipantKeys(byParticipant)
	if len(participantKeys) == 0 {
		return BestTimesResponse{Slots: nil}, nil
	}

	// Union per participant (merge overlapping intervals)
	participantUnions := make(map[uint][]interval)
	for _, key := range participantKeys {
		participantUnions[key] = mergeIntervals(byParticipant[key])
	}

	// Intersection of all participants
	var unionsAll [][]interval
	for _, key := range participantKeys {
		unionsAll = append(unionsAll, participantUnions[key])
	}
	allIntervals := intersectIntervalLists(unionsAll)
	allIntervals = clipToFrame(allIntervals, event.TimeFrameStart, event.TimeFrameEnd)

	if len(allIntervals) > 0 {
		slotsOut := emitWindows(allIntervals, duration, SlotStepMinutes, event.TimeFrameStart, event.TimeFrameEnd)
		return BestTimesResponse{Slots: slotsOut}, nil
	}

	// Fallback: try excluding one participant at a time
	for _, excludeKey := range participantKeys {
		var unionsWithout [][]interval
		for _, k := range participantKeys {
			if k != excludeKey {
				unionsWithout = append(unionsWithout, participantUnions[k])
			}
		}
		intervals := intersectIntervalLists(unionsWithout)
		intervals = clipToFrame(intervals, event.TimeFrameStart, event.TimeFrameEnd)
		if len(intervals) > 0 {
			slotsOut := emitWindows(intervals, duration, SlotStepMinutes, event.TimeFrameStart, event.TimeFrameEnd)
			var excludedIDs []uint
			if excludeKey != 0 {
				excludedIDs = []uint{excludeKey}
			}
			note := "No time slot where all participants are available. Showing slots where the most participants are available."
			if len(excludedIDs) > 0 {
				note += fmt.Sprintf(" Excluded participant ID(s): %v", excludedIDs)
			}
			return BestTimesResponse{
				Slots:                  slotsOut,
				Note:                   note,
				ExcludedParticipantIDs: excludedIDs,
			}, nil
		}
	}

	return BestTimesResponse{Slots: nil}, nil
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
