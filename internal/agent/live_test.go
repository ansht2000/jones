package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ansht2000/jones/internal/analysis"
	"github.com/ansht2000/jones/internal/llm"
	"github.com/joho/godotenv"
)

// Analyzes this repository and asks a question about it with the real
// Gemini API, using the key from the environment or .env. Skipped unless
// run with:
//
//	JONES_LIVE_TEST=1 go test ./internal/agent -run Live -v
func TestLiveAsk(t *testing.T) {
	if os.Getenv("JONES_LIVE_TEST") == "" {
		t.Skip("set JONES_LIVE_TEST=1 to call the real Gemini API")
	}
	godotenv.Load("../../.env")

	config, err := llm.GeminiConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	client, err := llm.NewGeminiClient(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	repo_path, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	analysis_options := analysis.DefaultOptions()
	analysis_options.SaveDir = t.TempDir()
	repo, err := analysis.Analyze(ctx, client, "jones", repo_path, analysis_options)
	if err != nil {
		t.Fatal(err)
	}

	options := DefaultOptions()
	options.OnEvent = func(event Event) {
		switch event.Kind {
		case EventDecided:
			t.Logf("decided: %s %v", event.Reason, event.Paths)
		case EventRead:
			t.Logf("read %v, problems %v", event.Paths, event.Issues)
		case EventRetry:
			t.Logf("retrying: %v", event.Issues)
		}
	}

	start := time.Now()
	answer, err := Ask(ctx, client, repo, repo_path, "How does the retry library decide how long to wait before the next attempt?", options)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("answered in %v after reading %v, verified: %v, issues: %v\n\n%s", time.Since(start), answer.FilesRead, answer.Verified, answer.Issues, answer.Text)
	if len(answer.FilesRead) == 0 || len(answer.Citations) == 0 {
		t.Error("Expected the answer to be based on files it read and cite them")
	}
}
