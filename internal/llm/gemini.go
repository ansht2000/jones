package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"strconv"
	"time"

	"github.com/ansht2000/jones/internal/retry"
	"golang.org/x/time/rate"
	"google.golang.org/genai"
)

const DefaultModel = "gemini-2.5-flash"

// Longest delay a server can ask for before a retry. Anything longer, like
// when a daily quota runs out, fails right away instead of waiting.
const maxRetryDelay = 2 * time.Minute

var ErrMissingAPIKey = errors.New("missing Gemini API key, set GEMINI_API_KEY in the environment or .env")

type GeminiConfig struct {
	// API key, if empty the GEMINI_API_KEY or GOOGLE_API_KEY environment variable is used
	APIKey string
	// Model used when a request doesn't set one
	Model string
	// Maximum requests per minute across every call made with the client,
	// including retries, 0 means no limit
	RequestsPerMinute int
	// Retry settings for each call, RetryIf and RetryAfter default to
	// IsRetryable and RetryDelay when nil
	Retry retry.RetryConfig
	// Maximum time for one attempt of Generate or GenerateJSON, 0 means no
	// limit. Streams are only limited by the caller's context.
	AttemptTimeout time.Duration
	// Overrides the API's URL, for testing
	BaseURL string
}

func DefaultGeminiConfig() GeminiConfig {
	return GeminiConfig{
		Model: DefaultModel,
		Retry: retry.NewRetryConfig(
			retry.WithBackoffType(retry.Exponential),
			retry.WithInitialDelay(time.Second),
			retry.WithDurationCap(30*time.Second),
			retry.WithMaxRetries(5),
			retry.WithJitter(),
		),
		AttemptTimeout: 2 * time.Minute,
	}
}

// The default config, with the model from JONES_MODEL and the requests
// per minute limit from JONES_RPM when they are set
func GeminiConfigFromEnv() (GeminiConfig, error) {
	config := DefaultGeminiConfig()
	if model := os.Getenv("JONES_MODEL"); model != "" {
		config.Model = model
	}
	if rpm := os.Getenv("JONES_RPM"); rpm != "" {
		requests_per_minute, err := strconv.Atoi(rpm)
		if err != nil || requests_per_minute < 0 {
			return GeminiConfig{}, fmt.Errorf("invalid JONES_RPM %q, expected a whole number of requests per minute", rpm)
		}
		config.RequestsPerMinute = requests_per_minute
	}
	return config, nil
}

// A GeminiClient calls the Gemini API. It is safe for concurrent use, and
// every call shares its rate limit.
type GeminiClient struct {
	client          *genai.Client
	model           string
	limiter         *rate.Limiter
	retry_config    retry.RetryConfig
	attempt_timeout time.Duration
}

var (
	_ Client = (*GeminiClient)(nil)
	_ Client = (*MockClient)(nil)
)

func NewGeminiClient(ctx context.Context, config GeminiConfig) (*GeminiClient, error) {
	// the SDK reads the key from the environment itself,
	// this only checks there is one to give a clearer error
	if config.APIKey == "" && os.Getenv("GEMINI_API_KEY") == "" && os.Getenv("GOOGLE_API_KEY") == "" {
		return nil, ErrMissingAPIKey
	}

	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:      config.APIKey,
		Backend:     genai.BackendGeminiAPI,
		HTTPOptions: genai.HTTPOptions{BaseURL: config.BaseURL},
	})
	if err != nil {
		return nil, err
	}

	model := config.Model
	if model == "" {
		model = DefaultModel
	}

	var limiter *rate.Limiter
	if config.RequestsPerMinute > 0 {
		// a burst of 1 spaces requests evenly across the minute
		limiter = rate.NewLimiter(rate.Every(time.Minute/time.Duration(config.RequestsPerMinute)), 1)
	}

	retry_config := config.Retry
	if retry_config.RetryIf == nil {
		retry_config.RetryIf = IsRetryable
	}
	if retry_config.RetryAfter == nil {
		retry_config.RetryAfter = RetryDelay
	}

	return &GeminiClient{
		client:          client,
		model:           model,
		limiter:         limiter,
		retry_config:    retry_config,
		attempt_timeout: config.AttemptTimeout,
	}, nil
}

func (c *GeminiClient) Generate(ctx context.Context, req Request) (string, error) {
	return retry.RetryWithValue(ctx, func(ctx context.Context) (string, error) {
		res, err := c.generate(ctx, req, nil)
		if err != nil {
			return "", err
		}
		return responseText(res)
	}, c.retry_config)
}

func (c *GeminiClient) GenerateJSON(ctx context.Context, req Request, out any) error {
	if err := checkOut(out); err != nil {
		return err
	}
	schema, err := SchemaFor(out)
	if err != nil {
		return err
	}

	// invalid JSON is retried, so each attempt decodes into a new value
	result, err := retry.RetryWithValue(ctx, func(ctx context.Context) (reflect.Value, error) {
		res, err := c.generate(ctx, req, schema)
		if err != nil {
			return reflect.Value{}, err
		}
		text, err := responseText(res)
		if err != nil {
			return reflect.Value{}, err
		}
		return decodeJSON(text, out)
	}, c.retry_config)
	if err != nil {
		return err
	}

	// out is only written on success
	reflect.ValueOf(out).Elem().Set(result)
	return nil
}

func (c *GeminiClient) Stream(ctx context.Context, req Request) iter.Seq2[string, error] {
	return func(yield func(string, error) bool) {
		model, config := c.requestConfig(req, nil)

		// only starting the stream is retried, since once text has reached
		// the caller a retry would repeat it
		stream, err := retry.RetryWithValue(ctx, func(retry_ctx context.Context) (*responseStream, error) {
			if err := c.wait(retry_ctx); err != nil {
				return nil, err
			}

			// the stream outlives this attempt, so it runs on the caller's
			// context instead of the retry's, which ends with the retry
			next, stop := iter.Pull2(c.client.Models.GenerateContentStream(ctx, model, genai.Text(req.Prompt), config))
			first, err, ok := next()
			if !ok {
				err = ErrEmptyResponse
			} else if err == nil {
				// an empty or refused first chunk is handled like a failed request
				var text string
				var done bool
				if text, done, err = streamChunk(first); err == nil && done && text == "" {
					err = ErrEmptyResponse
				}
			}
			if err != nil {
				stop()
				return nil, err
			}
			return &responseStream{first: first, next: next, stop: stop}, nil
		}, c.retry_config)
		if err != nil {
			yield("", err)
			return
		}
		defer stream.stop()

		res := stream.first
		for {
			text, done, err := streamChunk(res)
			if text != "" && !yield(text, nil) {
				return
			}
			if err != nil {
				yield("", err)
				return
			}
			if done {
				return
			}

			var ok bool
			if res, err, ok = stream.next(); !ok {
				// the SDK ends a stream quietly when its connection drops,
				// so a stream that ends without a finish reason failed
				if ctx.Err() != nil {
					yield("", context.Cause(ctx))
				} else {
					yield("", ErrStreamInterrupted)
				}
				return
			}
			if err != nil {
				yield("", err)
				return
			}
		}
	}
}

type responseStream struct {
	first *genai.GenerateContentResponse
	next  func() (*genai.GenerateContentResponse, error, bool)
	stop  func()
}

// make one attempt at a request
func (c *GeminiClient) generate(ctx context.Context, req Request, schema *genai.Schema) (*genai.GenerateContentResponse, error) {
	if err := c.wait(ctx); err != nil {
		return nil, err
	}
	if c.attempt_timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.attempt_timeout)
		defer cancel()
	}

	model, config := c.requestConfig(req, schema)
	return c.client.Models.GenerateContent(ctx, model, genai.Text(req.Prompt), config)
}

// wait until the rate limit allows another request
func (c *GeminiClient) wait(ctx context.Context) error {
	if c.limiter == nil {
		return nil
	}
	return c.limiter.Wait(ctx)
}

func (c *GeminiClient) requestConfig(req Request, schema *genai.Schema) (string, *genai.GenerateContentConfig) {
	model := req.Model
	if model == "" {
		model = c.model
	}

	config := &genai.GenerateContentConfig{
		Temperature:     req.Temperature,
		MaxOutputTokens: req.MaxOutputTokens,
	}
	if req.System != "" {
		config.SystemInstruction = &genai.Content{Parts: []*genai.Part{genai.NewPartFromText(req.System)}}
	}
	if req.ThinkingBudget != nil {
		config.ThinkingConfig = &genai.ThinkingConfig{ThinkingBudget: req.ThinkingBudget}
	}
	if schema != nil {
		config.ResponseMIMEType = "application/json"
		config.ResponseSchema = schema
	}
	return model, config
}

// the text of a finished response, or why there isn't any
func responseText(res *genai.GenerateContentResponse) (string, error) {
	text, _, err := streamChunk(res)
	if err != nil {
		return "", err
	}
	if text == "" {
		return "", ErrEmptyResponse
	}
	return text, nil
}

// the text in a response chunk, done is true once the model has finished
func streamChunk(res *genai.GenerateContentResponse) (text string, done bool, err error) {
	if res.PromptFeedback != nil && res.PromptFeedback.BlockReason != "" {
		return "", true, fmt.Errorf("%w: %s", ErrBlocked, res.PromptFeedback.BlockReason)
	}
	if len(res.Candidates) == 0 {
		return "", false, nil
	}

	text = res.Text()
	switch reason := res.Candidates[0].FinishReason; reason {
	case "":
		return text, false, nil
	case genai.FinishReasonStop:
		return text, true, nil
	case genai.FinishReasonMaxTokens:
		return text, true, ErrTruncated
	default:
		return text, true, fmt.Errorf("%w: %s", ErrBlocked, reason)
	}
}

func checkOut(out any) error {
	if value := reflect.ValueOf(out); value.Kind() != reflect.Pointer || value.IsNil() {
		return fmt.Errorf("%w: expected a non nil pointer, got %T", ErrUnsupportedType, out)
	}
	return nil
}

// unmarshal text into a new value of the type out points to
func decodeJSON(text string, out any) (reflect.Value, error) {
	value := reflect.New(reflect.TypeOf(out).Elem())
	if err := json.Unmarshal([]byte(text), value.Interface()); err != nil {
		return reflect.Value{}, fmt.Errorf("%w: %w", ErrInvalidJSON, err)
	}
	return value.Elem(), nil
}

// Reports whether a failed call is worth retrying: rate limits, server
// errors, timeouts, dropped connections, and empty or malformed responses
func IsRetryable(err error) bool {
	var api_err genai.APIError
	if errors.As(err, &api_err) {
		if delay, ok := RetryDelay(err); ok && delay > maxRetryDelay {
			return false
		}
		switch api_err.Code {
		case http.StatusRequestTimeout, http.StatusTooManyRequests, http.StatusInternalServerError,
			http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
			return true
		}
		return false
	}

	switch {
	case errors.Is(err, context.Canceled):
		return false
	case errors.Is(err, context.DeadlineExceeded),
		errors.Is(err, io.ErrUnexpectedEOF),
		errors.Is(err, ErrEmptyResponse),
		errors.Is(err, ErrInvalidJSON),
		errors.Is(err, ErrStreamInterrupted):
		return true
	}

	// the request never got a response, like when a connection is refused or reset
	var url_err *url.Error
	return errors.As(err, &url_err)
}

// Returns the delay Gemini asked for before retrying, from the RetryInfo
// detail it attaches to rate limit errors
func RetryDelay(err error) (time.Duration, bool) {
	var api_err genai.APIError
	if !errors.As(err, &api_err) {
		return 0, false
	}

	for _, detail := range api_err.Details {
		if detail["@type"] != "type.googleapis.com/google.rpc.RetryInfo" {
			continue
		}
		// a protobuf duration like "22s" or "0.5s"
		delay_str, ok := detail["retryDelay"].(string)
		if !ok {
			continue
		}
		if delay, err := time.ParseDuration(delay_str); err == nil && delay >= 0 {
			return delay, true
		}
	}
	return 0, false
}
