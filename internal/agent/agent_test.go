package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ansht2000/jones/internal/analysis"
	"github.com/ansht2000/jones/internal/llm"
	"github.com/ansht2000/jones/internal/retry"
)

var demoFiles = map[string]string{
	"main.go":        "package main\n\nimport \"demo/greet\"\n\nfunc main() {\n\tgreet.Hello()\n}\n",
	"greet/hello.go": "package greet\n\nimport \"fmt\"\n\n// Hello prints a greeting\nfunc Hello() {\n\tfmt.Println(\"hello\")\n}\n",
	"greet/bye.go":   "package greet\n\nfunc Bye() {}\n",
	"logo.png":       "\x89PNG\x00binary",
}

// a repo on disk and its analysis
func makeDemo(t *testing.T) (string, *analysis.Analysis) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "demo")
	for file, content := range demoFiles {
		file_path := filepath.Join(root, filepath.FromSlash(file))
		if err := os.MkdirAll(filepath.Dir(file_path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file_path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	file := func(purpose string, symbols ...string) *analysis.FileAnalysis {
		return &analysis.FileAnalysis{Summary: analysis.FileSummary{Purpose: purpose, KeySymbols: symbols}}
	}
	repo := &analysis.Analysis{
		Version:  analysis.Version,
		Name:     "demo",
		Overview: "A demo program that prints a greeting.",
		Files: map[string]*analysis.FileAnalysis{
			"main.go":        file("Entry point that calls greet.Hello.", "main"),
			"greet/hello.go": file("Prints a greeting.", "Hello"),
			"greet/bye.go":   file("Says goodbye.", "Bye"),
			"logo.png":       {Skipped: analysis.SkipBinary, Summary: analysis.FileSummary{Purpose: "Binary file."}},
		},
		Dirs: map[string]*analysis.DirAnalysis{
			"greet": {Summary: "Greeting functions. Used by main."},
		},
	}
	for file_path, record := range repo.Files {
		record.Size = int64(len(demoFiles[file_path]))
	}
	return root, repo
}

// Scripted responses for each kind of prompt, given in order, with the last
// one repeated when they run out. A response is a string or an error.
type script struct {
	mu        sync.Mutex
	responses map[string][]any
	calls     map[string]int
}

func newScript(decisions, answers, verdicts []any) *script {
	return &script{
		responses: map[string][]any{
			llm.DECIDE_PROMPT: decisions,
			llm.ANSWER_PROMPT: answers,
			llm.VERIFY_PROMPT: verdicts,
		},
		calls: map[string]int{},
	}
}

func (s *script) respond(req llm.Request) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	responses := s.responses[req.System]
	if len(responses) == 0 {
		return "", fmt.Errorf("no response scripted for system prompt %q", req.System)
	}
	response := responses[min(s.calls[req.System], len(responses)-1)]
	s.calls[req.System]++
	if err, ok := response.(error); ok {
		return "", err
	}
	return response.(string), nil
}

func readDecision(paths ...string) string {
	data, _ := json.Marshal(map[string]any{"reason": "need to see the code", "action": "read", "paths": paths})
	return string(data)
}

const (
	answerDecision = `{"reason": "the files read are enough", "action": "answer"}`
	supported      = `{"issues": [], "supported": true}`
	unsupported    = `{"issues": ["main.go:6 doesn't show what Hello prints"], "supported": false}`
	goodAnswer     = "`main` calls `greet.Hello` (main.go:6)."
)

// the prompts sent with a system prompt, in order
func promptsFor(mock *llm.MockClient, system string) []string {
	prompts := []string{}
	for _, req := range mock.Requests() {
		if req.System == system {
			prompts = append(prompts, req.Prompt)
		}
	}
	return prompts
}

// the nth prompt sent with a system prompt, failing the test if there isn't one
func nthPrompt(t *testing.T, mock *llm.MockClient, system string, n int) string {
	t.Helper()
	prompts := promptsFor(mock, system)
	if n < 0 || n >= len(prompts) {
		t.Fatalf("Expected at least %d prompts, got %d\n", n+1, len(prompts))
	}
	return prompts[n]
}

// the nth event, failing the test if there isn't one
func nthEvent(t *testing.T, events []Event, n int) Event {
	t.Helper()
	if n < 0 || n >= len(events) {
		t.Fatalf("Expected at least %d events, got %d: %+v\n", n+1, len(events), events)
	}
	return events[n]
}

// the kinds of events, with runs of answer chunks counted as one
func eventKinds(events []Event) []EventKind {
	kinds := []EventKind{}
	for _, event := range events {
		if event.Kind == EventAnswerChunk && len(kinds) > 0 && kinds[len(kinds)-1] == EventAnswerChunk {
			continue
		}
		kinds = append(kinds, event.Kind)
	}
	return kinds
}

func ask(t *testing.T, client llm.Client, repo *analysis.Analysis, root string, options Options) (*Answer, []Event) {
	t.Helper()
	events := []Event{}
	options.OnEvent = func(event Event) {
		events = append(events, event)
	}
	answer, err := Ask(context.Background(), client, repo, root, "What does main do?", options)
	if err != nil {
		t.Fatalf("Unexpected error %v\n", err)
	}
	return answer, events
}

func checkContains(t *testing.T, name, text string, parts ...string) {
	t.Helper()
	for _, part := range parts {
		if !strings.Contains(text, part) {
			t.Errorf("Expected %s to contain %q, got:\n%s\n", name, part, text)
		}
	}
}

func TestAsk(t *testing.T) {
	root, repo := makeDemo(t)
	text := "`main` calls `greet.Hello` (main.go:6), which prints hello (greet/hello.go:6-8)."
	s := newScript([]any{readDecision("main.go"), readDecision("greet/hello.go"), answerDecision}, []any{text}, []any{supported})
	mock := &llm.MockClient{Respond: s.respond}
	answer, events := ask(t, mock, repo, root, DefaultOptions())

	expected := &Answer{
		Text:      text,
		Citations: []Citation{{"main.go", 6, 6}, {"greet/hello.go", 6, 8}},
		FilesRead: []string{"main.go", "greet/hello.go"},
		Verified:  true,
	}
	if !reflect.DeepEqual(answer, expected) {
		t.Errorf("Expected answer %+v, got %+v\n", expected, answer)
	}

	decide_prompts := promptsFor(mock, llm.DECIDE_PROMPT)
	if len(decide_prompts) != 3 {
		t.Fatalf("Expected 3 decisions, got %d\n", len(decide_prompts))
	}
	checkContains(t, "the first decide prompt", decide_prompts[0],
		"Question: What does main do?",
		"A demo program that prints a greeting.",
		"greet/: Greeting functions.\n",
		"greet/hello.go: Prints a greeting. (Hello)\n",
		"Files you have read:\nNone yet.",
		"You can read files 4 more times, up to 5 at a time.")
	checkContains(t, "the second decide prompt", decide_prompts[1],
		"<file path=\"main.go\">\n1| package main\n", "6| \tgreet.Hello()\n", "3 more times")
	checkContains(t, "the answer prompt", nthPrompt(t, mock, llm.ANSWER_PROMPT, 0),
		"<file path=\"main.go\">", "<file path=\"greet/hello.go\">", "7| \tfmt.Println(\"hello\")")
	checkContains(t, "the verify prompt", nthPrompt(t, mock, llm.VERIFY_PROMPT, 0), "<answer>\n"+text+"\n</answer>")

	expected_kinds := []EventKind{EventDecided, EventRead, EventDecided, EventRead, EventDecided, EventAnswerStart, EventAnswerChunk, EventVerified}
	if kinds := eventKinds(events); !slices.Equal(kinds, expected_kinds) {
		t.Errorf("Expected events %v, got %v\n", expected_kinds, kinds)
	}
	streamed := ""
	for _, event := range events {
		streamed += event.Text
	}
	if streamed != text {
		t.Errorf("Expected the streamed chunks to add up to the answer, got %q\n", streamed)
	}
	if !slices.Equal(nthEvent(t, events, 1).Paths, []string{"main.go"}) || !slices.Equal(nthEvent(t, events, 0).Paths, []string{"main.go"}) {
		t.Errorf("Expected the first decision and read to be main.go, got %+v and %+v\n", nthEvent(t, events, 0), nthEvent(t, events, 1))
	}
}

func TestAskRejectsBadPaths(t *testing.T) {
	root, repo := makeDemo(t)
	s := newScript([]any{
		readDecision("./main.go", "src/helpers.go", "../../etc/passwd", "/etc/passwd", "logo.png", "greet", "greet/../main.go"),
		answerDecision,
	}, []any{goodAnswer}, []any{supported})
	mock := &llm.MockClient{Respond: s.respond}
	options := DefaultOptions()
	options.MaxFilesPerRound = 10
	answer, events := ask(t, mock, repo, root, options)

	if !slices.Equal(answer.FilesRead, []string{"main.go"}) {
		t.Errorf("Expected only main.go to be read, got %v\n", answer.FilesRead)
	}
	expected_problems := []string{
		"src/helpers.go doesn't exist, only paths from the file list can be read",
		"../../etc/passwd is outside the repository",
		"/etc/passwd is outside the repository",
		"logo.png can't be read (binary)",
		"greet is a directory, read the files in it instead",
	}
	if !slices.Equal(nthEvent(t, events, 1).Issues, expected_problems) {
		t.Errorf("Expected problems %q, got %q\n", expected_problems, nthEvent(t, events, 1).Issues)
	}
	// the model is told what went wrong
	checkContains(t, "the second decide prompt", nthPrompt(t, mock, llm.DECIDE_PROMPT, 1), expected_problems...)
}

func TestAskAlreadyRead(t *testing.T) {
	root, repo := makeDemo(t)
	s := newScript([]any{readDecision("main.go"), readDecision("main.go"), answerDecision}, []any{goodAnswer}, []any{supported})
	mock := &llm.MockClient{Respond: s.respond}
	answer, _ := ask(t, mock, repo, root, DefaultOptions())

	if !slices.Equal(answer.FilesRead, []string{"main.go"}) {
		t.Errorf("Expected main.go to be read once, got %v\n", answer.FilesRead)
	}
	checkContains(t, "the third decide prompt", nthPrompt(t, mock, llm.DECIDE_PROMPT, 2), "main.go was already read")
}

func TestAskReadRoundLimit(t *testing.T) {
	root, repo := makeDemo(t)
	s := newScript([]any{readDecision("main.go"), readDecision("greet/bye.go"), readDecision("greet/hello.go")}, []any{goodAnswer}, []any{supported})
	mock := &llm.MockClient{Respond: s.respond}
	options := DefaultOptions()
	options.MaxReadRounds = 2
	answer, events := ask(t, mock, repo, root, options)

	if !slices.Equal(answer.FilesRead, []string{"main.go", "greet/bye.go"}) {
		t.Errorf("Expected 2 rounds of reading, got %v\n", answer.FilesRead)
	}
	if n := len(promptsFor(mock, llm.DECIDE_PROMPT)); n != 2 {
		t.Errorf("Expected the third decision to be made without the model, got %d calls\n", n)
	}
	if nthEvent(t, events, 4).Kind != EventDecided || nthEvent(t, events, 4).Reason != "Read as many files as allowed." {
		t.Errorf("Expected a forced decision to answer, got %+v\n", nthEvent(t, events, 4))
	}
}

func TestAskFilesPerRound(t *testing.T) {
	root, repo := makeDemo(t)
	s := newScript([]any{readDecision("main.go", "greet/hello.go", "greet/bye.go"), answerDecision}, []any{goodAnswer}, []any{supported})
	options := DefaultOptions()
	options.MaxFilesPerRound = 2
	answer, _ := ask(t, &llm.MockClient{Respond: s.respond}, repo, root, options)

	if !slices.Equal(answer.FilesRead, []string{"main.go", "greet/hello.go"}) {
		t.Errorf("Expected the first 2 files to be read, got %v\n", answer.FilesRead)
	}
}

func TestAskContextLimit(t *testing.T) {
	root, repo := makeDemo(t)
	main_size := len(demoFiles["main.go"])

	// hello.go doesn't fit alongside main.go
	s := newScript([]any{readDecision("main.go", "greet/hello.go"), answerDecision}, []any{goodAnswer}, []any{supported})
	mock := &llm.MockClient{Respond: s.respond}
	options := DefaultOptions()
	options.MaxContextBytes = main_size + 10
	answer, events := ask(t, mock, repo, root, options)
	if !slices.Equal(answer.FilesRead, []string{"main.go"}) || !slices.Equal(nthEvent(t, events, 1).Issues, []string{"greet/hello.go wasn't read, there's no room left for more files"}) {
		t.Errorf("Expected only main.go to fit, got %v and %v\n", answer.FilesRead, nthEvent(t, events, 1).Issues)
	}

	// once the limit is reached the agent answers without asking the model
	s = newScript([]any{readDecision("main.go"), readDecision("greet/hello.go")}, []any{goodAnswer}, []any{supported})
	mock = &llm.MockClient{Respond: s.respond}
	options.MaxContextBytes = main_size
	ask(t, mock, repo, root, options)
	if n := len(promptsFor(mock, llm.DECIDE_PROMPT)); n != 1 {
		t.Errorf("Expected 1 decision by the model once the context was full, got %d\n", n)
	}
}

func TestAskRetriesBadCitations(t *testing.T) {
	root, repo := makeDemo(t)
	bad_answer := "It prints hello (greet/hello.go:7), see main.go:99 and src/app.go:3."
	s := newScript([]any{readDecision("main.go"), answerDecision}, []any{bad_answer, goodAnswer}, []any{supported})
	mock := &llm.MockClient{Respond: s.respond}
	answer, events := ask(t, mock, repo, root, DefaultOptions())

	if answer.Text != goodAnswer || !answer.Verified {
		t.Errorf("Expected the redone answer to be verified, got %+v\n", answer)
	}
	// the citations were wrong, so the model wasn't asked to check the claims
	if n := len(promptsFor(mock, llm.VERIFY_PROMPT)); n != 1 {
		t.Errorf("Expected 1 check by the model, got %d\n", n)
	}

	var retry_event Event
	for _, event := range events {
		if event.Kind == EventRetry {
			retry_event = event
		}
	}
	expected_issues := []string{
		"greet/hello.go:7 is cited but wasn't read, so the claim can't come from its code.",
		"main.go:99 is cited but only 7 lines of main.go were read.",
		"src/app.go is cited but isn't a file in the repository.",
	}
	if !slices.Equal(retry_event.Issues, expected_issues) {
		t.Errorf("Expected a retry with issues %q, got %+v\n", expected_issues, retry_event)
	}
	checkContains(t, "the decide prompt after the retry", nthPrompt(t, mock, llm.DECIDE_PROMPT, 2), "An earlier answer was rejected: "+expected_issues[0])
	checkContains(t, "the second answer prompt", nthPrompt(t, mock, llm.ANSWER_PROMPT, 1), append([]string{"An earlier answer was rejected because of these problems:"}, expected_issues...)...)

	starts := 0
	for _, kind := range eventKinds(events) {
		if kind == EventAnswerStart {
			starts++
		}
	}
	if starts != 2 {
		t.Errorf("Expected 2 answers to start, got %d\n", starts)
	}
}

func TestAskRetriesUnsupportedClaims(t *testing.T) {
	root, repo := makeDemo(t)
	s := newScript([]any{readDecision("main.go"), answerDecision}, []any{goodAnswer}, []any{unsupported, supported})
	mock := &llm.MockClient{Respond: s.respond}
	answer, _ := ask(t, mock, repo, root, DefaultOptions())

	if !answer.Verified || len(promptsFor(mock, llm.ANSWER_PROMPT)) != 2 || len(promptsFor(mock, llm.VERIFY_PROMPT)) != 2 {
		t.Errorf("Expected the answer to be redone once and then verified, got %+v\n", answer)
	}
}

func TestAskGivesUpAfterRetries(t *testing.T) {
	root, repo := makeDemo(t)
	s := newScript([]any{readDecision("main.go"), answerDecision}, []any{goodAnswer}, []any{unsupported})
	mock := &llm.MockClient{Respond: s.respond}
	answer, events := ask(t, mock, repo, root, DefaultOptions())

	expected_issues := []string{"main.go:6 doesn't show what Hello prints"}
	if answer.Verified || !slices.Equal(answer.Issues, expected_issues) {
		t.Errorf("Expected an unverified answer with issues %q, got %+v\n", expected_issues, answer)
	}
	if n := len(promptsFor(mock, llm.ANSWER_PROMPT)); n != 2 {
		t.Errorf("Expected 1 retry, got %d answers\n", n)
	}
	if last := nthEvent(t, events, len(events)-1); last.Kind != EventVerified || !slices.Equal(last.Issues, expected_issues) {
		t.Errorf("Expected a final verified event with the issues, got %+v\n", last)
	}
}

func TestAskWithoutModelCheck(t *testing.T) {
	root, repo := makeDemo(t)
	s := newScript([]any{readDecision("main.go"), answerDecision}, []any{goodAnswer}, nil)
	mock := &llm.MockClient{Respond: s.respond}
	options := DefaultOptions()
	options.VerifyWithModel = false
	answer, _ := ask(t, mock, repo, root, options)

	if !answer.Verified || len(promptsFor(mock, llm.VERIFY_PROMPT)) != 0 {
		t.Errorf("Expected the citations alone to verify the answer, got %+v\n", answer)
	}
}

func TestAskUncheckedAnswer(t *testing.T) {
	root, repo := makeDemo(t)
	s := newScript([]any{readDecision("main.go"), answerDecision}, []any{goodAnswer}, []any{fmt.Errorf("%w: SAFETY", llm.ErrBlocked)})
	answer, _ := ask(t, &llm.MockClient{Respond: s.respond}, repo, root, DefaultOptions())

	// the answer is still given, but not as verified
	if answer.Text != goodAnswer || answer.Verified || len(answer.Issues) != 1 || !strings.Contains(answer.Issues[0], "couldn't be checked") {
		t.Errorf("Expected an unverified answer, got %+v\n", answer)
	}
}

func TestAskDecisionProblem(t *testing.T) {
	root, repo := makeDemo(t)
	s := newScript([]any{"not json"}, []any{"I couldn't find that."}, []any{supported})
	mock := &llm.MockClient{Respond: s.respond}
	answer, _ := ask(t, mock, repo, root, DefaultOptions())

	// without a decision, the agent answers with what it has
	if len(answer.FilesRead) != 0 || answer.Text != "I couldn't find that." {
		t.Errorf("Expected an answer without reading, got %+v\n", answer)
	}
	checkContains(t, "the answer prompt", nthPrompt(t, mock, llm.ANSWER_PROMPT, 0), "Files:\nNo files were read.")
}

func TestAskAPIError(t *testing.T) {
	root, repo := makeDemo(t)
	errAPI := errors.New("API key not valid")
	s := newScript([]any{errAPI}, nil, nil)

	_, err := Ask(context.Background(), &llm.MockClient{Respond: s.respond}, repo, root, "What does main do?", DefaultOptions())
	if !errors.Is(err, errAPI) {
		t.Errorf("Expected error %v, got %v\n", errAPI, err)
	}
}

// fails its first stream partway through
type flakyStream struct {
	*llm.MockClient
	streams atomic.Int32
}

func (f *flakyStream) Stream(ctx context.Context, req llm.Request) iter.Seq2[string, error] {
	if f.streams.Add(1) == 1 {
		return func(yield func(string, error) bool) {
			if yield("partial ", nil) {
				yield("", llm.ErrStreamInterrupted)
			}
		}
	}
	return f.MockClient.Stream(ctx, req)
}

func TestAskRestartsBrokenStream(t *testing.T) {
	saved := answerRetry
	answerRetry = retry.NewRetryConfig(retry.WithInitialDelay(time.Millisecond), retry.WithMaxRetries(2), retry.WithRetryIf(saved.RetryIf))
	t.Cleanup(func() { answerRetry = saved })

	root, repo := makeDemo(t)
	s := newScript([]any{readDecision("main.go"), answerDecision}, []any{goodAnswer}, []any{supported})
	answer, events := ask(t, &flakyStream{MockClient: &llm.MockClient{Respond: s.respond}}, repo, root, DefaultOptions())

	if answer.Text != goodAnswer || !answer.Verified {
		t.Errorf("Expected the restarted answer, got %+v\n", answer)
	}
	// the second start tells the TUI to clear the partial answer
	expected_kinds := []EventKind{EventDecided, EventRead, EventDecided, EventAnswerStart, EventAnswerChunk, EventAnswerStart, EventAnswerChunk, EventVerified}
	if kinds := eventKinds(events); !slices.Equal(kinds, expected_kinds) {
		t.Errorf("Expected events %v, got %v\n", expected_kinds, kinds)
	}
}

func TestAskCancel(t *testing.T) {
	root, repo := makeDemo(t)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	_, err := Ask(ctx, &llm.MockClient{Latency: time.Hour}, repo, root, "What does main do?", DefaultOptions())
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Expected error %v, got %v\n", context.Canceled, err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("Expected cancelling to stop the agent, took %v\n", elapsed)
	}
}

func TestAskNoAnalysis(t *testing.T) {
	if _, err := Ask(context.Background(), &llm.MockClient{}, nil, t.TempDir(), "?", DefaultOptions()); !errors.Is(err, ErrNoAnalysis) {
		t.Errorf("Expected error %v, got %v\n", ErrNoAnalysis, err)
	}
}

func TestAskDoesNotFollowSymlinks(t *testing.T) {
	root, repo := makeDemo(t)
	secret := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(secret, []byte("TOP SECRET"), 0644); err != nil {
		t.Fatal(err)
	}
	// replaced after the analysis, which saw a normal file
	bye_path := filepath.Join(root, "greet", "bye.go")
	if err := os.Remove(bye_path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, bye_path); err != nil {
		t.Skipf("can't create symlinks: %v", err)
	}

	s := newScript([]any{readDecision("greet/bye.go"), answerDecision}, []any{goodAnswer}, []any{supported})
	mock := &llm.MockClient{Respond: s.respond}
	_, events := ask(t, mock, repo, root, DefaultOptions())

	if !slices.Equal(nthEvent(t, events, 1).Issues, []string{"greet/bye.go couldn't be read"}) {
		t.Errorf("Expected the symlink not to be read, got %+v\n", nthEvent(t, events, 1))
	}
	for _, req := range mock.Requests() {
		if strings.Contains(req.Prompt, "TOP SECRET") {
			t.Fatal("A file outside the repo was sent to the model through a symlink")
		}
	}
}

func TestNewReadFile(t *testing.T) {
	file := newReadFile("a.txt", []byte("one\r\ntwo\n\n"))
	if file.numbered != "1| one\n2| two\n3| \n" || file.lines != 3 || file.truncated {
		t.Errorf("Unexpected numbering %q, %d lines, truncated %v\n", file.numbered, file.lines, file.truncated)
	}

	// cut off at the last full line that fits
	line := "0123456789\n"
	file = newReadFile("big.txt", []byte(strings.Repeat(line, maxReadFileBytes/len(line)+100)))
	expected_lines := maxReadFileBytes / len(line)
	if !file.truncated || file.lines != expected_lines || !strings.HasSuffix(file.numbered, fmt.Sprintf("%d| 0123456789\n", expected_lines)) {
		t.Errorf("Expected %d full lines, got %d lines, truncated %v\n", expected_lines, file.lines, file.truncated)
	}
	if formatted := formatFiles([]*readFile{file}); !strings.Contains(formatted, fmt.Sprintf("note=\"only the first %d lines are shown\"", expected_lines)) {
		t.Error("Expected the file to be marked as cut off")
	}
}

func TestFormatFileList(t *testing.T) {
	_, repo := makeDemo(t)
	repo.Files["notes.md"] = &analysis.FileAnalysis{Summary: analysis.FileSummary{
		Purpose:    strings.Repeat("word ", 40) + "end. A second sentence.",
		KeySymbols: []string{"A", "B", "C", "D", "E"},
	}}

	expected := "greet/: Greeting functions.\n" +
		"greet/bye.go: Says goodbye. (Bye)\n" +
		"greet/hello.go: Prints a greeting. (Hello)\n" +
		"logo.png: Binary file.\n" +
		"main.go: Entry point that calls greet.Hello. (main)\n" +
		"notes.md: " + strings.Repeat("word ", 24) + "... (A, B, C, D)\n"
	if list := formatFileList(repo); list != expected {
		t.Errorf("Expected file list:\n%s\ngot:\n%s\n", expected, list)
	}
}
