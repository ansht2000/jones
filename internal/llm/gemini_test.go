package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/ansht2000/jones/internal/retry"
	"google.golang.org/genai"
)

// a response from the fake Gemini API, with an empty finish reason left out
func fakeResponse(text string, finish_reason string) map[string]any {
	candidate := map[string]any{
		"content": map[string]any{
			"role":  "model",
			"parts": []any{map[string]any{"text": text}},
		},
	}
	if finish_reason != "" {
		candidate["finishReason"] = finish_reason
	}
	return map[string]any{"candidates": []any{candidate}}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(body)
}

func writeResponse(w http.ResponseWriter, text string) {
	writeJSON(w, http.StatusOK, fakeResponse(text, "STOP"))
}

// an error in the format Gemini uses, with a RetryInfo detail if retry_delay is set
func writeError(w http.ResponseWriter, code int, retry_delay string) {
	error_info := map[string]any{"code": code, "message": "fake error", "status": http.StatusText(code)}
	if retry_delay != "" {
		error_info["details"] = []any{map[string]any{
			"@type":      "type.googleapis.com/google.rpc.RetryInfo",
			"retryDelay": retry_delay,
		}}
	}
	writeJSON(w, code, map[string]any{"error": error_info})
}

// send each chunk as a server sent event
func writeStream(w http.ResponseWriter, chunks ...map[string]any) {
	w.Header().Set("Content-Type", "text/event-stream")
	for _, chunk := range chunks {
		data, _ := json.Marshal(chunk)
		fmt.Fprintf(w, "data: %s\n\n", data)
		w.(http.Flusher).Flush()
	}
}

// a client talking to a fake Gemini API served by handler, with fast retries
func newTestClient(t *testing.T, handler http.HandlerFunc, configure ...func(*GeminiConfig)) *GeminiClient {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	config := GeminiConfig{
		APIKey:  "test-key",
		Model:   "test-model",
		BaseURL: server.URL,
		Retry:   retry.NewRetryConfig(retry.WithInitialDelay(time.Millisecond), retry.WithMaxRetries(3)),
	}
	for _, c := range configure {
		c(&config)
	}

	client, err := NewGeminiClient(context.Background(), config)
	if err != nil {
		t.Fatalf("Failed to create client: %v\n", err)
	}
	return client
}

// look up a value in a decoded JSON body by its keys and array indexes
func lookup(body any, path ...any) any {
	for _, key := range path {
		switch k := key.(type) {
		case string:
			m, ok := body.(map[string]any)
			if !ok {
				return nil
			}
			body = m[k]
		case int:
			s, ok := body.([]any)
			if !ok || k >= len(s) {
				return nil
			}
			body = s[k]
		}
	}
	return body
}

func TestGenerate(t *testing.T) {
	var body map[string]any
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/models/test-model:generateContent") {
			t.Errorf("Unexpected path %s\n", r.URL.Path)
		}
		if key := r.Header.Get("x-goog-api-key"); key != "test-key" {
			t.Errorf("Expected API key test-key, got %q\n", key)
		}
		json.NewDecoder(r.Body).Decode(&body)
		writeResponse(w, "hello")
	})

	text, err := client.Generate(context.Background(), Request{
		System:          "be brief",
		Prompt:          "say hello",
		Temperature:     Ptr[float32](0.5),
		ThinkingBudget:  Ptr[int32](0),
		MaxOutputTokens: 100,
	})
	if err != nil {
		t.Fatalf("Unexpected error %v\n", err)
	}
	if text != "hello" {
		t.Errorf("Expected text hello, got %q\n", text)
	}

	checks := []struct {
		path     []any
		expected any
	}{
		{[]any{"contents", 0, "parts", 0, "text"}, "say hello"},
		{[]any{"systemInstruction", "parts", 0, "text"}, "be brief"},
		{[]any{"generationConfig", "temperature"}, 0.5},
		{[]any{"generationConfig", "maxOutputTokens"}, 100.0},
		{[]any{"generationConfig", "thinkingConfig", "thinkingBudget"}, 0.0},
	}
	for _, c := range checks {
		if got := lookup(body, c.path...); got != c.expected {
			t.Errorf("Expected request %v to be %v, got %v\n", c.path, c.expected, got)
		}
	}
}

func TestGenerateDefaultsOmitted(t *testing.T) {
	var body map[string]any
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&body)
		writeResponse(w, "hello")
	})

	if _, err := client.Generate(context.Background(), Request{Prompt: "say hello"}); err != nil {
		t.Fatalf("Unexpected error %v\n", err)
	}
	for _, path := range [][]any{
		{"systemInstruction"},
		{"generationConfig", "temperature"},
		{"generationConfig", "thinkingConfig"},
		{"generationConfig", "maxOutputTokens"},
	} {
		if got := lookup(body, path...); got != nil {
			t.Errorf("Expected %v to use the model's default, got %v\n", path, got)
		}
	}
}

func TestGenerateModelOverride(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/models/other-model:generateContent") {
			t.Errorf("Unexpected path %s\n", r.URL.Path)
		}
		writeResponse(w, "hello")
	})

	if _, err := client.Generate(context.Background(), Request{Model: "other-model", Prompt: "hi"}); err != nil {
		t.Fatalf("Unexpected error %v\n", err)
	}
}

func TestGenerateRetriesServerErrors(t *testing.T) {
	var requests atomic.Int32
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) < 3 {
			writeError(w, http.StatusServiceUnavailable, "")
			return
		}
		writeResponse(w, "hello")
	})

	text, err := client.Generate(context.Background(), Request{Prompt: "hi"})
	if err != nil || text != "hello" {
		t.Errorf("Expected hello, got %q, %v\n", text, err)
	}
	if n := requests.Load(); n != 3 {
		t.Errorf("Expected 3 requests, got %d\n", n)
	}
}

func TestGenerateHonorsRetryDelay(t *testing.T) {
	var mu sync.Mutex
	times := []time.Time{}
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		times = append(times, time.Now())
		first := len(times) == 1
		mu.Unlock()
		if first {
			writeError(w, http.StatusTooManyRequests, "0.2s")
			return
		}
		writeResponse(w, "hello")
	})

	if _, err := client.Generate(context.Background(), Request{Prompt: "hi"}); err != nil {
		t.Fatalf("Unexpected error %v\n", err)
	}
	if len(times) != 2 {
		t.Fatalf("Expected 2 requests, got %d\n", len(times))
	}
	// the backoff alone would wait 1ms
	if waited := times[1].Sub(times[0]); waited < 200*time.Millisecond {
		t.Errorf("Expected to wait the 200ms the server asked for, waited %v\n", waited)
	}
}

func TestGenerateLongRetryDelayFails(t *testing.T) {
	var requests atomic.Int32
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		writeError(w, http.StatusTooManyRequests, "3600s")
	})

	_, err := client.Generate(context.Background(), Request{Prompt: "hi"})
	var api_err genai.APIError
	if !errors.As(err, &api_err) || api_err.Code != http.StatusTooManyRequests {
		t.Errorf("Expected a 429 error, got %v\n", err)
	}
	if n := requests.Load(); n != 1 {
		t.Errorf("Expected an hour long retry delay to fail without retrying, got %d requests\n", n)
	}
}

func TestGenerateNoRetryOnClientError(t *testing.T) {
	var requests atomic.Int32
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		writeError(w, http.StatusBadRequest, "")
	})

	_, err := client.Generate(context.Background(), Request{Prompt: "hi"})
	var api_err genai.APIError
	if !errors.As(err, &api_err) || api_err.Code != http.StatusBadRequest {
		t.Errorf("Expected a 400 error, got %v\n", err)
	}
	if n := requests.Load(); n != 1 {
		t.Errorf("Expected 1 request, got %d\n", n)
	}
}

func TestGenerateGivesUp(t *testing.T) {
	var requests atomic.Int32
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		writeError(w, http.StatusInternalServerError, "")
	})

	_, err := client.Generate(context.Background(), Request{Prompt: "hi"})
	if !errors.Is(err, retry.ErrFunctionNotCompletedMaxRetries) {
		t.Errorf("Expected error %v, got %v\n", retry.ErrFunctionNotCompletedMaxRetries, err)
	}
	if n := requests.Load(); n != 4 {
		t.Errorf("Expected 1 request and 3 retries, got %d requests\n", n)
	}
}

func TestGenerateRetriesEmptyResponse(t *testing.T) {
	var requests atomic.Int32
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			writeResponse(w, "")
			return
		}
		writeResponse(w, "hello")
	})

	text, err := client.Generate(context.Background(), Request{Prompt: "hi"})
	if err != nil || text != "hello" {
		t.Errorf("Expected hello, got %q, %v\n", text, err)
	}
	if n := requests.Load(); n != 2 {
		t.Errorf("Expected 2 requests, got %d\n", n)
	}
}

func TestGenerateBlocked(t *testing.T) {
	var requests atomic.Int32
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		writeJSON(w, http.StatusOK, map[string]any{"promptFeedback": map[string]any{"blockReason": "SAFETY"}})
	})

	_, err := client.Generate(context.Background(), Request{Prompt: "hi"})
	if !errors.Is(err, ErrBlocked) {
		t.Errorf("Expected error %v, got %v\n", ErrBlocked, err)
	}
	if n := requests.Load(); n != 1 {
		t.Errorf("Expected blocked requests not to be retried, got %d requests\n", n)
	}
}

func TestGenerateTruncated(t *testing.T) {
	var requests atomic.Int32
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		writeJSON(w, http.StatusOK, fakeResponse("hel", "MAX_TOKENS"))
	})

	_, err := client.Generate(context.Background(), Request{Prompt: "hi"})
	if !errors.Is(err, ErrTruncated) {
		t.Errorf("Expected error %v, got %v\n", ErrTruncated, err)
	}
	if n := requests.Load(); n != 1 {
		t.Errorf("Expected truncated responses not to be retried, got %d requests\n", n)
	}
}

func TestGenerateAttemptTimeout(t *testing.T) {
	var requests atomic.Int32
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			// hang until the client gives up on the attempt, the body has to
			// be read for the server to notice the client disconnecting
			io.Copy(io.Discard, r.Body)
			select {
			case <-r.Context().Done():
			case <-time.After(5 * time.Second):
			}
			return
		}
		writeResponse(w, "hello")
	}, func(c *GeminiConfig) {
		c.AttemptTimeout = 50 * time.Millisecond
	})

	start := time.Now()
	text, err := client.Generate(context.Background(), Request{Prompt: "hi"})
	if err != nil || text != "hello" {
		t.Errorf("Expected hello, got %q, %v\n", text, err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("Expected the hung attempt to time out, took %v\n", elapsed)
	}
}

func TestGenerateCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var requests atomic.Int32
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		io.Copy(io.Discard, r.Body)
		cancel()
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	})

	_, err := client.Generate(ctx, Request{Prompt: "hi"})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Expected error %v, got %v\n", context.Canceled, err)
	}
	if n := requests.Load(); n != 1 {
		t.Errorf("Expected cancellation not to be retried, got %d requests\n", n)
	}
}

func TestRateLimit(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeResponse(w, "hello")
	}, func(c *GeminiConfig) {
		// one request every 50ms
		c.RequestsPerMinute = 1200
	})

	// concurrent calls share the limit
	start := time.Now()
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := client.Generate(context.Background(), Request{Prompt: "hi"}); err != nil {
				t.Errorf("Unexpected error %v\n", err)
			}
		}()
	}
	wg.Wait()

	if elapsed := time.Since(start); elapsed < 140*time.Millisecond {
		t.Errorf("Expected 4 requests to take at least 150ms, took %v\n", elapsed)
	}
}

type testDecision struct {
	Reason string   `json:"reason" description:"why this action was chosen"`
	Action string   `json:"action" enum:"read,answer"`
	Paths  []string `json:"paths,omitempty"`
}

func TestGenerateJSON(t *testing.T) {
	var body map[string]any
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&body)
		writeResponse(w, `{"reason": "need the code", "action": "read", "paths": ["main.go"]}`)
	})

	var decision testDecision
	if err := client.GenerateJSON(context.Background(), Request{Prompt: "decide"}, &decision); err != nil {
		t.Fatalf("Unexpected error %v\n", err)
	}
	expected := testDecision{Reason: "need the code", Action: "read", Paths: []string{"main.go"}}
	if !reflect.DeepEqual(decision, expected) {
		t.Errorf("Expected %+v, got %+v\n", expected, decision)
	}

	if got := lookup(body, "generationConfig", "responseMimeType"); got != "application/json" {
		t.Errorf("Expected JSON response type, got %v\n", got)
	}
	ordering, _ := json.Marshal(lookup(body, "generationConfig", "responseSchema", "propertyOrdering"))
	if string(ordering) != `["reason","action","paths"]` {
		t.Errorf("Expected schema properties in field order, got %s\n", ordering)
	}
	enum, _ := json.Marshal(lookup(body, "generationConfig", "responseSchema", "properties", "action", "enum"))
	if string(enum) != `["read","answer"]` {
		t.Errorf("Expected the action enum in the schema, got %s\n", enum)
	}
}

func TestGenerateJSONRetriesInvalidJSON(t *testing.T) {
	var requests atomic.Int32
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			writeResponse(w, `{"reason": "cut off`)
			return
		}
		writeResponse(w, `{"reason": "done", "action": "answer"}`)
	})

	var decision testDecision
	if err := client.GenerateJSON(context.Background(), Request{Prompt: "decide"}, &decision); err != nil {
		t.Fatalf("Unexpected error %v\n", err)
	}
	if decision.Action != "answer" || requests.Load() != 2 {
		t.Errorf("Expected action answer after 2 requests, got %q after %d\n", decision.Action, requests.Load())
	}
}

func TestGenerateJSONLeavesOutOnFailure(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		// decoding sets reason before failing on action's type
		writeResponse(w, `{"reason": "changed", "action": 5}`)
	})

	decision := testDecision{Reason: "unchanged"}
	err := client.GenerateJSON(context.Background(), Request{Prompt: "decide"}, &decision)
	if !errors.Is(err, ErrInvalidJSON) {
		t.Errorf("Expected error %v, got %v\n", ErrInvalidJSON, err)
	}
	if decision.Reason != "unchanged" {
		t.Errorf("Expected out to be left alone on failure, got %+v\n", decision)
	}
}

func TestGenerateJSONBadOut(t *testing.T) {
	var requests atomic.Int32
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		writeResponse(w, `{}`)
	})

	var nil_decision *testDecision
	var bad_type map[string]string
	for _, out := range []any{testDecision{}, nil_decision, nil, &bad_type} {
		if err := client.GenerateJSON(context.Background(), Request{Prompt: "decide"}, out); !errors.Is(err, ErrUnsupportedType) {
			t.Errorf("Expected error %v for %T, got %v\n", ErrUnsupportedType, out, err)
		}
	}
	if n := requests.Load(); n != 0 {
		t.Errorf("Expected no requests, got %d\n", n)
	}
}

func collectStream(client Client, req Request) ([]string, error) {
	chunks := []string{}
	for chunk, err := range client.Stream(context.Background(), req) {
		if err != nil {
			return chunks, err
		}
		chunks = append(chunks, chunk)
	}
	return chunks, nil
}

func TestStream(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/models/test-model:streamGenerateContent") || r.URL.Query().Get("alt") != "sse" {
			t.Errorf("Unexpected stream URL %s\n", r.URL)
		}
		writeStream(w, fakeResponse("Hello", ""), fakeResponse(", world", ""), fakeResponse("!", "STOP"))
	})

	chunks, err := collectStream(client, Request{Prompt: "hi"})
	if err != nil {
		t.Fatalf("Unexpected error %v\n", err)
	}
	if expected := []string{"Hello", ", world", "!"}; !slices.Equal(chunks, expected) {
		t.Errorf("Expected chunks %q, got %q\n", expected, chunks)
	}
}

func TestStreamRetriesBeforeFirstChunk(t *testing.T) {
	var requests atomic.Int32
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			writeError(w, http.StatusServiceUnavailable, "")
			return
		}
		writeStream(w, fakeResponse("Hello", ""), fakeResponse("!", "STOP"))
	})

	chunks, err := collectStream(client, Request{Prompt: "hi"})
	if err != nil {
		t.Fatalf("Unexpected error %v\n", err)
	}
	if expected := []string{"Hello", "!"}; !slices.Equal(chunks, expected) {
		t.Errorf("Expected chunks %q, got %q\n", expected, chunks)
	}
	if n := requests.Load(); n != 2 {
		t.Errorf("Expected 2 requests, got %d\n", n)
	}
}

func TestStreamInterrupted(t *testing.T) {
	var requests atomic.Int32
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		// the connection closes before a chunk with a finish reason
		writeStream(w, fakeResponse("Hello", ""))
	})

	chunks, err := collectStream(client, Request{Prompt: "hi"})
	if !errors.Is(err, ErrStreamInterrupted) {
		t.Errorf("Expected error %v, got %v\n", ErrStreamInterrupted, err)
	}
	if !slices.Equal(chunks, []string{"Hello"}) {
		t.Errorf("Expected the chunk before the failure, got %q\n", chunks)
	}
	if n := requests.Load(); n != 1 {
		t.Errorf("Expected no retry once text was streamed, got %d requests\n", n)
	}
}

func TestStreamBlocked(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeStream(w, fakeResponse("Hello", ""), fakeResponse("", "SAFETY"))
	})

	chunks, err := collectStream(client, Request{Prompt: "hi"})
	if !errors.Is(err, ErrBlocked) {
		t.Errorf("Expected error %v, got %v\n", ErrBlocked, err)
	}
	if !slices.Equal(chunks, []string{"Hello"}) {
		t.Errorf("Expected the chunk before the failure, got %q\n", chunks)
	}
}

func TestStreamCallerStops(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeStream(w, fakeResponse("Hello", ""), fakeResponse(", world", ""), fakeResponse("!", "STOP"))
	})

	chunks := []string{}
	for chunk, err := range client.Stream(context.Background(), Request{Prompt: "hi"}) {
		if err != nil {
			t.Fatalf("Unexpected error %v\n", err)
		}
		chunks = append(chunks, chunk)
		break
	}
	if !slices.Equal(chunks, []string{"Hello"}) {
		t.Errorf("Expected only the first chunk, got %q\n", chunks)
	}
}

func TestMissingAPIKey(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("GOOGLE_API_KEY", "")
	if _, err := NewGeminiClient(context.Background(), GeminiConfig{}); !errors.Is(err, ErrMissingAPIKey) {
		t.Errorf("Expected error %v, got %v\n", ErrMissingAPIKey, err)
	}
}

func TestGeminiConfigFromEnv(t *testing.T) {
	t.Setenv("JONES_MODEL", "")
	t.Setenv("JONES_RPM", "")
	config, err := GeminiConfigFromEnv()
	if err != nil || config.Model != DefaultModel || config.RequestsPerMinute != 0 {
		t.Errorf("Expected defaults, got model %q, rpm %d, error %v\n", config.Model, config.RequestsPerMinute, err)
	}

	t.Setenv("JONES_MODEL", "other-model")
	t.Setenv("JONES_RPM", "30")
	config, err = GeminiConfigFromEnv()
	if err != nil || config.Model != "other-model" || config.RequestsPerMinute != 30 {
		t.Errorf("Expected model other-model and rpm 30, got %q, %d, error %v\n", config.Model, config.RequestsPerMinute, err)
	}

	for _, rpm := range []string{"fast", "-1", "1.5"} {
		t.Setenv("JONES_RPM", rpm)
		if _, err := GeminiConfigFromEnv(); err == nil {
			t.Errorf("Expected an error for JONES_RPM %q\n", rpm)
		}
	}
}

func TestIsRetryable(t *testing.T) {
	with_delay := func(code int, delay string) error {
		return genai.APIError{Code: code, Details: []map[string]any{{
			"@type":      "type.googleapis.com/google.rpc.RetryInfo",
			"retryDelay": delay,
		}}}
	}
	connection_reset := fmt.Errorf("doRequest: error sending request: %w", &url.Error{Op: "Post", URL: "https://example.com", Err: syscall.ECONNRESET})

	cases := []struct {
		name      string
		err       error
		retryable bool
	}{
		{"rate limited", genai.APIError{Code: 429}, true},
		{"rate limited with short delay", with_delay(429, "30s"), true},
		{"rate limited with long delay", with_delay(429, "3600s"), false},
		{"server error", genai.APIError{Code: 500}, true},
		{"unavailable", genai.APIError{Code: 503}, true},
		{"gateway timeout", genai.APIError{Code: 504}, true},
		{"bad request", genai.APIError{Code: 400}, false},
		{"forbidden", genai.APIError{Code: 403}, false},
		{"model not found", genai.APIError{Code: 404}, false},
		{"wrapped server error", fmt.Errorf("call failed: %w", genai.APIError{Code: 503}), true},
		{"connection reset", connection_reset, true},
		{"attempt timed out", &url.Error{Op: "Post", URL: "https://example.com", Err: context.DeadlineExceeded}, true},
		{"cancelled", &url.Error{Op: "Post", URL: "https://example.com", Err: context.Canceled}, false},
		{"empty response", ErrEmptyResponse, true},
		{"invalid json", fmt.Errorf("%w: unexpected end", ErrInvalidJSON), true},
		{"blocked", ErrBlocked, false},
		{"truncated", ErrTruncated, false},
		{"other", errors.New("something else"), false},
	}
	for _, c := range cases {
		if got := IsRetryable(c.err); got != c.retryable {
			t.Errorf("%s: expected retryable %v, got %v\n", c.name, c.retryable, got)
		}
	}
}

func TestRetryDelay(t *testing.T) {
	retry_info := func(delay any) error {
		return genai.APIError{Code: 429, Details: []map[string]any{
			{"@type": "type.googleapis.com/google.rpc.QuotaFailure"},
			{"@type": "type.googleapis.com/google.rpc.RetryInfo", "retryDelay": delay},
		}}
	}

	cases := []struct {
		name     string
		err      error
		expected time.Duration
		ok       bool
	}{
		{"seconds", retry_info("22s"), 22 * time.Second, true},
		{"fractional seconds", retry_info("0.5s"), 500 * time.Millisecond, true},
		{"not a duration", retry_info("soon"), 0, false},
		{"not a string", retry_info(22), 0, false},
		{"no retry info", genai.APIError{Code: 429}, 0, false},
		{"not an API error", errors.New("other"), 0, false},
	}
	for _, c := range cases {
		delay, ok := RetryDelay(c.err)
		if delay != c.expected || ok != c.ok {
			t.Errorf("%s: expected %v, %v, got %v, %v\n", c.name, c.expected, c.ok, delay, ok)
		}
	}
}
