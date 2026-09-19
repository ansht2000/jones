package retry

import (
	"context"
	"math"
	"math/rand"
	"time"
)

type BackoffType int

const (
	Constant BackoffType = iota
	Exponential
	Fibonacci
	Custom
)

const maxDuration = time.Duration(math.MaxInt64)

// A BackoffFunc sends the delay before each retry on the backoff channel.
// It must return when ctx is done. Closing the channel early means no more
// retries should be attempted.
type BackoffFunc func(ctx context.Context, backoff chan<- time.Duration, retry_config RetryConfig)

// TODO: look into turning this into a function that returns a map
// research which one is more idiomatic/performant
var backoffMap = map[BackoffType]BackoffFunc{
	Constant:    constantBackoff,
	Exponential: exponentialBackoff,
	Fibonacci:   fibonacciBackoff,
}

// Build a BackoffFunc from a function mapping the retry number
// (starting at 0) to a delay
func BackoffFromFunc(delay_func func(attempt int) time.Duration) BackoffFunc {
	return func(ctx context.Context, backoff chan<- time.Duration, retry_config RetryConfig) {
		defer close(backoff)

		for attempt := 0; ; attempt++ {
			select {
			case <-ctx.Done():
				return
			case backoff <- delay_func(attempt):
			}
		}
	}
}

func constantBackoff(ctx context.Context, backoff chan<- time.Duration, retry_config RetryConfig) {
	BackoffFromFunc(func(int) time.Duration {
		return retry_config.InitialDelay
	})(ctx, backoff, retry_config)
}

func exponentialBackoff(ctx context.Context, backoff chan<- time.Duration, retry_config RetryConfig) {
	delay := retry_config.InitialDelay
	BackoffFromFunc(func(attempt int) time.Duration {
		current := delay
		delay = scaleDuration(delay, retry_config.DelayScale)
		return current
	})(ctx, backoff, retry_config)
}

// delays follow InitialDelay * (1, 1, 2, 3, 5, 8, ...)
func fibonacciBackoff(ctx context.Context, backoff chan<- time.Duration, retry_config RetryConfig) {
	a, b := 1.0, 1.0
	BackoffFromFunc(func(attempt int) time.Duration {
		current := scaleDuration(retry_config.InitialDelay, a)
		a, b = b, a+b
		return current
	})(ctx, backoff, retry_config)
}

// multiply a duration, clamping to the max duration instead of overflowing
func scaleDuration(d time.Duration, scale float64) time.Duration {
	scaled := float64(d) * scale
	if scaled >= float64(maxDuration) {
		return maxDuration
	}
	if scaled <= 0 {
		return 0
	}
	return time.Duration(scaled)
}

// returns a random jitter in [-duration/2, duration/2)
func getRandomJitter(duration time.Duration) int64 {
	return rand.Int63n(int64(duration)) - (int64(duration) / 2)
}
