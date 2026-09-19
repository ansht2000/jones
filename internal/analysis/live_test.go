package analysis

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ansht2000/jones/internal/llm"
	"github.com/joho/godotenv"
)

// Analyzes this repository with the real Gemini API, using the key from the
// environment or .env. Skipped unless run with:
//
//	JONES_LIVE_TEST=1 go test ./internal/analysis -run Live -v
func TestLiveAnalyze(t *testing.T) {
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

	options := DefaultOptions()
	options.SaveDir = t.TempDir()
	options.OnProgress = func(progress Progress) {
		if progress.Done == 0 || progress.Done == progress.Total {
			t.Logf("%s: %d/%d", progress.Stage, progress.Done, progress.Total)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	start := time.Now()
	result, err := Analyze(ctx, client, "jones", repo_path, options)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("analyzed %d files and %d directories in %v", len(result.Files), len(result.Dirs), time.Since(start))
	for file_path, file := range result.Files {
		if file.Error != "" {
			t.Errorf("%s couldn't be summarized: %s\n", file_path, file.Error)
		}
	}
	t.Logf("overview:\n%s", result.Overview)

	// nothing changed, so the second run reuses everything
	start = time.Now()
	if _, err := Analyze(ctx, client, "jones", repo_path, options); err != nil {
		t.Fatal(err)
	}
	t.Logf("second run took %v", time.Since(start))
}
