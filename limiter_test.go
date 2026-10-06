package main

import (
	"context"
	"testing"
	"time"
)

func TestRateLimiterAcquire(t *testing.T) {
	// 60 per min = 1 per sec
	limiter := NewRateLimiter(60)
	ctx := context.Background()

	// Should consume initial burst without waiting
	for i := 0; i < 60; i++ {
		if err := limiter.Acquire(ctx); err != nil {
			t.Fatalf("unexpected error acquiring token: %v", err)
		}
	}

	// Next acquire should wait ~1s
	start := time.Now()
	if err := limiter.Acquire(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	elapsed := time.Since(start)
	if elapsed < 800*time.Millisecond {
		t.Fatalf("expected delay >800ms, got %v", elapsed)
	}
}

func TestRateLimiterBackoff(t *testing.T) {
	limiter := NewRateLimiter(100)
	ctx := context.Background()

	// Pause for 100ms
	limiter.Backoff(100 * time.Millisecond)

	start := time.Now()
	if err := limiter.Acquire(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	elapsed := time.Since(start)
	if elapsed < 90*time.Millisecond {
		t.Fatalf("expected backoff delay >90ms, got %v", elapsed)
	}
}
