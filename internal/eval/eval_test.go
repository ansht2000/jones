package eval

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ansht2000/jones/internal/analysis"
	"github.com/ansht2000/jones/internal/llm"
)

func TestDataset(t *testing.T) {
	dataset, err := LoadDataset("../../eval/questions.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(dataset.Questions) != 20 {
		t.Errorf("Expected 20 questions, got %d\n", len(dataset.Questions))
	}

	for _, question := range dataset.Questions {
		// a phrase that's already in the question would count any answer as complete
		lower := strings.ToLower(question.Question)
		for _, group := range question.Required {
			for _, phrase := range group {
				if strings.Contains(lower, strings.ToLower(phrase)) {
					t.Errorf("%q: required phrase %q is in the question itself\n", question.Question, phrase)
				}
			}
		}
	}

	// the evidence has to exist in the version of each module being evaluated
	cache, err := exec.Command("go", "env", "GOMODCACHE").Output()
	if err != nil {
		t.Skipf("can't find the module cache: %v", err)
	}
	for _, repo := range dataset.Repos {
		module_dir := filepath.Join(strings.TrimSpace(string(cache)), repo.Module+"@"+repo.Version)
		if _, err := os.Stat(module_dir); err != nil {
			t.Logf("%s isn't downloaded, not checking its evidence", repo.Module)
			continue
		}
		for _, question := range dataset.Questions {
			if question.Repo != repo.Name {
				continue
			}
			for _, evidence := range question.Evidence {
				if _, err := os.Stat(filepath.Join(module_dir, filepath.FromSlash(evidence))); err != nil {
					t.Errorf("%q: evidence %s doesn't exist in %s@%s\n", question.Question, evidence, repo.Module, repo.Version)
				}
			}
		}
	}
}

func TestValidate(t *testing.T) {
	repos := []Repo{{Name: "demo", Module: "example.com/demo", Version: "v1.0.0"}}
	valid := Question{Repo: "demo", Question: "?", Reference: "!", Required: [][]string{{"a"}}}
	cases := map[string]Dataset{
		"unknown repo":           {Repos: repos, Questions: []Question{{Repo: "other", Question: "?", Reference: "!", Required: [][]string{{"a"}}}}},
		"no reference":           {Repos: repos, Questions: []Question{{Repo: "demo", Question: "?", Required: [][]string{{"a"}}}}},
		"nothing required":       {Repos: repos, Questions: []Question{{Repo: "demo", Question: "?", Reference: "!"}}},
		"empty group":            {Repos: repos, Questions: []Question{{Repo: "demo", Question: "?", Reference: "!", Required: [][]string{{}}}}},
		"repo without a version": {Repos: []Repo{{Name: "demo", Module: "example.com/demo"}}, Questions: []Question{valid}},
	}
	for name, dataset := range cases {
		if err := dataset.Validate(); err == nil {
			t.Errorf("%s: expected an error\n", name)
		}
	}
	if err := (&Dataset{Repos: repos, Questions: []Question{valid}}).Validate(); err != nil {
		t.Errorf("Unexpected error %v\n", err)
	}
}

var demoFiles = map[string]string{
	"main.go":        "package main\n\nimport \"demo/greet\"\n\nfunc main() {\n\tgreet.Hello()\n}\n",
	"greet/hello.go": "package greet\n\nimport \"fmt\"\n\nfunc Hello() {\n\tfmt.Println(\"hello\")\n}",
}

func makeDemo(t *testing.T) (string, *analysis.Analysis) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "demo")
	repo := &analysis.Analysis{Files: map[string]*analysis.FileAnalysis{}, Dirs: map[string]*analysis.DirAnalysis{}}
	for file, content := range demoFiles {
		file_path := filepath.Join(root, filepath.FromSlash(file))
		if err := os.MkdirAll(filepath.Dir(file_path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file_path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
		repo.Files[file] = &analysis.FileAnalysis{}
	}
	return root, repo
}

func TestScoreAnswer(t *testing.T) {
	root, repo := makeDemo(t)
	question := Question{Required: [][]string{{"Hello"}, {"goroutine", "concurrently"}, {"SIGWINCH"}}}

	// matches ignore case, and any phrase in a group counts
	score := scoreAnswer(question, "main calls hello (main.go:6), which runs CONCURRENTLY (greet/hello.go:5-7).", repo, root)
	if score.Mentioned != 2 || score.Required != 3 || score.Complete() {
		t.Errorf("Expected 2 of 3 groups mentioned, got %+v\n", score)
	}
	if score.Citations != 2 || score.BadCitations != 0 {
		t.Errorf("Expected 2 good citations, got %+v\n", score)
	}

	// a line past the end, a made up file, and a file cited by the end of its path
	score = scoreAnswer(question, "See main.go:8, src/app.go:3, and hello.go:7.", repo, root)
	if score.Citations != 3 || score.BadCitations != 2 {
		t.Errorf("Expected 3 citations with 2 bad ones, got %+v\n", score)
	}
}

func TestCountLines(t *testing.T) {
	root, _ := makeDemo(t)
	// with and without a newline at the end
	for file, expected := range map[string]int{"main.go": 7, "greet/hello.go": 7} {
		if lines, err := countLines(root, file); err != nil || lines != expected {
			t.Errorf("Expected %s to have %d lines, got %d, %v\n", file, expected, lines, err)
		}
	}
}

// answers every prompt the evaluation sends, with a baseline that cites a
// line main.go doesn't have and a judge that only accepts the agent's answer
func respond(req llm.Request) (string, error) {
	switch req.System {
	case llm.FILE_SUMMARY_PROMPT:
		return `{"purpose": "A file.", "key_symbols": [], "dependencies": []}`, nil
	case llm.DIR_SUMMARY_PROMPT:
		return "A directory.", nil
	case llm.REPO_OVERVIEW_PROMPT:
		return "A demo that says hello.", nil
	case BASELINE_PROMPT:
		return "main calls Hello (main.go:99).", nil
	case llm.DECIDE_PROMPT:
		if strings.Contains(req.Prompt, "Files you have read:\nNone yet.") {
			return `{"reason": "main.go is the entry point", "action": "read", "paths": ["main.go"]}`, nil
		}
		return `{"reason": "main.go answers it", "action": "answer"}`, nil
	case llm.ANSWER_PROMPT:
		return "main calls greet.Hello (main.go:6).", nil
	case llm.VERIFY_PROMPT:
		return `{"issues": [], "supported": true}`, nil
	case JUDGE_PROMPT:
		if strings.Contains(req.Prompt, "main.go:6") {
			return `{"reasoning": "It matches.", "correct": true}`, nil
		}
		return `{"reasoning": "It cites the wrong line.", "correct": false}`, nil
	}
	return "", errors.New("unexpected system prompt")
}

func demoDataset() *Dataset {
	return &Dataset{
		Repos: []Repo{{Name: "demo", Module: "example.com/demo", Version: "v1.0.0"}},
		Questions: []Question{
			{Repo: "demo", Question: "What does main do?", Reference: "It calls greet.Hello.", Required: [][]string{{"Hello"}}},
			{Repo: "demo", Question: "How does the program end?", Reference: "It returns from main.", Required: [][]string{{"returns"}}},
		},
	}
}

func TestRun(t *testing.T) {
	root, _ := makeDemo(t)
	mock := &llm.MockClient{Respond: respond, Latency: 5 * time.Millisecond}
	result, err := Run(context.Background(), demoDataset(), map[string]string{"demo": root}, Options{
		Client:     mock,
		WorkDir:    t.TempDir(),
		Systems:    SYSTEMS,
		Judge:      true,
		Sequential: true,
	})
	if err != nil {
		t.Fatalf("Unexpected error %v\n", err)
	}

	if len(result.Repos) != 1 || len(result.Answers) != 6 {
		t.Fatalf("Expected 1 repo and 6 answers, got %d and %d\n", len(result.Repos), len(result.Answers))
	}
	repo := result.Repos[0]
	if repo.Files != 2 || repo.Dirs != 1 || repo.SequentialRun == 0 {
		t.Errorf("Unexpected repo result %+v\n", repo)
	}
	// nothing changed, so the second analysis makes no calls
	if repo.CachedRun >= repo.FirstRun {
		t.Errorf("Expected the second analysis to be faster, got %v and %v\n", repo.CachedRun, repo.FirstRun)
	}

	summaries := result.Summaries(SYSTEMS)
	expected := []Summary{
		{System: Baseline, Answers: 2, Complete: 1, Judged: 2, Correct: 0, Citations: 2, BadCitations: 2, AnswersWithBadCites: 2},
		{System: Agent, Answers: 2, Complete: 1, Judged: 2, Correct: 2, Citations: 2},
		{System: Verified, Answers: 2, Complete: 1, Judged: 2, Correct: 2, Citations: 2, Verified: 2},
	}
	for i, summary := range summaries {
		summary.Latency = 0
		if summary != expected[i] {
			t.Errorf("Expected summary %+v, got %+v\n", expected[i], summary)
		}
	}

	report := result.Markdown(SYSTEMS, "Model: test")
	for _, part := range []string{
		"Model: test",
		"| demo | 2 | 1 |",
		"| baseline | 0/2 (0%) | 1/2 (50%) | 2 | 2 | 2/2 (100%) | - |",
		"| agent + verify | 2/2 (100%) | 1/2 (50%) | 2 | 0 | 0/2 (0%) | 2/2 (100%) |",
		"| demo | What does main do? | ✗ (1 bad) | ✓ | ✓ |",
	} {
		if !strings.Contains(report, part) {
			t.Errorf("Expected the report to contain %q, got:\n%s\n", part, report)
		}
	}
}

func TestRunRecordsFailures(t *testing.T) {
	root, _ := makeDemo(t)
	mock := &llm.MockClient{Respond: func(req llm.Request) (string, error) {
		if req.System == BASELINE_PROMPT {
			return "", errors.New("quota exceeded")
		}
		return respond(req)
	}}

	// one system failing doesn't stop the others
	result, err := Run(context.Background(), demoDataset(), map[string]string{"demo": root}, Options{Client: mock, WorkDir: t.TempDir(), Systems: SYSTEMS})
	if err != nil {
		t.Fatalf("Unexpected error %v\n", err)
	}
	summaries := result.Summaries(SYSTEMS)
	if summaries[0].Errors != 2 || summaries[2].Errors != 0 || summaries[2].Verified != 2 {
		t.Errorf("Expected only the baseline to fail, got %+v\n", summaries)
	}
	if !strings.Contains(result.Markdown(SYSTEMS, ""), "| demo | What does main do? | ! | ? | ? |") {
		t.Error("Expected failed answers to be marked in the report")
	}

	// a repo that can't be analyzed stops the evaluation
	if _, err := Run(context.Background(), demoDataset(), map[string]string{"demo": filepath.Join(root, "missing")}, Options{Client: mock, WorkDir: t.TempDir(), Systems: SYSTEMS}); err == nil {
		t.Error("Expected an error for a repo that can't be analyzed")
	}
}
