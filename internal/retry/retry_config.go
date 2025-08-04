package retry

import "time"

type RetryConfig struct {
	// Type of backoff: Constant, Exponential, Fibonacci, or Custom
	BackoffType BackoffType
	// Backoff function, used for Custom backoff
	BackoffFunc BackoffFunc
	// Base delay time, used for Constant and Exponential backoff
	InitialDelay time.Duration
	// Value to multiply each delay by, used for Exponential backoff
	DelayScale int
	// Maximum delay time, used for all backoffs
	DurationCap time.Duration
	// Maximum time to wait for the function to succeed
	MaxDuration time.Duration
	// Maximum attempts to retry the function
	MaxRetries int
	// Whether jitter should be added to the delay
	Jitter bool
}

type RetryOption func(*RetryConfig)

func DefaultConstantRetryConfig() RetryConfig {
	return RetryConfig{
		BackoffType: Constant,
		InitialDelay: time.Second,
		MaxRetries: 10,
	}
}

func DefaultExponentialRetryConfig() RetryConfig {
	return RetryConfig{
		BackoffType: Exponential,
		InitialDelay: time.Second,
		DelayScale: 2,
		MaxRetries: 10,
	}
}

func DefaultFibonacciRetryConfig() RetryConfig {
	return RetryConfig{
		BackoffType: Fibonacci,
		MaxRetries: 10,
	}
}

func WithBackoffType(backoff_type BackoffType) RetryOption {
	return func(rc *RetryConfig) {
		rc.BackoffType = backoff_type
	}
}

func WithCustomBackoff(backoff_func BackoffFunc) RetryOption {
	return func(rc *RetryConfig) {
		rc.BackoffType = Custom
		rc.BackoffFunc = backoff_func
	}
}

func WithInitialDelay(initial_delay time.Duration) RetryOption {
	return func(rc *RetryConfig) {
		rc.InitialDelay = initial_delay
	}
}

func WithDelayScale(delay_scale int) RetryOption {
	return func(rc *RetryConfig) {
		rc.DelayScale = delay_scale
	}
}

func WithDurationCap(duration_cap time.Duration) RetryOption {
	return func(rc *RetryConfig) {
		rc.DurationCap = duration_cap
	}
}

func WithMaxDuration(max_duration time.Duration) RetryOption {
	return func(rc *RetryConfig) {
		rc.MaxDuration = max_duration
	}
}

func WithMaxRetries(max_retries int) RetryOption {
	return func(rc *RetryConfig) {
		rc.MaxRetries = max_retries
	}
}

func WithJitter() RetryOption {
	return func(rc *RetryConfig) {
		rc.Jitter = true
	}
}

// Get a new retry config from options
// If no options are provided, will return the same
// config as DefaultRetryConstantConfig()
func NewRetryConfig(opts ...RetryOption) RetryConfig {
	retry_config := DefaultConstantRetryConfig()
	for _, opt := range opts {
		opt(&retry_config)
	}

	return retry_config
}