package retry

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"
)

var errTest = errors.New("test error")

func alwaysFail(ctx context.Context) error { return errTest }

func TestRetryExternalCancellation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	err := Retry(ctx, alwaysFail, NewRetryConfig(WithBackoffType(Constant), WithInitialDelay(time.Second)))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Got unexpected error %v, was expecting %v\n", err, context.DeadlineExceeded)
	}
	if !errors.Is(err, errTest) {
		t.Errorf("Expected error %v to wrap the last function error %v\n", err, errTest)
	}
}

func TestRetryAlreadyCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls := 0
	err := Retry(ctx, func(ctx context.Context) error {
		calls++
		return nil
	}, DefaultConstantRetryConfig())
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Got unexpected error %v, was expecting %v\n", err, context.Canceled)
	}
	if calls != 0 {
		t.Errorf("Expected function not to be called, was called %d times\n", calls)
	}
}

func TestRetryTimeout(t *testing.T) {
	err := Retry(context.Background(), alwaysFail,
		NewRetryConfig(WithBackoffType(Constant), WithInitialDelay(10*time.Millisecond), WithMaxDuration(100*time.Millisecond)))
	if !errors.Is(err, ErrFunctionNotCompletedTimeout) {
		t.Errorf("Got unexpected error %v, was expecting %v\n", err, ErrFunctionNotCompletedTimeout)
	}
}

func TestRetryMaxRetries(t *testing.T) {
	calls := 0
	err := Retry(context.Background(), func(ctx context.Context) error {
		calls++
		return errTest
	}, NewRetryConfig(WithBackoffType(Constant), WithInitialDelay(time.Millisecond), WithMaxRetries(3)))
	if !errors.Is(err, ErrFunctionNotCompletedMaxRetries) {
		t.Errorf("Got unexpected error %v, was expecting %v\n", err, ErrFunctionNotCompletedMaxRetries)
	}
	if !errors.Is(err, errTest) {
		t.Errorf("Expected error %v to wrap the last function error %v\n", err, errTest)
	}
	if calls != 4 {
		t.Errorf("Expected 1 call plus 3 retries, got %d calls\n", calls)
	}
}

func TestRetryZeroMaxRetries(t *testing.T) {
	calls := 0
	err := Retry(context.Background(), func(ctx context.Context) error {
		calls++
		return errTest
	}, NewRetryConfig(WithMaxRetries(0)))
	if !errors.Is(err, ErrFunctionNotCompletedMaxRetries) {
		t.Errorf("Got unexpected error %v, was expecting %v\n", err, ErrFunctionNotCompletedMaxRetries)
	}
	if calls != 1 {
		t.Errorf("Expected 1 call, got %d\n", calls)
	}
}

func TestRetryUnlimitedRetries(t *testing.T) {
	calls := 0
	err := Retry(context.Background(), func(ctx context.Context) error {
		calls++
		return errTest
	}, NewRetryConfig(WithInitialDelay(time.Millisecond), WithMaxRetries(-1), WithMaxDuration(100*time.Millisecond)))
	if !errors.Is(err, ErrFunctionNotCompletedTimeout) {
		t.Errorf("Got unexpected error %v, was expecting %v\n", err, ErrFunctionNotCompletedTimeout)
	}
	if calls < 10 {
		t.Errorf("Expected retries to continue until timeout, only got %d calls\n", calls)
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

func TestSuccessAfterFailures(t *testing.T) {
	calls := 0
	val, err := RetryWithValue(context.Background(), func(ctx context.Context) (int, error) {
		calls++
		if calls < 3 {
			return 0, errTest
		}
		return 42, nil
	}, NewRetryConfig(WithInitialDelay(time.Millisecond)))
	if err != nil {
		t.Errorf("Expected error to be nil, got %v instead\n", err)
	}
	if val != 42 || calls != 3 {
		t.Errorf("Expected value 42 after 3 calls, got %d after %d calls\n", val, calls)
	}
}

func TestPermanentStopsRetrying(t *testing.T) {
	calls := 0
	err := Retry(context.Background(), func(ctx context.Context) error {
		calls++
		return Permanent(errTest)
	}, NewRetryConfig(WithInitialDelay(time.Millisecond)))
	if err != errTest {
		t.Errorf("Expected unwrapped error %v, got %v\n", errTest, err)
	}
	if calls != 1 {
		t.Errorf("Expected 1 call, got %d\n", calls)
	}
}

func TestPermanentNil(t *testing.T) {
	if Permanent(nil) != nil {
		t.Error("Expected Permanent(nil) to be nil")
	}
}

func TestRetryIf(t *testing.T) {
	errRetryable := errors.New("retryable")
	calls := 0
	err := Retry(context.Background(), func(ctx context.Context) error {
		calls++
		if calls < 3 {
			return errRetryable
		}
		return errTest
	}, NewRetryConfig(WithInitialDelay(time.Millisecond), WithRetryIf(func(err error) bool {
		return errors.Is(err, errRetryable)
	})))
	if err != errTest {
		t.Errorf("Expected non retryable error %v, got %v\n", errTest, err)
	}
	if calls != 3 {
		t.Errorf("Expected 3 calls, got %d\n", calls)
	}
}

func TestOnRetry(t *testing.T) {
	attempts := []int{}
	err := Retry(context.Background(), alwaysFail,
		NewRetryConfig(WithInitialDelay(time.Millisecond), WithMaxRetries(3), WithOnRetry(func(attempt int, err error, delay time.Duration) {
			if !errors.Is(err, errTest) {
				t.Errorf("OnRetry got unexpected error %v\n", err)
			}
			if delay != time.Millisecond {
				t.Errorf("OnRetry got unexpected delay %v\n", delay)
			}
			attempts = append(attempts, attempt)
		})))
	if !errors.Is(err, ErrFunctionNotCompletedMaxRetries) {
		t.Errorf("Got unexpected error %v\n", err)
	}
	if !slices.Equal(attempts, []int{1, 2, 3}) {
		t.Errorf("Expected attempts [1 2 3], got %v\n", attempts)
	}
}

func TestRetryAfter(t *testing.T) {
	errSlowDown := errors.New("slow down")
	ms := time.Millisecond
	calls := 0
	delays := []time.Duration{}
	Retry(context.Background(), func(ctx context.Context) error {
		calls++
		if calls == 1 {
			return errSlowDown
		}
		return errTest
	}, NewRetryConfig(
		WithBackoffType(Exponential),
		WithInitialDelay(5*ms),
		WithDurationCap(8*ms),
		WithMaxRetries(3),
		WithRetryAfter(func(err error) (time.Duration, bool) {
			if errors.Is(err, errSlowDown) {
				return 20 * ms, true
			}
			// shorter than the backoff, so the backoff's delay is used
			return ms, true
		}),
		WithOnRetry(func(attempt int, err error, delay time.Duration) {
			delays = append(delays, delay)
		}),
	))

	// the requested delay replaces the backoff and ignores the cap
	expected := []time.Duration{20 * ms, 8 * ms, 8 * ms}
	if !slices.Equal(delays, expected) {
		t.Errorf("Expected delays %v, got %v\n", expected, delays)
	}
}

func TestRetryAfterNotRequested(t *testing.T) {
	delays := []time.Duration{}
	Retry(context.Background(), alwaysFail, NewRetryConfig(
		WithInitialDelay(time.Millisecond),
		WithMaxRetries(2),
		WithRetryAfter(func(err error) (time.Duration, bool) {
			return time.Hour, false
		}),
		WithOnRetry(func(attempt int, err error, delay time.Duration) {
			delays = append(delays, delay)
		}),
	))

	expected := []time.Duration{time.Millisecond, time.Millisecond}
	if !slices.Equal(delays, expected) {
		t.Errorf("Expected delays %v, got %v\n", expected, delays)
	}
}

func TestCancelDuringSleep(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	err := Retry(ctx, alwaysFail, NewRetryConfig(WithInitialDelay(time.Hour)))
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Got unexpected error %v, was expecting %v\n", err, context.Canceled)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("Expected cancellation to interrupt the sleep, took %v\n", elapsed)
	}
}

// collect the first n delays from a backoff function
func collectDelays(t *testing.T, backoff_func BackoffFunc, retry_config RetryConfig, n int) []time.Duration {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	backoff := make(chan time.Duration)
	go backoff_func(ctx, backoff, retry_config)

	delays := make([]time.Duration, 0, n)
	for range n {
		delays = append(delays, <-backoff)
	}
	return delays
}

func TestBackoffSequences(t *testing.T) {
	ms := time.Millisecond
	cases := []struct {
		name     string
		config   RetryConfig
		expected []time.Duration
	}{
		{
			name:     "constant",
			config:   NewRetryConfig(WithInitialDelay(10 * ms)),
			expected: []time.Duration{10 * ms, 10 * ms, 10 * ms, 10 * ms, 10 * ms, 10 * ms},
		},
		{
			name:     "exponential",
			config:   NewRetryConfig(WithBackoffType(Exponential), WithInitialDelay(10*ms)),
			expected: []time.Duration{10 * ms, 20 * ms, 40 * ms, 80 * ms, 160 * ms, 320 * ms},
		},
		{
			name:     "exponential fractional scale",
			config:   NewRetryConfig(WithBackoffType(Exponential), WithInitialDelay(16*ms), WithDelayScale(1.5)),
			expected: []time.Duration{16 * ms, 24 * ms, 36 * ms, 54 * ms, 81 * ms, 121500 * time.Microsecond},
		},
		{
			name:     "fibonacci",
			config:   DefaultFibonacciRetryConfig(),
			expected: []time.Duration{1 * time.Second, 1 * time.Second, 2 * time.Second, 3 * time.Second, 5 * time.Second, 8 * time.Second},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := collectDelays(t, backoffMap[c.config.BackoffType], c.config, len(c.expected))
			if !slices.Equal(got, c.expected) {
				t.Errorf("Expected delays %v, got %v\n", c.expected, got)
			}
		})
	}
}

func TestExponentialBackoffNoOverflow(t *testing.T) {
	config := NewRetryConfig(WithBackoffType(Exponential), WithInitialDelay(time.Hour), WithDelayScale(10))
	for _, delay := range collectDelays(t, exponentialBackoff, config, 50) {
		if delay <= 0 {
			t.Fatalf("Delay overflowed to %v\n", delay)
		}
	}
}

func TestBackoffFromFunc(t *testing.T) {
	backoff_func := BackoffFromFunc(func(attempt int) time.Duration {
		return time.Duration(attempt+1) * time.Millisecond
	})
	got := collectDelays(t, backoff_func, RetryConfig{}, 4)
	expected := []time.Duration{time.Millisecond, 2 * time.Millisecond, 3 * time.Millisecond, 4 * time.Millisecond}
	if !slices.Equal(got, expected) {
		t.Errorf("Expected delays %v, got %v\n", expected, got)
	}
}

func TestCustomBackoff(t *testing.T) {
	delays := []time.Duration{}
	err := Retry(context.Background(), alwaysFail, NewRetryConfig(
		WithCustomBackoff(BackoffFromFunc(func(attempt int) time.Duration {
			return time.Duration(attempt) * time.Millisecond
		})),
		WithMaxRetries(3),
		WithOnRetry(func(attempt int, err error, delay time.Duration) {
			delays = append(delays, delay)
		}),
	))
	if !errors.Is(err, ErrFunctionNotCompletedMaxRetries) {
		t.Errorf("Got unexpected error %v\n", err)
	}
	expected := []time.Duration{0, time.Millisecond, 2 * time.Millisecond}
	if !slices.Equal(delays, expected) {
		t.Errorf("Expected delays %v, got %v\n", expected, delays)
	}
}

func TestCustomBackoffClosedChannel(t *testing.T) {
	calls := 0
	// gives two delays then stops
	two_delays := func(ctx context.Context, backoff chan<- time.Duration, retry_config RetryConfig) {
		defer close(backoff)
		for range 2 {
			select {
			case <-ctx.Done():
				return
			case backoff <- time.Millisecond:
			}
		}
	}
	err := Retry(context.Background(), func(ctx context.Context) error {
		calls++
		return errTest
	}, NewRetryConfig(WithCustomBackoff(two_delays), WithMaxRetries(10)))
	if !errors.Is(err, ErrFunctionNotCompletedMaxRetries) || !errors.Is(err, errTest) {
		t.Errorf("Got unexpected error %v\n", err)
	}
	if calls != 3 {
		t.Errorf("Expected 3 calls, got %d\n", calls)
	}
}

func TestCustomBackoffMissingFunc(t *testing.T) {
	err := Retry(context.Background(), alwaysFail, NewRetryConfig(WithBackoffType(Custom)))
	if err == nil {
		t.Error("Expected an error for a custom backoff without a function")
	}
}

func TestDurationCap(t *testing.T) {
	delays := []time.Duration{}
	Retry(context.Background(), alwaysFail, NewRetryConfig(
		WithBackoffType(Exponential),
		WithInitialDelay(time.Millisecond),
		WithDurationCap(4*time.Millisecond),
		WithMaxRetries(5),
		WithOnRetry(func(attempt int, err error, delay time.Duration) {
			delays = append(delays, delay)
		}),
	))
	ms := time.Millisecond
	expected := []time.Duration{1 * ms, 2 * ms, 4 * ms, 4 * ms, 4 * ms}
	if !slices.Equal(delays, expected) {
		t.Errorf("Expected delays %v, got %v\n", expected, delays)
	}
}

func TestJitterRespectsCap(t *testing.T) {
	config := NewRetryConfig(WithJitter(), WithDurationCap(10*time.Millisecond))
	for range 1000 {
		delay := config.applyJitterAndCap(10 * time.Millisecond)
		if delay < 5*time.Millisecond || delay > 10*time.Millisecond {
			t.Fatalf("Delay %v outside of [5ms, 10ms]\n", delay)
		}
	}
}

func TestRetryExternalCancellationValue(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
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
	}, NewRetryConfig(WithBackoffType(Constant), WithInitialDelay(10*time.Millisecond), WithMaxDuration(100*time.Millisecond)))
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
	}, NewRetryConfig(WithBackoffType(Exponential), WithInitialDelay(time.Millisecond), WithDelayScale(2), WithMaxRetries(5), WithJitter()))
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

func TestJitter(t *testing.T) {
	sleep_time := 2 * time.Second
	for range 1000 {
		jittered := sleep_time + time.Duration(getRandomJitter(sleep_time))
		if jittered < sleep_time/2 || jittered > sleep_time+sleep_time/2 {
			t.Fatalf("Jittered delay %v outside of [1s, 3s]\n", jittered)
		}
	}
}
