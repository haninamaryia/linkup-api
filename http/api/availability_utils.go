package api

import "time"

// parseISO8601 parses RFC3339 timestamps for slot_start/slot_end.
func parseISO8601(s string) (time.Time, error) {
	return time.Parse(time.RFC3339, s)
}
