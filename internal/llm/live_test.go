package llm

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/joho/godotenv"
)

// Calls the real Gemini API with a few tiny prompts, using the key from the
// environment or .env. Skipped unless run with:
//
//	JONES_LIVE_TEST=1 go test ./internal/llm -run Live -v
func TestLiveGemini(t *testing.T) {
	if os.Getenv("JONES_LIVE_TEST") == "" {
		t.Skip("set JONES_LIVE_TEST=1 to call the real Gemini API")
	}
	godotenv.Load("../../.env")

	config, err := GeminiConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewGeminiClient(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	t.Run("generate", func(t *testing.T) {
		text, err := client.Generate(ctx, Request{
			Prompt:         "Reply with the single word: hello",
			ThinkingBudget: Ptr[int32](0),
		})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(strings.ToLower(text), "hello") {
			t.Errorf("Expected hello, got %q\n", text)
		}
	})

	t.Run("json", func(t *testing.T) {
		var decision testDecision
		err := client.GenerateJSON(ctx, Request{
			System:         DECIDE_PROMPT,
			Prompt:         "Question: what does main.go do?\n\nFile tree:\nmain.go - entry point\n\nFiles read: none",
			ThinkingBudget: Ptr[int32](0),
		}, &decision)
		if err != nil {
			t.Fatal(err)
		}
		if decision.Action != "read" && decision.Action != "answer" {
			t.Errorf("Expected action read or answer, got %+v\n", decision)
		}
		t.Logf("decision: %+v", decision)
	})

	t.Run("stream", func(t *testing.T) {
		chunks := 0
		var text strings.Builder
		for chunk, err := range client.Stream(ctx, Request{
			Prompt:         "Count from 1 to 20, separated by spaces.",
			ThinkingBudget: Ptr[int32](0),
		}) {
			if err != nil {
				t.Fatal(err)
			}
			chunks++
			text.WriteString(chunk)
		}
		if !strings.Contains(text.String(), "20") {
			t.Errorf("Expected the count to reach 20, got %q\n", text.String())
		}
		t.Logf("%d chunks: %q", chunks, text.String())
	})
}
