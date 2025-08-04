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
		t.Errorf("Got unexpected error %v, was expecting %v", err, context.DeadlineExceeded)
	}
}

func TestRetryTimeout(t *testing.T) {
	err := Retry(context.Background(), func(ctx context.Context) error {
		return errTest
	}, NewRetryConfig(WithBackoffType(Constant), WithMaxDuration(time.Second)))
	if !errors.Is(err, ErrFunctionNotCompletedTimeout) {
		t.Errorf("Got unexpected error %v, was expecting %v", err, ErrFunctionNotCompletedTimeout)
	}
}

func TestRetryMaxRetries(t *testing.T) {
	err := Retry(context.Background(), func(ctx context.Context) error {
		return errTest
	}, NewRetryConfig(WithBackoffType(Constant), WithMaxRetries(1)))
	if !errors.Is(err, ErrFunctionNotCompletedMaxRetries) {
		t.Errorf("Got unexpected error %v, was expecting %v", err, ErrFunctionNotCompletedMaxRetries)
	}
}

func TestSuccessfulReturn(t *testing.T) {
	err := Retry(context.Background(), func(ctx context.Context) error {
		return nil
	}, DefaultConstantRetryConfig())
	if err != nil {
		t.Errorf("Expected error to be nil, got %v instead", err)
	}
}