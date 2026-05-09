package utils

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// RetryableFunc is a function that can be retried
type RetryableFunc func() error

// RetryConfig holds retry configuration
type RetryConfig struct {
	MaxRetries  int
	InitialWait time.Duration
	MaxWait     time.Duration
	Backoff     float64
}

// DefaultRetryConfig returns default retry configuration
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxRetries:  3,
		InitialWait: 1 * time.Second,
		MaxWait:     30 * time.Second,
		Backoff:     2.0,
	}
}

// Retry retries a function with exponential backoff
func Retry(ctx context.Context, config RetryConfig, fn RetryableFunc) error {
	var err error
	wait := config.InitialWait

	for attempt := 0; attempt <= config.MaxRetries; attempt++ {
		err = fn()
		if err == nil {
			return nil
		}

		if attempt == config.MaxRetries {
			break
		}

		slog.Warn("retry attempt failed",
			"attempt", attempt+1,
			"max_retries", config.MaxRetries,
			"error", err,
			"wait", wait)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
			// Exponential backoff
			wait = time.Duration(float64(wait) * config.Backoff)
			if wait > config.MaxWait {
				wait = config.MaxWait
			}
		}
	}

	return fmt.Errorf("failed after %d retries: %w", config.MaxRetries, err)
}

// RetryWithFixedDelay retries with a fixed delay
func RetryWithFixedDelay(ctx context.Context, maxRetries int, delay time.Duration, fn RetryableFunc) error {
	config := RetryConfig{
		MaxRetries:  maxRetries,
		InitialWait: delay,
		MaxWait:     delay,
		Backoff:     1.0,
	}
	return Retry(ctx, config, fn)
}
