package analysis

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ansht2000/jones/internal/llm"
)

var fixtureFiles = map[string]string{
	"README.md":                  "# Demo\nA demo project for testing.\n",
	"main.go":                    "package main\n\nfunc main() {}\n",
	"go.sum":                     "example.com/dep v1.0.0 h1:abc=\n",
	"empty.txt":                  "",
	"logo.png":                   "\x89PNG\x00\x00\x00binary",
	"internal/big.go":            strings.Repeat("// padding\n", 5000),
	"internal/util/util.go":      "package util\n\nfunc Add(a, b int) int { return a + b }\n",
	"internal/util/util_test.go": "package util\n",
	"node_modules/dep/index.js":  "module.exports = {}\n",
}

// files sent to the model, and directories below the root
var (
	fixtureSummarized = []string{"README.md", "internal/big.go", "internal/util/util.go", "internal/util/util_test.go", "main.go"}
	fixtureDirs       = []string{"internal", "internal/util"}
)

func writeFixture(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for file, content := range files {
		file_path := filepath.Join(root, filepath.FromSlash(file))
		if err := os.MkdirAll(filepath.Dir(file_path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file_path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
}

func makeFixture(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "demo")
	writeFixture(t, root, fixtureFiles)
	return root
}

// the first line of a prompt, which names what it is about
func promptSubject(prompt string) string {
	first_line, _, _ := strings.Cut(prompt, "\n")
	_, subject, _ := strings.Cut(first_line, ": ")
	return subject
}

// answers each kind of prompt with a summary naming what it is about
func respond(req llm.Request) (string, error) {
	subject := promptSubject(req.Prompt)
	switch req.System {
	case llm.FILE_SUMMARY_PROMPT:
		return fmt.Sprintf(`{"purpose": "summary of %s", "key_symbols": ["Symbol"], "dependencies": []}`, subject), nil
	case llm.DIR_SUMMARY_PROMPT:
		return "summary of " + subject, nil
	case llm.REPO_OVERVIEW_PROMPT:
		return "overview of " + subject, nil
	}
	return "", fmt.Errorf("unexpected system prompt %q", req.System)
}

// what each request was about, grouped by the kind of prompt
func requestSubjects(mock *llm.MockClient) (files, dirs, overviews []string) {
	for _, req := range mock.Requests() {
		subject := promptSubject(req.Prompt)
		switch req.System {
		case llm.FILE_SUMMARY_PROMPT:
			files = append(files, subject)
		case llm.DIR_SUMMARY_PROMPT:
			dirs = append(dirs, subject)
		case llm.REPO_OVERVIEW_PROMPT:
			overviews = append(overviews, subject)
		}
	}
	slices.Sort(files)
	slices.Sort(dirs)
	return files, dirs, overviews
}

func testOptions(t *testing.T) Options {
	return Options{SaveDir: t.TempDir(), Concurrency: 4}
}

func analyze(t *testing.T, mock *llm.MockClient, root string, options Options) *Analysis {
	t.Helper()
	result, err := Analyze(context.Background(), mock, "demo", root, options)
	if err != nil {
		t.Fatalf("Unexpected error %v\n", err)
	}
	return result
}

func TestAnalyze(t *testing.T) {
	mock := &llm.MockClient{Respond: respond}
	result := analyze(t, mock, makeFixture(t), testOptions(t))

	expected_files := []string{"README.md", "empty.txt", "go.sum", "internal/big.go", "internal/util/util.go", "internal/util/util_test.go", "logo.png", "main.go"}
	if files := slices.Sorted(maps.Keys(result.Files)); !slices.Equal(files, expected_files) {
		t.Errorf("Expected files %v, got %v\n", expected_files, files)
	}
	for _, file := range fixtureSummarized {
		record := result.Files[file]
		if record.Summary.Purpose != "summary of "+file || record.Skipped != "" || record.Error != "" {
			t.Errorf("Expected %s to be summarized, got %+v\n", file, record)
		}
		if len(record.Hash) != 64 || record.Size != int64(len(fixtureFiles[file])) {
			t.Errorf("Expected %s to have a SHA-256 hash and its size, got %q and %d\n", file, record.Hash, record.Size)
		}
	}
	for file, reason := range map[string]string{"go.sum": SkipLockfile, "empty.txt": SkipEmpty, "logo.png": SkipBinary} {
		record := result.Files[file]
		if record.Skipped != reason || record.Summary.Purpose != SKIPPED_PURPOSES[reason] {
			t.Errorf("Expected %s to be skipped as %s, got %+v\n", file, reason, record)
		}
	}

	if dirs := slices.Sorted(maps.Keys(result.Dirs)); !slices.Equal(dirs, fixtureDirs) {
		t.Errorf("Expected directories %v, got %v\n", fixtureDirs, dirs)
	}
	for _, dir := range fixtureDirs {
		if summary := result.Dirs[dir].Summary; summary != "summary of "+dir {
			t.Errorf("Expected %s to be summarized, got %q\n", dir, summary)
		}
	}
	if result.Overview != "overview of demo" || result.Stale || len(result.Hash) != 64 {
		t.Errorf("Expected an overview and hash, got %q, %q, stale %v\n", result.Overview, result.Hash, result.Stale)
	}

	files, dirs, overviews := requestSubjects(mock)
	if !slices.Equal(files, fixtureSummarized) || !slices.Equal(dirs, fixtureDirs) || len(overviews) != 1 {
		t.Errorf("Expected one call per summarized file, directory, and the overview, got %v, %v, %v\n", files, dirs, overviews)
	}
}

func TestAnalyzeBottomUp(t *testing.T) {
	mock := &llm.MockClient{Respond: respond}
	analyze(t, mock, makeFixture(t), testOptions(t))

	prompts := map[string]string{}
	order := []string{}
	for _, req := range mock.Requests() {
		if req.System != llm.FILE_SUMMARY_PROMPT {
			prompts[promptSubject(req.Prompt)] = req.Prompt
			order = append(order, promptSubject(req.Prompt))
		}
	}

	// every directory is summarized after the ones inside it, then the overview
	if expected := []string{"internal/util", "internal", "demo"}; !slices.Equal(order, expected) {
		t.Errorf("Expected summaries in order %v, got %v\n", expected, order)
	}
	for subject, parts := range map[string][]string{
		"internal/util": {"- util.go: summary of internal/util/util.go Defines Symbol.", "- util_test.go: summary of internal/util/util_test.go"},
		"internal":      {"- util/ (directory): summary of internal/util", "- big.go: summary of internal/big.go"},
		"demo":          {"- internal/ (directory): summary of internal", "- logo.png: Binary file.", "A demo project for testing."},
	} {
		for _, part := range parts {
			if !strings.Contains(prompts[subject], part) {
				t.Errorf("Expected the prompt for %s to contain %q, got:\n%s\n", subject, part, prompts[subject])
			}
		}
	}
}

func TestAnalyzeTruncatesLargeFiles(t *testing.T) {
	mock := &llm.MockClient{Respond: respond}
	analyze(t, mock, makeFixture(t), testOptions(t))

	for _, req := range mock.Requests() {
		if promptSubject(req.Prompt) != "internal/big.go" {
			continue
		}
		size := len(fixtureFiles["internal/big.go"])
		if note := fmt.Sprintf("Only the first %d of the file's %d bytes are shown.", maxFileBytes, size); !strings.Contains(req.Prompt, note) {
			t.Errorf("Expected the prompt to say the file was cut off\n")
		}
		if len(req.Prompt) > maxFileBytes+500 {
			t.Errorf("Expected the prompt to be cut to about %d bytes, got %d\n", maxFileBytes, len(req.Prompt))
		}
		return
	}
	t.Error("internal/big.go was not summarized")
}

func TestAnalyzeReusesSavedAnalysis(t *testing.T) {
	root := makeFixture(t)
	options := testOptions(t)
	first := analyze(t, &llm.MockClient{Respond: respond}, root, options)

	saved, err := LoadAnalysis(options.SaveDir, "demo")
	if err != nil || !reflect.DeepEqual(saved, first) {
		t.Fatalf("Expected the analysis to be saved, got %v\n", err)
	}

	// nothing changed, so nothing is sent to the model
	mock := &llm.MockClient{Respond: respond}
	if second := analyze(t, mock, root, options); !reflect.DeepEqual(second, first) {
		t.Errorf("Expected the same analysis when nothing changed\n")
	}
	if mock.Calls() != 0 {
		t.Errorf("Expected no calls when nothing changed, got %d\n", mock.Calls())
	}

	// only the changed file and the directories above it are redone
	writeFixture(t, root, map[string]string{"internal/util/util.go": "package util\n\nfunc Sub(a, b int) int { return a - b }\n"})
	mock = &llm.MockClient{Respond: respond}
	third := analyze(t, mock, root, options)
	files, dirs, overviews := requestSubjects(mock)
	if !slices.Equal(files, []string{"internal/util/util.go"}) || !slices.Equal(dirs, fixtureDirs) || len(overviews) != 1 {
		t.Errorf("Expected only util.go and its parents to be redone, got %v, %v, %v\n", files, dirs, overviews)
	}
	if third.Files["internal/util/util.go"].Hash == first.Files["internal/util/util.go"].Hash || third.Hash == first.Hash {
		t.Error("Expected the changed file and the repo to get new hashes")
	}
	if third.Files["main.go"] != nil && third.Files["main.go"].Hash != first.Files["main.go"].Hash {
		t.Error("Expected unchanged files to keep their hashes")
	}
}

func TestAnalyzeWithoutSaving(t *testing.T) {
	root := makeFixture(t)
	for range 2 {
		mock := &llm.MockClient{Respond: respond}
		analyze(t, mock, root, Options{Concurrency: 4})
		if mock.Calls() != 8 {
			t.Errorf("Expected every run to start from scratch without a save directory, got %d calls\n", mock.Calls())
		}
	}
}

func TestAnalyzeFileProblem(t *testing.T) {
	root := makeFixture(t)
	options := testOptions(t)
	blocked := &llm.MockClient{Respond: func(req llm.Request) (string, error) {
		if promptSubject(req.Prompt) == "main.go" {
			return "", fmt.Errorf("%w: RECITATION", llm.ErrBlocked)
		}
		return respond(req)
	}}

	// one file failing doesn't stop the analysis
	result := analyze(t, blocked, root, options)
	record := result.Files["main.go"]
	if record.Summary.Purpose != failedSummary || !strings.Contains(record.Error, "RECITATION") {
		t.Errorf("Expected main.go to be marked as failed, got %+v\n", record)
	}
	if !result.Stale || result.Dirs["internal"].Stale {
		t.Errorf("Expected only the repo, which holds main.go, to be stale\n")
	}

	// the failed file and the overview built on it are retried on the next run
	mock := &llm.MockClient{Respond: respond}
	result = analyze(t, mock, root, options)
	files, dirs, overviews := requestSubjects(mock)
	if !slices.Equal(files, []string{"main.go"}) || len(dirs) != 0 || len(overviews) != 1 {
		t.Errorf("Expected main.go and the overview to be retried, got %v, %v, %v\n", files, dirs, overviews)
	}
	if result.Files["main.go"].Error != "" || result.Stale {
		t.Errorf("Expected the retry to fix main.go\n")
	}
}

func TestAnalyzeDirProblem(t *testing.T) {
	root := makeFixture(t)
	options := testOptions(t)
	blocked := &llm.MockClient{Respond: func(req llm.Request) (string, error) {
		if req.System == llm.DIR_SUMMARY_PROMPT && promptSubject(req.Prompt) == "internal/util" {
			return "", llm.ErrEmptyResponse
		}
		return respond(req)
	}}

	result := analyze(t, blocked, root, options)
	if !result.Dirs["internal/util"].Stale || !result.Dirs["internal"].Stale || !result.Stale {
		t.Errorf("Expected the failed directory and everything above it to be stale\n")
	}

	mock := &llm.MockClient{Respond: respond}
	analyze(t, mock, root, options)
	files, dirs, overviews := requestSubjects(mock)
	if len(files) != 0 || !slices.Equal(dirs, fixtureDirs) || len(overviews) != 1 {
		t.Errorf("Expected the stale directories and overview to be redone, got %v, %v, %v\n", files, dirs, overviews)
	}
}

func TestAnalyzeFailsOnAPIErrors(t *testing.T) {
	errAPI := errors.New("API key not valid")
	options := testOptions(t)
	mock := &llm.MockClient{Respond: func(req llm.Request) (string, error) {
		return "", errAPI
	}}

	_, err := Analyze(context.Background(), mock, "demo", makeFixture(t), options)
	if !errors.Is(err, errAPI) {
		t.Errorf("Expected error %v, got %v\n", errAPI, err)
	}
	if _, err := LoadAnalysis(options.SaveDir, "demo"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Expected nothing to be saved after a failure, got %v\n", err)
	}
}

func TestAnalyzeTooManyFiles(t *testing.T) {
	mock := &llm.MockClient{Respond: respond}
	options := testOptions(t)
	options.MaxFiles = 4

	_, err := Analyze(context.Background(), mock, "demo", makeFixture(t), options)
	if !errors.Is(err, ErrTooManyFiles) {
		t.Errorf("Expected error %v, got %v\n", ErrTooManyFiles, err)
	}
	if mock.Calls() != 0 {
		t.Errorf("Expected no calls, got %d\n", mock.Calls())
	}
}

func TestAnalyzeProgress(t *testing.T) {
	events := []Progress{}
	options := testOptions(t)
	options.OnProgress = func(progress Progress) {
		events = append(events, progress)
	}
	analyze(t, &llm.MockClient{Respond: respond}, makeFixture(t), options)

	by_stage := map[Stage][]Progress{}
	stages := []Stage{}
	for _, event := range events {
		if len(by_stage[event.Stage]) == 0 {
			stages = append(stages, event.Stage)
		}
		by_stage[event.Stage] = append(by_stage[event.Stage], event)
	}
	if expected := []Stage{StageScan, StageFiles, StageDirs, StageOverview}; !slices.Equal(stages, expected) {
		t.Errorf("Expected stages %v, got %v\n", expected, stages)
	}

	for stage, total := range map[Stage]int{StageScan: 8, StageFiles: 5, StageDirs: 2, StageOverview: 1} {
		stage_events := by_stage[stage]
		if len(stage_events) != total+1 {
			t.Errorf("%s: expected a start event and %d item events, got %v\n", stage, total, stage_events)
			continue
		}
		// reports never overlap, so they arrive in order
		for i, event := range stage_events {
			if event.Done != i || event.Total != total || (i > 0 && stage != StageOverview && event.Path == "") {
				t.Errorf("%s: unexpected event %d: %+v\n", stage, i, event)
			}
		}
	}
}

func TestAnalyzeParallel(t *testing.T) {
	// the file calls wait for each other, which only works if all 5 run at once
	var started atomic.Int32
	all_started := make(chan struct{})
	mock := &llm.MockClient{Respond: func(req llm.Request) (string, error) {
		if req.System == llm.FILE_SUMMARY_PROMPT {
			if started.Add(1) == 5 {
				close(all_started)
			}
			select {
			case <-all_started:
			case <-time.After(2 * time.Second):
				return "", errors.New("file summaries did not run in parallel")
			}
		}
		return respond(req)
	}}

	options := testOptions(t)
	options.Concurrency = 5
	analyze(t, mock, makeFixture(t), options)
}

func TestAnalyzeCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	_, err := Analyze(ctx, &llm.MockClient{Latency: time.Hour}, "demo", makeFixture(t), testOptions(t))
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Expected error %v, got %v\n", context.Canceled, err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("Expected cancelling to stop the analysis, took %v\n", elapsed)
	}
}

func TestAnalyzeSkipsSymlinks(t *testing.T) {
	secret := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(secret, []byte("TOP SECRET"), 0644); err != nil {
		t.Fatal(err)
	}
	root := makeFixture(t)
	if err := os.Symlink(secret, filepath.Join(root, "link.txt")); err != nil {
		t.Skipf("can't create symlinks: %v", err)
	}

	var mu sync.Mutex
	leaked := false
	mock := &llm.MockClient{Respond: func(req llm.Request) (string, error) {
		mu.Lock()
		leaked = leaked || strings.Contains(req.Prompt, "TOP SECRET")
		mu.Unlock()
		return respond(req)
	}}
	result := analyze(t, mock, root, testOptions(t))

	if leaked {
		t.Error("A file outside the repo was sent to the model through a symlink")
	}
	if record := result.Files["link.txt"]; record == nil || record.Skipped != SkipSymlink {
		t.Errorf("Expected the symlink to be skipped, got %+v\n", record)
	}
}

func TestAnalyzeMissingRepo(t *testing.T) {
	_, err := Analyze(context.Background(), &llm.MockClient{}, "demo", filepath.Join(t.TempDir(), "missing"), testOptions(t))
	if err == nil {
		t.Error("Expected an error for a missing repo")
	}
}

func TestAnalyzeInvalidName(t *testing.T) {
	for _, name := range []string{"", ".", "..", "../demo", `a\b`} {
		if _, err := Analyze(context.Background(), &llm.MockClient{}, name, t.TempDir(), testOptions(t)); !errors.Is(err, ErrInvalidRepoName) {
			t.Errorf("Expected error %v for name %q, got %v\n", ErrInvalidRepoName, name, err)
		}
	}
}

func TestLoadAnalysisOutdated(t *testing.T) {
	save_dir := t.TempDir()
	if err := os.WriteFile(savePath(save_dir, "demo"), []byte(`{"version": 0}`), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadAnalysis(save_dir, "demo"); !errors.Is(err, ErrOutdated) {
		t.Errorf("Expected error %v, got %v\n", ErrOutdated, err)
	}
}

func TestAnalyzeEmptyDirectory(t *testing.T) {
	root := makeFixture(t)
	if err := os.MkdirAll(filepath.Join(root, "internal", "nothing"), 0755); err != nil {
		t.Fatal(err)
	}
	mock := &llm.MockClient{Respond: respond}
	result := analyze(t, mock, root, testOptions(t))

	if summary := result.Dirs["internal/nothing"].Summary; summary != "Empty directory." {
		t.Errorf("Expected an empty directory placeholder, got %q\n", summary)
	}
	if _, dirs, _ := requestSubjects(mock); !slices.Equal(dirs, fixtureDirs) {
		t.Errorf("Expected no call for the empty directory, got %v\n", dirs)
	}
}
