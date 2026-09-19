package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ansht2000/jones/internal/agent"
	"github.com/ansht2000/jones/internal/analysis"
	"github.com/ansht2000/jones/internal/llm"
	"github.com/ansht2000/jones/internal/repo"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var demoFiles = map[string]string{
	"main.go":        "package main\n\nimport \"demo/greet\"\n\nfunc main() {\n\tgreet.Hello()\n}\n",
	"greet/hello.go": "package greet\n\nimport \"fmt\"\n\nfunc Hello() {\n\tfmt.Println(\"hello\")\n}\n",
}

// a repo manager in a temporary directory, with a demo repo cloned into it
func newTestManager(t *testing.T) *repo.RepoManager {
	t.Helper()
	root := t.TempDir()
	manager := repo.NewRepoManager(filepath.Join(root, "repos"), filepath.Join(root, "trees"), filepath.Join(root, "analysis"))
	for _, dir := range []string{manager.Root, manager.Tree, manager.Analysis} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	for file, content := range demoFiles {
		file_path := filepath.Join(manager.Root, "demo", filepath.FromSlash(file))
		if err := os.MkdirAll(filepath.Dir(file_path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file_path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return manager
}

// a model past the splash screen in a 100x30 terminal, with no client if client is nil
func newTestModel(t *testing.T, manager *repo.RepoManager, client llm.Client) Model {
	t.Helper()
	var client_err error
	if client == nil {
		client_err = llm.ErrMissingAPIKey
	}
	m := initialModel(manager, client, "test-model", client_err)
	m, _ = update(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m, _ = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	return m
}

func update(t *testing.T, m Model, msg tea.Msg) (Model, tea.Cmd) {
	t.Helper()
	updated, cmd := m.Update(msg)
	return updated.(Model), cmd
}

// type a line, press enter, and handle the command it sends
func submit(t *testing.T, m Model, input string) (Model, tea.Cmd) {
	t.Helper()
	m.text_input.SetValue(input)
	m, cmd := update(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		return m, nil
	}
	return update(t, m, cmd())
}

// Run a command, failing the test if it doesn't return soon, like when
// waiting on a task that never finishes
func runCmd(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	result := make(chan tea.Msg, 1)
	go func() {
		result <- cmd()
	}()
	select {
	case msg := <-result:
		return msg
	case <-time.After(5 * time.Second):
		t.Fatal("Timed out waiting for the task's next message")
		return nil
	}
}

// Run commands and feed the messages they produce back into the model until
// the task finishes. Spinner ticks are skipped, since they never stop.
func runTask(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	cmds := []tea.Cmd{cmd}
	for len(cmds) > 0 {
		next := cmds[0]
		cmds = cmds[1:]
		if next == nil {
			continue
		}
		switch msg := runCmd(t, next).(type) {
		case tea.BatchMsg:
			cmds = append(cmds, msg...)
		case spinner.TickMsg, nil:
		default:
			var cmd tea.Cmd
			m, cmd = update(t, m, msg)
			cmds = append(cmds, cmd)
		}
	}
	if m.task != nil {
		t.Fatalf("Expected the task %q to be finished\n", m.task.status)
	}
	return m
}

// the transcript's text, one entry per line
func transcriptText(m Model) string {
	var text strings.Builder
	for _, e := range m.transcript {
		text.WriteString(e.text);text.WriteString("\n")
	}
	return text.String()
}

func lastEntry(t *testing.T, m Model) entry {
	t.Helper()
	if len(m.transcript) == 0 {
		t.Fatal("Expected the transcript to have entries")
	}
	return m.transcript[len(m.transcript)-1]
}

func checkContains(t *testing.T, name, text string, parts ...string) {
	t.Helper()
	for _, part := range parts {
		if !strings.Contains(text, part) {
			t.Errorf("Expected %s to contain %q, got:\n%s\n", name, part, text)
		}
	}
}

// answers every kind of prompt jones sends, reading main.go before answering
func demoResponder() func(llm.Request) (string, error) {
	var mu sync.Mutex
	decisions := 0
	return func(req llm.Request) (string, error) {
		switch req.System {
		case llm.FILE_SUMMARY_PROMPT:
			return `{"purpose": "A file.", "key_symbols": [], "dependencies": []}`, nil
		case llm.DIR_SUMMARY_PROMPT:
			return "A directory.", nil
		case llm.REPO_OVERVIEW_PROMPT:
			return "A demo that prints hello.", nil
		case llm.DECIDE_PROMPT:
			mu.Lock()
			defer mu.Unlock()
			decisions++
			if decisions == 1 {
				return `{"reason": "main.go is the entry point", "action": "read", "paths": ["main.go"]}`, nil
			}
			return `{"reason": "main.go answers it", "action": "answer"}`, nil
		case llm.ANSWER_PROMPT:
			return "main calls greet.Hello (main.go:6).", nil
		case llm.VERIFY_PROMPT:
			return `{"issues": [], "supported": true}`, nil
		}
		return "", fmt.Errorf("unexpected system prompt %q", req.System)
	}
}

func TestSplashScreen(t *testing.T) {
	m := initialModel(newTestManager(t), nil, "test-model", llm.ErrMissingAPIKey)
	checkContains(t, "the splash screen", m.View(), "Press any key to continue...")

	started, _ := update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	if started.state != Accepting {
		t.Errorf("Expected any key to leave the splash screen")
	}
	if _, cmd := update(t, m, tea.KeyMsg{Type: tea.KeyCtrlC}); cmd == nil || cmd() != tea.Quit() {
		t.Errorf("Expected ctrl+c to quit from the splash screen")
	}
}

func TestHelp(t *testing.T) {
	m, _ := submit(t, newTestModel(t, newTestManager(t), nil), "help")
	help := lastEntry(t, m).text
	for _, command := range getCommands() {
		checkContains(t, "help", help, command.name+": "+command.description)
	}
	checkContains(t, "help", help, "esc cancels what's running")
}

func TestUnknownCommand(t *testing.T) {
	m, _ := submit(t, newTestModel(t, newTestManager(t), nil), "frobnicate the repo")
	if last := lastEntry(t, m); last.kind != entryError || !strings.Contains(last.text, `Unknown command "frobnicate"`) {
		t.Errorf("Expected an unknown command error, got %+v\n", last)
	}
	// with no repo in use, a question isn't sent anywhere
	m, cmd := submit(t, m, "ask what does main do?")
	if cmd != nil || !strings.Contains(lastEntry(t, m).text, "Pick a repo") {
		t.Errorf("Expected asking without a repo to fail, got %+v\n", lastEntry(t, m))
	}
}

func TestList(t *testing.T) {
	manager := newTestManager(t)
	if err := os.Mkdir(filepath.Join(manager.Root, "other"), 0755); err != nil {
		t.Fatal(err)
	}
	// files in the repo root aren't repos
	if err := os.WriteFile(filepath.Join(manager.Root, "notes.txt"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(analysis.SavePath(manager.Analysis, "demo"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	m, _ := submit(t, newTestModel(t, manager, nil), "list")
	if list := lastEntry(t, m).text; list != "Available repos:\n\ndemo (analyzed)\nother" {
		t.Errorf("Unexpected repo list:\n%s\n", list)
	}
}

func TestUse(t *testing.T) {
	manager := newTestManager(t)
	m := newTestModel(t, manager, nil)

	m, _ = submit(t, m, "use missing")
	checkContains(t, "the reply to using a missing repo", lastEntry(t, m).text, "Could not find repo missing")
	m, _ = submit(t, m, "use demo")
	checkContains(t, "the reply to using an unanalyzed repo", lastEntry(t, m).text, "demo hasn't been analyzed yet")
	if m.active_repo != "" {
		t.Errorf("Expected an unanalyzed repo not to be used")
	}

	if err := os.WriteFile(analysis.SavePath(manager.Analysis, "demo"), []byte(`{"version": 0}`), 0644); err != nil {
		t.Fatal(err)
	}
	m, _ = submit(t, m, "use demo")
	checkContains(t, "the reply to using an outdated analysis", lastEntry(t, m).text, "older version of jones")

	saved := fmt.Sprintf(`{"version": %d, "name": "demo", "overview": "A demo that prints hello."}`, analysis.Version)
	if err := os.WriteFile(analysis.SavePath(manager.Analysis, "demo"), []byte(saved), 0644); err != nil {
		t.Fatal(err)
	}
	m, _ = submit(t, m, "use demo")
	if m.active_repo != "demo" || lastEntry(t, m).text != "A demo that prints hello." {
		t.Errorf("Expected demo to be in use and its overview shown, got %+v\n", lastEntry(t, m))
	}
	checkContains(t, "the status bar", m.View(), "using demo")
}

func TestAnalyzeAndAsk(t *testing.T) {
	manager := newTestManager(t)
	m := newTestModel(t, manager, &llm.MockClient{Respond: demoResponder()})

	m, cmd := submit(t, m, "analyze demo")
	if m.task == nil || cmd == nil {
		t.Fatal("Expected analyze to start a task")
	}
	checkContains(t, "the view while analyzing", m.View(), "Analyzing demo", "esc cancel")

	m = runTask(t, m, cmd)
	if m.analyses["demo"] == nil || m.active_repo != "demo" {
		t.Fatal("Expected the analysis to be kept and demo to be in use")
	}
	checkContains(t, "the transcript", transcriptText(m), "Analyzed demo in", "2 files", "A demo that prints hello.")
	if _, err := os.Stat(analysis.SavePath(manager.Analysis, "demo")); err != nil {
		t.Errorf("Expected the analysis to be saved, got %v\n", err)
	}

	// anything that isn't a command is a question
	before := len(m.transcript)
	m, cmd = submit(t, m, "What does main do?")
	if m.task == nil {
		t.Fatal("Expected the question to start a task")
	}
	m = runTask(t, m, cmd)

	answer_entries := m.transcript[before:]
	text := transcriptText(Model{transcript: answer_entries})
	checkContains(t, "the answer", text, "Reading main.go: main.go is the entry point", "Answering: main.go answers it", "✓ Verified against main.go:6")
	if strings.Count(text, "main calls greet.Hello (main.go:6).") != 1 {
		t.Errorf("Expected the answer to be shown once, got:\n%s\n", text)
	}
	if m.live != "" {
		t.Errorf("Expected the streamed answer to be moved into the transcript")
	}
}

func TestNoClient(t *testing.T) {
	m := newTestModel(t, newTestManager(t), nil)
	checkContains(t, "the startup message", transcriptText(m), "Analyzing repos and asking questions are turned off")
	checkContains(t, "the status bar", m.View(), "no API key")

	m, cmd := submit(t, m, "analyze demo")
	if cmd != nil || !strings.Contains(lastEntry(t, m).text, "Can't analyze without an API key: "+llm.ErrMissingAPIKey.Error()) {
		t.Errorf("Expected analyze to fail without a client, got %+v\n", lastEntry(t, m))
	}
}

func TestCancelTask(t *testing.T) {
	manager := newTestManager(t)
	m := newTestModel(t, manager, &llm.MockClient{Latency: time.Hour})
	m, cmd := submit(t, m, "analyze demo")

	// only one task runs at a time
	m, second := submit(t, m, "analyze demo")
	if second != nil || !strings.Contains(lastEntry(t, m).text, `Still busy with "Analyzing demo"`) {
		t.Errorf("Expected a second task to be refused, got %+v\n", lastEntry(t, m))
	}

	m, _ = update(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	checkContains(t, "the view while cancelling", m.View(), "Cancelling")
	m = runTask(t, m, cmd)
	if last := lastEntry(t, m); last.kind != entryWarning || last.text != "Cancelled." {
		t.Errorf("Expected the task to be cancelled, got %+v\n", last)
	}
	if m.analyses["demo"] != nil {
		t.Errorf("Expected a cancelled analysis not to be kept")
	}
}

func TestCtrlCStopsTask(t *testing.T) {
	m := newTestModel(t, newTestManager(t), &llm.MockClient{Latency: time.Hour})
	m, task_cmd := submit(t, m, "analyze demo")

	m, cmd := update(t, m, tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil || cmd() != tea.Quit() {
		t.Errorf("Expected ctrl+c to quit")
	}
	// the task was cancelled rather than left running
	m = runTask(t, m, task_cmd)
	if lastEntry(t, m).text != "Cancelled." {
		t.Errorf("Expected ctrl+c to cancel the task, got %+v\n", lastEntry(t, m))
	}
}

// a model with a running task that tests send messages to directly
func withTask(m Model) Model {
	m.task = &task{status: "Thinking", cancel: func() {}, events: make(chan tea.Msg, 1)}
	return m
}

func TestStreamingAnswer(t *testing.T) {
	m := withTask(newTestModel(t, newTestManager(t), nil))
	for _, event := range []agent.Event{
		{Kind: agent.EventAnswerStart},
		{Kind: agent.EventAnswerChunk, Text: "main calls "},
		{Kind: agent.EventAnswerChunk, Text: "greet.Hello"},
	} {
		m, _ = update(t, m, agentEventMsg{event: event})
	}
	if m.live != "main calls greet.Hello" {
		t.Errorf("Expected the chunks to be joined, got %q\n", m.live)
	}
	checkContains(t, "the view", m.View(), "main calls greet.Hello", "Writing the answer")

	m, _ = update(t, m, agentEventMsg{event: agent.Event{Kind: agent.EventChecking}})
	checkContains(t, "the view", m.View(), "Checking the answer against the code")

	// a rejected answer is replaced by the next one
	m, _ = update(t, m, agentEventMsg{event: agent.Event{Kind: agent.EventRetry, Issues: []string{"main.go:99 is cited but only 7 lines of main.go were read."}}})
	if m.live != "" || !strings.Contains(lastEntry(t, m).text, "- main.go:99 is cited") {
		t.Errorf("Expected the rejected answer to be cleared and the issues shown, got %q and %+v\n", m.live, lastEntry(t, m))
	}
}

func TestProgressLine(t *testing.T) {
	m := withTask(newTestModel(t, newTestManager(t), nil))
	m, cmd := update(t, m, progressMsg{progress: analysis.Progress{Stage: analysis.StageFiles, Done: 3, Total: 10, Path: "main.go"}})
	if cmd == nil {
		t.Error("Expected to keep waiting for the task's next message")
	}
	checkContains(t, "the task line", m.taskLine(), "Summarizing files 3/10", "30%")
}

func TestRetryNotice(t *testing.T) {
	m := withTask(newTestModel(t, newTestManager(t), nil))
	m, cmd := update(t, m, retryMsg{attempt: 1, delay: 22 * time.Second})
	// retries are sent from outside the task, so they must not start a second wait
	if cmd != nil {
		t.Error("Expected no command for a retry message")
	}
	checkContains(t, "the task line", m.taskLine(), "retrying in 22s")

	m, _ = update(t, m, agentEventMsg{event: agent.Event{Kind: agent.EventDecided, Reason: "need code", Paths: []string{"main.go"}}})
	if strings.Contains(m.taskLine(), "retrying") {
		t.Error("Expected the notice to clear once the task moves on")
	}
}

func TestResize(t *testing.T) {
	m := newTestModel(t, newTestManager(t), nil)
	m.addEntry(entryOutput, strings.Repeat("word ", 50))
	m, _ = update(t, m, tea.WindowSizeMsg{Width: 40, Height: 12})

	if m.viewport.Width != 40 || m.viewport.Height != 9 {
		t.Errorf("Expected a 40x9 viewport, got %dx%d\n", m.viewport.Width, m.viewport.Height)
	}
	view := m.View()
	for _, line := range strings.Split(view, "\n") {
		if width := lipgloss.Width(line); width > 40 {
			t.Errorf("Expected lines to fit in 40 columns, got %d: %q\n", width, line)
		}
	}
	if lines := strings.Count(view, "\n") + 1; lines != 12 {
		t.Errorf("Expected the view to fill 12 lines, got %d\n", lines)
	}

	// shrinking the window keeps the latest output in view
	for i := range 40 {
		m.addEntry(entryOutput, fmt.Sprintf("line %d", i))
	}
	m, _ = update(t, m, tea.WindowSizeMsg{Width: 30, Height: 8})
	if !m.viewport.AtBottom() || !strings.Contains(m.View(), "line 39") {
		t.Errorf("Expected the view to stay at the bottom after resizing, got:\n%s\n", m.View())
	}
}

func TestScrolling(t *testing.T) {
	m := newTestModel(t, newTestManager(t), nil)
	for i := range 100 {
		m.addEntry(entryOutput, fmt.Sprintf("line %d", i))
	}
	if !m.viewport.AtBottom() {
		t.Fatal("Expected new output to be followed")
	}

	m, _ = update(t, m, tea.KeyMsg{Type: tea.KeyPgUp})
	offset := m.viewport.YOffset
	m.addEntry(entryOutput, "more output")
	if m.viewport.AtBottom() || m.viewport.YOffset != offset {
		t.Error("Expected new output not to move the view while scrolled up")
	}

	// running a command jumps back to the bottom
	m, _ = submit(t, m, "list")
	if !m.viewport.AtBottom() {
		t.Error("Expected a command to scroll to the bottom")
	}
}

func TestClearAndQuit(t *testing.T) {
	m, _ := submit(t, newTestModel(t, newTestManager(t), nil), "help")
	m, _ = submit(t, m, "clear")
	if len(m.transcript) != 0 {
		t.Errorf("Expected clear to empty the transcript, got %d entries\n", len(m.transcript))
	}
	if _, cmd := submit(t, m, "quit"); cmd == nil || cmd() != tea.Quit() {
		t.Error("Expected quit to quit")
	}
}

func TestCloneInvalidURL(t *testing.T) {
	m, cmd := submit(t, newTestModel(t, newTestManager(t), nil), "clone not-a-url")
	m = runTask(t, m, cmd)
	if last := lastEntry(t, m); last.kind != entryError || !strings.Contains(last.text, repo.ErrInvalidRepoURL.Error()) {
		t.Errorf("Expected an invalid URL error, got %+v\n", last)
	}
}

func TestTree(t *testing.T) {
	// the text tree is written to the working directory
	working_dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(working_dir) })

	manager := newTestManager(t)
	m, cmd := submit(t, newTestModel(t, manager, nil), "tree demo")
	m = runTask(t, m, cmd)
	if _, err := os.Stat(filepath.Join(manager.Tree, "demo.json")); err != nil {
		t.Errorf("Expected the tree JSON to be written, got %v\n", err)
	}
	if tree, err := os.ReadFile("demo_tree.txt"); err != nil || !strings.Contains(string(tree), "---main.go") {
		t.Errorf("Expected the text tree to be written, got %q, %v\n", tree, err)
	}
}

func TestTaskPanic(t *testing.T) {
	// the panic's stack trace is logged, which isn't worth showing here
	log.SetOutput(io.Discard)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	m := newTestModel(t, newTestManager(t), nil)
	cmd := m.startTask("Breaking", func(ctx context.Context, send func(tea.Msg)) taskDoneMsg {
		panic("oops")
	})
	m = runTask(t, m, cmd)
	if last := lastEntry(t, m); last.kind != entryError || !strings.Contains(last.text, "something went wrong: oops") {
		t.Errorf("Expected the panic to be reported, got %+v\n", last)
	}
}

func TestParseCommand(t *testing.T) {
	if name, args := parseCommand("  analyze   demo  "); name != "analyze" || len(args) != 1 || args[0] != "demo" {
		t.Errorf("Unexpected parse %q %q\n", name, args)
	}
	if name, args := parseCommand("   "); name != "" || args != nil {
		t.Errorf("Expected nothing from blank input, got %q %q\n", name, args)
	}
}
