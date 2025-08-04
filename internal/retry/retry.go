package retry

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type RetryFunc func(ctx context.Context) error

type RetryFuncWithValue[T any] func(ctx context.Context) (T, error)

type RetryReturn[T any] struct {
	val T
	err error
}

var ErrFunctionNotCompletedMaxRetries = errors.New("function did not succeed within given amount of retries")
var ErrFunctionNotCompletedTimeout = errors.New("function did not succeed within given time")

func RetryWithValue[T any](ctx context.Context, retry_func RetryFuncWithValue[T], retry_config RetryConfig) (T, error) {
	// nil value for type of return
	var nilT T
	
	// check if context is already cancelled
	select {
	case <-ctx.Done():
		return nilT, ctx.Err()
	default:
	}

	// create cancellation if max duration is set
	var cancel context.CancelFunc
	if retry_config.MaxDuration > 0 {
		ctx, cancel = context.WithTimeoutCause(ctx, retry_config.MaxDuration, ErrFunctionNotCompletedTimeout)
		defer cancel()
	}

	done := make(chan RetryReturn[T])
	go func() {
		val, err := retry_func(ctx)
		if err == nil {
			done <- RetryReturn[T]{
				val: val,
				err: nil,
			}
			close(done)
			return
		}

		var cancel context.CancelFunc
		ctx, cancel = context.WithCancel(ctx)
		defer cancel()

		backoff := make(chan time.Duration)
		
		var backoff_func BackoffFunc
		if retry_config.BackoffType == Custom {
			backoff_func = retry_config.BackoffFunc
		} else {
			backoff_func = backoffMap[retry_config.BackoffType]
		}

		go backoff_func(ctx, backoff, retry_config)

		for i := retry_config.MaxRetries; i != 0; i-- {
			select {
			case <-ctx.Done():
				done <- RetryReturn[T]{
					val: nilT,
					err: context.Cause(ctx),
				}
				close(done)
				return
			default:
				sleep_time := <-backoff
				time.Sleep(sleep_time)
				val, err = retry_func(ctx)
				if err == nil {
					done <- RetryReturn[T]{
						val: val,
						err: nil,
					}
					close(done)
					return
				}
			}
		}
		done <- RetryReturn[T]{
			val: nilT,
			err: ErrFunctionNotCompletedMaxRetries,
		}
		close(done)
	}()

	select {
	case <-ctx.Done():
		return nilT, context.Cause(ctx)
	case retry_return := <-done:
		return retry_return.val, retry_return.err
	}
}

func Retry(ctx context.Context, retry_func RetryFunc, retry_config RetryConfig) error {	
	// check if context is already cancelled
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// create cancellation if max duration is set
	var cancel context.CancelFunc
	if retry_config.MaxDuration > 0 {
		ctx, cancel = context.WithTimeoutCause(ctx, retry_config.MaxDuration, ErrFunctionNotCompletedTimeout)
		defer cancel()
	}

	done := make(chan error)
	go func() {
		err := retry_func(ctx)
		if err == nil {
			done <- nil
			close(done)
			return
		}

		var cancel context.CancelFunc
		ctx, cancel = context.WithCancel(ctx)
		defer cancel()

		backoff := make(chan time.Duration)
		
		var backoff_func BackoffFunc
		if retry_config.BackoffType == Custom {
			backoff_func = retry_config.BackoffFunc
		} else {
			backoff_func = backoffMap[retry_config.BackoffType]
		}

		go backoff_func(ctx, backoff, retry_config)

		for i := retry_config.MaxRetries; i != 0; i-- {
			select {
			case <-ctx.Done():
				done <- context.Cause(ctx)
				close(done)
				return
			default:
				sleep_time := <-backoff
				fmt.Printf("Sleeping for %v seconds\n", sleep_time)
				time.Sleep(sleep_time)
				err = retry_func(ctx)
				if err == nil {
					done <- nil
					close(done)
					return
				}
			}
		}
		done <- ErrFunctionNotCompletedMaxRetries
		close(done)
	}()

	select {
	case <-ctx.Done():
		return context.Cause(ctx)
	case err := <-done:
		return err
	}
}
