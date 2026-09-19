package retry

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type RetryFunc func(ctx context.Context) error

type RetryFuncWithValue[T any] func(ctx context.Context) (T, error)

var (
	ErrFunctionNotCompletedMaxRetries = errors.New("function did not succeed within given amount of retries")
	ErrFunctionNotCompletedTimeout    = errors.New("function did not succeed within given time")
)

type permanentError struct {
	err error
}

func (p *permanentError) Error() string { return p.err.Error() }

func (p *permanentError) Unwrap() error { return p.err }

// Wrap an error to stop retrying immediately, the retry
// functions return the unwrapped error
func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return &permanentError{err: err}
}

func Retry(ctx context.Context, retry_func RetryFunc, retry_config RetryConfig) error {
	_, err := RetryWithValue(ctx, func(ctx context.Context) (struct{}, error) {
		return struct{}{}, retry_func(ctx)
	}, retry_config)
	return err
}

func RetryWithValue[T any](ctx context.Context, retry_func RetryFuncWithValue[T], retry_config RetryConfig) (T, error) {
	// nil value for type of return
	var nilT T

	// create cancellation if max duration is set
	if retry_config.MaxDuration > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeoutCause(ctx, retry_config.MaxDuration, ErrFunctionNotCompletedTimeout)
		defer cancel()
	}

	// started lazily so functions that succeed first try never spawn a goroutine,
	// cancelled on return so the backoff goroutine always exits
	var backoff chan time.Duration
	backoff_ctx, cancel_backoff := context.WithCancel(ctx)
	defer cancel_backoff()

	var last_err error
	for attempt := 0; ; attempt++ {
		if ctx.Err() != nil {
			return nilT, cancellationError(ctx, last_err)
		}

		val, err := retry_func(ctx)
		if err == nil {
			return val, nil
		}
		last_err = err

		var permanent_err *permanentError
		if errors.As(err, &permanent_err) {
			return nilT, permanent_err.err
		}
		if retry_config.RetryIf != nil && !retry_config.RetryIf(err) {
			return nilT, err
		}
		if retry_config.MaxRetries >= 0 && attempt >= retry_config.MaxRetries {
			return nilT, fmt.Errorf("%w: %w", ErrFunctionNotCompletedMaxRetries, err)
		}

		if backoff == nil {
			backoff_func := backoffMap[retry_config.BackoffType]
			if retry_config.BackoffType == Custom {
				backoff_func = retry_config.BackoffFunc
			}
			if backoff_func == nil {
				return nilT, fmt.Errorf("no backoff function for backoff type %d", retry_config.BackoffType)
			}
			backoff = make(chan time.Duration)
			go backoff_func(backoff_ctx, backoff, retry_config)
		}

		var delay time.Duration
		var ok bool
		select {
		case <-ctx.Done():
			return nilT, cancellationError(ctx, last_err)
		case delay, ok = <-backoff:
		}
		// a backoff closing its channel means it has no more delays to give
		if !ok {
			return nilT, fmt.Errorf("%w: %w", ErrFunctionNotCompletedMaxRetries, err)
		}

		delay = retry_config.applyJitterAndCap(delay)
		if retry_config.OnRetry != nil {
			retry_config.OnRetry(attempt+1, err, delay)
		}

		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nilT, cancellationError(ctx, last_err)
		case <-timer.C:
		}
	}
}

func (rc RetryConfig) applyJitterAndCap(delay time.Duration) time.Duration {
	// check to make sure the delay is greater than 0
	// so the rand generator doesn't panic
	if rc.Jitter && delay > 0 {
		delay += time.Duration(getRandomJitter(delay))
	}
	if rc.DurationCap > 0 && delay > rc.DurationCap {
		delay = rc.DurationCap
	}
	return max(delay, 0)
}

// the context's cause, plus the last error the function returned if there was one
func cancellationError(ctx context.Context, last_err error) error {
	cause := context.Cause(ctx)
	if last_err == nil {
		return cause
	}
	return fmt.Errorf("%w: %w", cause, last_err)
}
