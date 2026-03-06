package services

import (
	"sync"
	"time"
)

// AuthCodeRateLimiter limits how often a single email can request a verification code.
type AuthCodeRateLimiter interface {
	Allow(email string) bool
}

// NewAuthCodeRateLimiter returns an in-memory per-email rate limiter.
// maxPerWindow is the max requests allowed per email in the given window (e.g. 3 per 15m).
func NewAuthCodeRateLimiter(maxPerWindow int, window time.Duration) AuthCodeRateLimiter {
	if maxPerWindow <= 0 {
		maxPerWindow = 3
	}
	if window <= 0 {
		window = 15 * time.Minute
	}
	return &authCodeRateLimiter{
		mu:           sync.Mutex{},
		byEmail:      make(map[string][]time.Time),
		maxPerWindow: maxPerWindow,
		window:       window,
	}
}

type authCodeRateLimiter struct {
	mu           sync.Mutex
	byEmail      map[string][]time.Time
	maxPerWindow int
	window       time.Duration
}

func (r *authCodeRateLimiter) Allow(email string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	cutoff := now.Add(-r.window)
	if r.byEmail[email] == nil {
		r.byEmail[email] = []time.Time{}
	}
	// Drop timestamps outside the window
	valid := r.byEmail[email][:0]
	for _, t := range r.byEmail[email] {
		if t.After(cutoff) {
			valid = append(valid, t)
		}
	}
	r.byEmail[email] = valid
	if len(valid) >= r.maxPerWindow {
		return false
	}
	r.byEmail[email] = append(r.byEmail[email], now)
	return true
}
