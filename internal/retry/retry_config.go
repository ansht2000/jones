package retry

import "time"

type RetryConfig struct {
	// Type of backoff: Constant, Exponential, Fibonacci, or Custom
	BackoffType BackoffType
	// Backoff function, used for Custom backoff
	BackoffFunc BackoffFunc
	// Base delay time, used for Constant, Exponential, and Fibonacci backoff
	InitialDelay time.Duration
	// Value to multiply each delay by, used for Exponential backoff
	DelayScale float64
	// Maximum delay time, used for all backoffs, applied after jitter
	DurationCap time.Duration
	// Maximum time to wait for the function to succeed
	MaxDuration time.Duration
	// Maximum attempts to retry the function after the first call
	// 0 means the function is only called once, a negative value
	// means retry until success, cancellation, or MaxDuration
	MaxRetries int
	// Whether jitter should be added to the delay
	Jitter bool
	// Decides whether an error should be retried, if nil every error
	// is retried except ones wrapped with Permanent
	RetryIf func(err error) bool
	// Called before sleeping for each retry, attempt starts at 1
	OnRetry func(attempt int, err error, delay time.Duration)
}

type RetryOption func(*RetryConfig)

func DefaultConstantRetryConfig() RetryConfig {
	return RetryConfig{
		BackoffType:  Constant,
		InitialDelay: time.Second,
		// set so switching to Exponential with WithBackoffType
		// does not collapse every delay to 0
		DelayScale: 2,
		MaxRetries: 10,
	}
}

func DefaultExponentialRetryConfig() RetryConfig {
	return RetryConfig{
		BackoffType:  Exponential,
		InitialDelay: time.Second,
		DelayScale:   2,
		MaxRetries:   10,
	}
}

func DefaultFibonacciRetryConfig() RetryConfig {
	return RetryConfig{
		BackoffType:  Fibonacci,
		InitialDelay: time.Second,
		DelayScale:   2,
		MaxRetries:   10,
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

func WithDelayScale(delay_scale float64) RetryOption {
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

func WithRetryIf(retry_if func(err error) bool) RetryOption {
	return func(rc *RetryConfig) {
		rc.RetryIf = retry_if
	}
}

func WithOnRetry(on_retry func(attempt int, err error, delay time.Duration)) RetryOption {
	return func(rc *RetryConfig) {
		rc.OnRetry = on_retry
	}
}

// Get a new retry config from options
// If no options are provided, will return the same
// config as DefaultConstantRetryConfig()
func NewRetryConfig(opts ...RetryOption) RetryConfig {
	retry_config := DefaultConstantRetryConfig()
	for _, opt := range opts {
		opt(&retry_config)
	}

	return retry_config
}
