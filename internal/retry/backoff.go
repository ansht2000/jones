package retry

import (
	"context"
	"time"
)

type BackoffType int

const (
	Constant BackoffType = iota
	Exponential
	Fibonacci
	Custom
)

type BackoffFunc func(ctx context.Context, backoff chan<- time.Duration, retry_config RetryConfig)

// TODO: look into turning this into a function that returns a map
// research which one is more idiomatic/performant
var backoffMap = map[BackoffType]BackoffFunc{
	Constant: constantBackoff,
	Exponential: exponentialBackoff,
	Fibonacci: fibonacciBackoff,
}

func constantBackoff(ctx context.Context, backoff chan<- time.Duration, retry_config RetryConfig) {
	delay := retry_config.InitialDelay
	for {
		select {
		case <-ctx.Done():
			return
		default:
			backoff <- delay
		}
	}
}

func exponentialBackoff(ctx context.Context, backoff chan<- time.Duration, retry_config RetryConfig) {
	delay := retry_config.InitialDelay
	scale := retry_config.DelayScale
	for {
		select {
		case <-ctx.Done():
			return
		case backoff <- delay:
			delay *= time.Duration(scale)
		}
	}
}

func fibonacciBackoff(ctx context.Context, backoff chan<- time.Duration, retry_config RetryConfig) {}