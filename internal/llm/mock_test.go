package llm

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/ansht2000/jones/internal/retry"
)

func TestMockDefaults(t *testing.T) {
	mock := &MockClient{}
	ctx := context.Background()

	if text, err := mock.Generate(ctx, Request{Prompt: "a"}); err != nil || text != "mock response" {
		t.Errorf("Expected mock response, got %q, %v\n", text, err)
	}
	decision := testDecision{Reason: "old"}
	if err := mock.GenerateJSON(ctx, Request{Prompt: "b"}, &decision); err != nil || decision.Reason != "" {
		t.Errorf("Expected an empty decision, got %+v, %v\n", decision, err)
	}
	chunks, err := collectStream(mock, Request{Prompt: "c"})
	if err != nil || !slices.Equal(chunks, []string{"mock ", "response"}) {
		t.Errorf("Expected the mock response in chunks, got %q, %v\n", chunks, err)
	}

	if mock.Calls() != 3 {
		t.Errorf("Expected 3 calls, got %d\n", mock.Calls())
	}
	prompts := []string{}
	for _, req := range mock.Requests() {
		prompts = append(prompts, req.Prompt)
	}
	if !slices.Equal(prompts, []string{"a", "b", "c"}) {
		t.Errorf("Expected requests a, b, c, got %v\n", prompts)
	}
}

func TestMockRespond(t *testing.T) {
	mock := &MockClient{
		Respond: func(req Request) (string, error) {
			if req.Prompt == "decide" {
				return `{"reason": "enough read", "action": "answer"}`, nil
			}
			return "echo " + req.Prompt, nil
		},
	}

	if text, err := mock.Generate(context.Background(), Request{Prompt: "hi"}); err != nil || text != "echo hi" {
		t.Errorf("Expected echo hi, got %q, %v\n", text, err)
	}
	var decision testDecision
	if err := mock.GenerateJSON(context.Background(), Request{Prompt: "decide"}, &decision); err != nil || decision.Action != "answer" {
		t.Errorf("Expected action answer, got %+v, %v\n", decision, err)
	}
}

func TestMockInvalidJSON(t *testing.T) {
	mock := &MockClient{Respond: func(req Request) (string, error) { return "not json", nil }}
	var decision testDecision
	if err := mock.GenerateJSON(context.Background(), Request{}, &decision); !errors.Is(err, ErrInvalidJSON) {
		t.Errorf("Expected error %v, got %v\n", ErrInvalidJSON, err)
	}
	if err := mock.GenerateJSON(context.Background(), Request{}, decision); !errors.Is(err, ErrUnsupportedType) {
		t.Errorf("Expected error %v for a non pointer, got %v\n", ErrUnsupportedType, err)
	}
}

func TestMockLatency(t *testing.T) {
	mock := &MockClient{Latency: 30 * time.Millisecond}
	start := time.Now()
	mock.Generate(context.Background(), Request{})
	if elapsed := time.Since(start); elapsed < 30*time.Millisecond {
		t.Errorf("Expected the call to take at least 30ms, took %v\n", elapsed)
	}
}

func TestMockCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	_, err := (&MockClient{Latency: time.Hour}).Generate(ctx, Request{})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Expected error %v, got %v\n", context.Canceled, err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("Expected cancellation to end the call, took %v\n", elapsed)
	}
}

func TestMockFailuresRetried(t *testing.T) {
	mock := &MockClient{
		FailureRate: 1,
		Retry:       retry.NewRetryConfig(retry.WithInitialDelay(time.Millisecond), retry.WithMaxRetries(2)),
	}
	if _, err := mock.Generate(context.Background(), Request{}); !errors.Is(err, ErrMockFailure) {
		t.Errorf("Expected error %v, got %v\n", ErrMockFailure, err)
	}
	if mock.Calls() != 3 {
		t.Errorf("Expected 1 call and 2 retries, got %d calls\n", mock.Calls())
	}
}

func TestMockFailureRate(t *testing.T) {
	mock := &MockClient{FailureRate: 0.5}
	failures := 0
	for range 1000 {
		if _, err := mock.Generate(context.Background(), Request{}); err != nil {
			failures++
		}
	}
	// more than 6 standard deviations from 500
	if failures < 400 || failures > 600 {
		t.Errorf("Expected about half of 1000 calls to fail, %d failed\n", failures)
	}
}

func TestMockConcurrent(t *testing.T) {
	mock := &MockClient{Latency: time.Millisecond}
	var wg sync.WaitGroup
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			mock.Generate(context.Background(), Request{})
		}()
	}
	wg.Wait()

	if mock.Calls() != 50 || len(mock.Requests()) != 50 {
		t.Errorf("Expected 50 calls and requests, got %d and %d\n", mock.Calls(), len(mock.Requests()))
	}
}
