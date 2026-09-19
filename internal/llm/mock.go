package llm

import (
	"context"
	"errors"
	"iter"
	"math/rand/v2"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ansht2000/jones/internal/retry"
)

var ErrMockFailure = errors.New("mock failure")

// A MockClient is a Client for tests and benchmarks. Each call takes
// Latency, fails with ErrMockFailure at FailureRate, and otherwise answers
// with Respond. Failed calls are retried according to Retry.
// It is safe for concurrent use.
type MockClient struct {
	// Returns the response to a request, GenerateJSON unmarshals it and
	// Stream splits it into words. If nil, calls return "mock response",
	// or "{}" for GenerateJSON.
	Respond func(req Request) (string, error)
	// How long each call takes
	Latency time.Duration
	// Fraction of calls that fail, from 0 to 1
	FailureRate float64
	// Retry settings for failed calls, the zero value doesn't retry
	Retry retry.RetryConfig

	calls    atomic.Int64
	mu       sync.Mutex
	requests []Request
}

// Number of calls made, including retries
func (m *MockClient) Calls() int64 {
	return m.calls.Load()
}

// Every request received, including retries, in the order they arrived
func (m *MockClient) Requests() []Request {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Request(nil), m.requests...)
}

func (m *MockClient) Generate(ctx context.Context, req Request) (string, error) {
	return m.call(ctx, req, "mock response")
}

func (m *MockClient) GenerateJSON(ctx context.Context, req Request, out any) error {
	// same checks as a real client, so bad types fail in tests too
	if err := checkOut(out); err != nil {
		return err
	}
	if _, err := SchemaFor(out); err != nil {
		return err
	}

	text, err := m.call(ctx, req, "{}")
	if err != nil {
		return err
	}
	value, err := decodeJSON(text, out)
	if err != nil {
		return err
	}
	reflect.ValueOf(out).Elem().Set(value)
	return nil
}

func (m *MockClient) Stream(ctx context.Context, req Request) iter.Seq2[string, error] {
	return func(yield func(string, error) bool) {
		text, err := m.call(ctx, req, "mock response")
		if err != nil {
			yield("", err)
			return
		}
		for _, chunk := range strings.SplitAfter(text, " ") {
			if chunk != "" && !yield(chunk, nil) {
				return
			}
		}
	}
}

// make a call with retries
func (m *MockClient) call(ctx context.Context, req Request, default_response string) (string, error) {
	return retry.RetryWithValue(ctx, func(ctx context.Context) (string, error) {
		m.calls.Add(1)
		m.mu.Lock()
		m.requests = append(m.requests, req)
		m.mu.Unlock()

		timer := time.NewTimer(m.Latency)
		select {
		case <-ctx.Done():
			timer.Stop()
			return "", context.Cause(ctx)
		case <-timer.C:
		}

		if m.FailureRate > 0 && rand.Float64() < m.FailureRate {
			return "", ErrMockFailure
		}
		if m.Respond == nil {
			return default_response, nil
		}
		return m.Respond(req)
	}, m.Retry)
}
