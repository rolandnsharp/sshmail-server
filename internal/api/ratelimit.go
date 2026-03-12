package api

import (
	"sync"
	"time"
)

// RateLimiter tracks per-agent send rates using a sliding window.
type RateLimiter struct {
	mu      sync.Mutex
	window  time.Duration
	limit   int
	buckets map[int64][]time.Time
}

// NewRateLimiter creates a rate limiter that allows limit sends per window.
func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	return &RateLimiter{
		window:  window,
		limit:   limit,
		buckets: make(map[int64][]time.Time),
	}
}

// Allow checks if agentID can send. Returns true and records the event,
// or returns false if the limit is exceeded.
func (r *RateLimiter) Allow(agentID int64) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-r.window)

	// Evict old entries
	times := r.buckets[agentID]
	start := 0
	for start < len(times) && times[start].Before(cutoff) {
		start++
	}
	times = times[start:]

	if len(times) >= r.limit {
		r.buckets[agentID] = times
		return false
	}

	r.buckets[agentID] = append(times, now)
	return true
}
