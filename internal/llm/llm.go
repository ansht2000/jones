package llm

import (
	"context"
	"errors"
	"iter"
)

// A Request is a single prompt to a model
type Request struct {
	// Model to use, empty uses the client's default model
	Model string
	// Instructions for the model, sent as the system prompt
	System string
	Prompt string
	// Sampling temperature, nil uses the model's default
	Temperature *float32
	// Tokens the model can spend thinking before it answers, nil uses the
	// model's default, 0 turns thinking off which lowers latency
	ThinkingBudget *int32
	// Maximum tokens in the response, 0 uses the model's default
	MaxOutputTokens int32
}

// A Client generates text with a language model. Clients retry flaky calls
// themselves, so callers only see errors that retrying didn't fix.
type Client interface {
	// Generate a text response
	Generate(ctx context.Context, req Request) (string, error)
	// Generate a JSON response and unmarshal it into out, which must be a
	// pointer. The model's output is constrained to a schema built from
	// out's type, see SchemaFor.
	GenerateJSON(ctx context.Context, req Request, out any) error
	// Stream a text response in chunks as it is generated. If the stream
	// fails, the last value has a non-nil error.
	Stream(ctx context.Context, req Request) iter.Seq2[string, error]
}

var (
	ErrEmptyResponse     = errors.New("model returned an empty response")
	ErrBlocked           = errors.New("model refused to respond")
	ErrTruncated         = errors.New("model response hit the output token limit")
	ErrInvalidJSON       = errors.New("model returned invalid JSON")
	ErrStreamInterrupted = errors.New("response stream ended before the model finished")
)

// Returns a pointer to v, for the optional fields in Request
func Ptr[T any](v T) *T {
	return &v
}
