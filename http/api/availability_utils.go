package api

import (
	"fmt"
	"time"

	"linkup-backend/models"
)

// parseISO8601 parses RFC3339 timestamps for slot_start/slot_end.
func parseISO8601(s string) (time.Time, error) {
	return time.Parse(time.RFC3339, s)
}

// validateSlotInEventTimeFrame returns an error if the event has a time frame and the slot is outside it.
func validateSlotInEventTimeFrame(event *models.Event, slotStart, slotEnd time.Time) error {
	if event.TimeFrameStart == nil || event.TimeFrameEnd == nil {
		return nil
	}
	start, end := *event.TimeFrameStart, *event.TimeFrameEnd
	if slotStart.Before(start) {
		return fmt.Errorf("slot_start must be within event time frame (earliest %s)", start.Format(time.RFC3339))
	}
	if slotEnd.After(end) {
		return fmt.Errorf("slot_end must be within event time frame (latest %s)", end.Format(time.RFC3339))
	}
	return nil
}

// validateSlotDuration returns an error if the slot is shorter than the event duration (meeting must fit in slot).
func validateSlotDuration(slotStart, slotEnd time.Time, durationMinutes int) error {
	if durationMinutes <= 0 {
		return nil
	}
	slotMins := int(slotEnd.Sub(slotStart).Minutes())
	if slotMins < durationMinutes {
		return fmt.Errorf("slot length (%d min) is shorter than event duration (%d min); slot must fit a full meeting", slotMins, durationMinutes)
	}
	return nil
}

// validateTimeFrame returns an error if both start and end are set but start is not before end.
func validateTimeFrame(start, end *time.Time) error {
	if start == nil || end == nil {
		return nil
	}
	if !end.After(*start) {
		return fmt.Errorf("time_frame_end must be after time_frame_start")
	}
	return nil
}

// slotsOverlap returns true if the two time ranges overlap.
func slotsOverlap(aStart, aEnd, bStart, bEnd time.Time) bool {
	return aStart.Before(bEnd) && bStart.Before(aEnd)
}
