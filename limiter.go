package main

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// RateLimiter enforces a maximum request rate per minute with burst capacity
// and dynamic backoff for API 429 responses.
type RateLimiter struct {
	mu          sync.Mutex
	capacity    float64
	tokens      float64
	refillRate  float64 // tokens per second
	lastRefill  time.Time
	pauseUntil  time.Time
}

// NewRateLimiter returns a limiter for the given rate per minute.
func NewRateLimiter(ratePerMinute int) *RateLimiter {
	if ratePerMinute <= 0 {
		ratePerMinute = 50
	}
	cap := float64(ratePerMinute)
	return &RateLimiter{
		capacity:   cap,
		tokens:     cap,
		refillRate: cap / 60.0,
		lastRefill: time.Now(),
	}
}

// Acquire blocks until a token is available or context is cancelled.
func (r *RateLimiter) Acquire(ctx context.Context) error {
	for {
		r.mu.Lock()
		now := time.Now()

		if now.Before(r.pauseUntil) {
			wait := r.pauseUntil.Sub(now)
			r.mu.Unlock()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(wait):
				continue
			}
		}

		// Refill tokens based on elapsed time
		elapsed := now.Sub(r.lastRefill).Seconds()
		r.lastRefill = now
		r.tokens += elapsed * r.refillRate
		if r.tokens > r.capacity {
			r.tokens = r.capacity
		}

		if r.tokens >= 1.0 {
			r.tokens -= 1.0
			r.mu.Unlock()
			return nil
		}

		// Calculate sleep needed for 1 token
		needed := 1.0 - r.tokens
		sleepDur := time.Duration((needed / r.refillRate) * float64(time.Second))
		r.mu.Unlock()

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(sleepDur):
		}
	}
}

// Backoff pauses all tokens until duration passes (e.g., on HTTP 429).
func (r *RateLimiter) Backoff(d time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	until := time.Now().Add(d)
	if until.After(r.pauseUntil) {
		r.pauseUntil = until
	}
}

// Status returns current token count and pause state.
func (r *RateLimiter) Status() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return fmt.Sprintf("tokens: %.1f/%.0f, paused: %v", r.tokens, r.capacity, time.Now().Before(r.pauseUntil))
}
