package services

import (
	"time"

	"gorm.io/gorm"

	"linkup-backend/models"
)

// SchedulingService computes overlapping availability slots for best meeting times.
type SchedulingService struct {
	db *gorm.DB
}

func NewSchedulingService(db *gorm.DB) *SchedulingService {
	return &SchedulingService{db: db}
}

type BestTimeResult struct {
	SlotStart time.Time `json:"slot_start"`
	SlotEnd   time.Time `json:"slot_end"`
}

// GetBestTime returns the first overlapping slot (backwards compat with /events/:id/best-time).
func (s *SchedulingService) GetBestTime(eventID uint, durationMinutes int) (*BestTimeResult, error) {
	all, err := s.GetBestTimes(eventID, durationMinutes)
	if err != nil || len(all) == 0 {
		return nil, err
	}
	return &all[0], nil
}

// GetBestTimes returns all overlapping windows that fit the event duration.
// Respects event.TimeFrameStart and TimeFrameEnd if set (clips results to that range).
func (s *SchedulingService) GetBestTimes(eventID uint, durationMinutes int) ([]BestTimeResult, error) {
	var event models.Event
	if err := s.db.First(&event, eventID).Error; err != nil {
		return nil, err
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
		return nil, err
	}

	if len(slots) == 0 {
		return nil, nil
	}

	overlaps := findOverlappingSlots(slots, duration, event.TimeFrameStart, event.TimeFrameEnd)
	result := make([]BestTimeResult, len(overlaps))
	for i, o := range overlaps {
		result[i] = BestTimeResult{SlotStart: o.Start, SlotEnd: o.End}
	}
	return result, nil
}

type overlap struct {
	Start time.Time
	End   time.Time
}

// findOverlappingSlots sweeps slot boundaries to find intervals where all participants overlap.
// Each result is clipped to frameStart/frameEnd if provided.
func findOverlappingSlots(slots []models.Availability, durationMinutes int, frameStart, frameEnd *time.Time) []overlap {
	if len(slots) == 0 {
		return nil
	}

	// Collect all boundaries and sort
	type boundary struct {
		t    time.Time
		incr int // +1 start, -1 end
	}
	var boundaries []boundary
	for _, s := range slots {
		boundaries = append(boundaries, boundary{s.SlotStart, 1}, boundary{s.SlotEnd, -1})
	}
	// Simple sort by time
	for i := 0; i < len(boundaries)-1; i++ {
		for j := i + 1; j < len(boundaries); j++ {
			if boundaries[i].t.After(boundaries[j].t) {
				boundaries[i], boundaries[j] = boundaries[j], boundaries[i]
			}
		}
	}

	duration := time.Duration(durationMinutes) * time.Minute
	var result []overlap
	active := 0
	intervalStart := time.Time{}

	for _, b := range boundaries {
		if b.incr == 1 {
			if active == 0 {
				intervalStart = b.t
			}
			active++
		} else {
			active--
			if active == 0 {
				intervalEnd := b.t
				start := intervalStart
				end := intervalStart.Add(duration)
				if intervalEnd.Sub(intervalStart) >= duration {
					if frameStart != nil && start.Before(*frameStart) {
						start = *frameStart
						end = start.Add(duration)
						if end.After(intervalEnd) {
							continue
						}
					}
					if frameEnd != nil && end.After(*frameEnd) {
						continue
					}
					result = append(result, overlap{Start: start, End: end})
				}
			}
		}
	}

	return result
}
