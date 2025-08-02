package retry

import "time"

type RetryConfig struct {
	BackoffType BackoffType
	// only used if backoff type is custom
	BackoffFunc BackoffFunc
	InitialDelay time.Duration
	DelayScale int
	DurationType DurationType
	DurationCap time.Duration
	MaxDuration time.Duration
	MaxRetries int
	Jitter time.Duration
}

type RetryOption func(*RetryConfig)

func DefaultRetryConfigConstant() RetryConfig {
	return RetryConfig{
		BackoffType: Constant,
		InitialDelay: time.Second,
		DurationType: UntilSuccess,
	}
}

func DefaultRetryConfigExponential() RetryConfig {
	return RetryConfig{
		BackoffType: Exponential,
		InitialDelay: time.Second,
		DelayScale: 2,
		DurationType: UntilSuccess,
	}
}

func DefaultRetryConfigFibonacci() RetryConfig {
	return RetryConfig{
		BackoffType: Fibonacci,
		DurationType: UntilSuccess,
	}
}

func WithBackoffType(backoff_type BackoffType) RetryOption {
	return func(rc *RetryConfig) {
		rc.BackoffType = backoff_type
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

func WithDurationType(duration_type DurationType) RetryOption {
	return func(rc *RetryConfig) {
		rc.DurationType = duration_type
	}
}

func WithJitter(jitter time.Duration) RetryOption {
	return func(rc *RetryConfig) {
		rc.Jitter = jitter
	}
}

func WithCustomBackoff(backoff_func BackoffFunc) RetryOption {
	return func(rc *RetryConfig) {
		rc.BackoffType = Custom
		rc.BackoffFunc = backoff_func
	}
}

// Get a new retry config from options
// If no options are provided, will return the same
// config as DefaultRetryConfigConst()
func NewRetryConfig(opts ...RetryOption) RetryConfig {
	retry_config := DefaultRetryConfigConstant()
	for _, opt := range opts {
		opt(&retry_config)
	}

	return retry_config
}