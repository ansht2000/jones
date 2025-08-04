package retry

import (
	"context"
	"errors"
	"testing"
	"time"
)

var errTest = errors.New("test error")

func TestRetryExternalCancellation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 1 * time.Second)
	defer cancel()
	err := Retry(ctx, func(ctx context.Context) error {
		return errTest
	}, NewRetryConfig(WithBackoffType(Constant), WithInitialDelay(time.Second)))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Got unexpected error %v, was expecting %v\n", err, context.DeadlineExceeded)
	}
}

func TestRetryTimeout(t *testing.T) {
	err := Retry(context.Background(), func(ctx context.Context) error {
		return errTest
	}, NewRetryConfig(WithBackoffType(Constant), WithMaxDuration(time.Second)))
	if !errors.Is(err, ErrFunctionNotCompletedTimeout) {
		t.Errorf("Got unexpected error %v, was expecting %v\n", err, ErrFunctionNotCompletedTimeout)
	}
}

func TestRetryMaxRetries(t *testing.T) {
	err := Retry(context.Background(), func(ctx context.Context) error {
		return errTest
	}, NewRetryConfig(WithBackoffType(Constant), WithMaxRetries(1)))
	if !errors.Is(err, ErrFunctionNotCompletedMaxRetries) {
		t.Errorf("Got unexpected error %v, was expecting %v\n", err, ErrFunctionNotCompletedMaxRetries)
	}
}

func TestSuccessfulReturn(t *testing.T) {
	err := Retry(context.Background(), func(ctx context.Context) error {
		return nil
	}, DefaultConstantRetryConfig())
	if err != nil {
		t.Errorf("Expected error to be nil, got %v instead\n", err)
	}
}

func TestRetryExternalCancellationValue(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 1 * time.Second)
	defer cancel()
	val, err := RetryWithValue(ctx, func(ctx context.Context) (int, error) {
		return 1, errTest
	}, NewRetryConfig(WithBackoffType(Constant), WithInitialDelay(time.Second)))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Got unexpected error %v, was expecting %v\n", err, context.DeadlineExceeded)
	}
	if val != 0 {
		t.Errorf("Expected nil value, got %d instead\n", val)
	}
}

func TestRetryTimeoutValue(t *testing.T) {
	val, err := RetryWithValue(context.Background(), func(ctx context.Context) (int, error) {
		return 1, errTest
	}, NewRetryConfig(WithBackoffType(Constant), WithMaxDuration(time.Second)))
	if !errors.Is(err, ErrFunctionNotCompletedTimeout) {
		t.Errorf("Got unexpected error %v, was expecting %v\n", err, ErrFunctionNotCompletedTimeout)
	}
	if val != 0 {
		t.Errorf("Expected nil value, got %d instead\n", val)
	}
}

func TestRetryMaxRetriesValue(t *testing.T) {
	val, err := RetryWithValue(context.Background(), func(ctx context.Context) (int, error) {
		return 1, errTest
	}, NewRetryConfig(WithBackoffType(Constant), WithMaxRetries(1)))
	if !errors.Is(err, ErrFunctionNotCompletedMaxRetries) {
		t.Errorf("Got unexpected error %v, was expecting %v\n", err, ErrFunctionNotCompletedMaxRetries)
	}
	if val != 0 {
		t.Errorf("Expected nil value, got %d instead\n", val)
	}
}

func TestSuccessfulReturnValue(t *testing.T) {
	val, err := RetryWithValue(context.Background(), func(ctx context.Context) (int, error) {
		return 1, nil
	}, DefaultConstantRetryConfig())

	if val != 1 {
		t.Errorf("Expected value %d, got %d instead\n", 1, val)
	}
	if err != nil {
		t.Errorf("Expected error to be nil, got %v instead\n", err)
	}
}