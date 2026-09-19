package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"runtime/debug"
	"strings"
	"time"

	"github.com/ansht2000/jones/internal/agent"
	"github.com/ansht2000/jones/internal/analysis"
	tea "github.com/charmbracelet/bubbletea"
)

// A background job, like cloning a repo or answering a question. Its
// goroutine sends messages as it goes and ends with a taskDoneMsg, so the UI
// keeps responding while it runs. Only one task runs at a time.
type task struct {
	// what the task is doing, shown next to the spinner
	status string
	// shown after the status, like an API call being retried
	notice string
	// the latest progress of an analysis
	progress *analysis.Progress
	cancel   context.CancelFunc
	events   chan tea.Msg
}

// Sent by a task when it finishes
type taskDoneMsg struct {
	entries []entry
	// changes the model with the task's result, called from Update so the
	// model is only ever touched by the UI's goroutine
	apply func(m *Model)
}

// Sent by an analysis as it makes progress
type progressMsg struct {
	progress analysis.Progress
}

// Sent by the agent as it works on an answer
type agentEventMsg struct {
	event agent.Event
}

// Sent when an API call failed and is about to be retried
type retryMsg struct {
	attempt int
	delay   time.Duration
	err     error
}

// Start work in the background as the model's task. work reports progress
// with send, and returns the message that finishes the task.
func (m *Model) startTask(status string, work func(ctx context.Context, send func(tea.Msg)) taskDoneMsg) tea.Cmd {
	if m.task != nil {
		m.addEntry(entryError, fmt.Sprintf("Still busy with %q, press esc to cancel it first.", m.task.status))
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	events := make(chan tea.Msg, 64)
	m.task = &task{status: status, cancel: cancel, events: events}
	go func() {
		defer close(events)
		defer func() {
			// a bug in a task shouldn't take the whole UI down with it
			if recovered := recover(); recovered != nil {
				log.Printf("Task %q panicked: %v\n%s", status, recovered, debug.Stack())
				events <- failed(fmt.Errorf("something went wrong: %v", recovered))
			}
		}()
		events <- work(ctx, func(msg tea.Msg) {
			events <- msg
		})
	}()
	return tea.Batch(waitForTask(events), m.spinner.Tick)
}

// Wait for the next message from a task
func waitForTask(events <-chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		return <-events
	}
}

// Wait for the next message from the running task. Only called after
// handling a message that came from the task, so there is one wait at a time.
func (m *Model) waitForTask() tea.Cmd {
	if m.task == nil {
		return nil
	}
	return waitForTask(m.task.events)
}

func (m *Model) cancelTask() {
	if m.task != nil {
		m.task.cancel()
	}
}

func (m *Model) finishTask(msg taskDoneMsg) {
	if m.task != nil {
		// releases the task's context
		m.task.cancel()
		m.task = nil
	}
	if msg.apply != nil {
		msg.apply(m)
	}
	// a streamed answer stays in the transcript, even a partial one from a cancelled task
	if m.live != "" {
		live := m.live
		m.live = ""
		m.addEntry(entryOutput, live)
	}
	for _, done_entry := range msg.entries {
		m.addEntry(done_entry.kind, done_entry.text)
	}
}

// Finish a task that failed
func failed(err error) taskDoneMsg {
	if errors.Is(err, context.Canceled) {
		return taskDoneMsg{entries: []entry{{kind: entryWarning, text: "Cancelled."}}}
	}
	return taskDoneMsg{entries: []entry{{kind: entryError, text: "Error: " + err.Error()}}}
}

// Show what the agent is doing as it answers
func (m *Model) handleAgentEvent(event agent.Event) {
	if m.task == nil {
		return
	}
	m.task.notice = ""

	switch event.Kind {
	case agent.EventDecided:
		if len(event.Paths) > 0 {
			m.task.status = "Reading " + strings.Join(event.Paths, ", ")
			m.addEntry(entryNote, fmt.Sprintf("Reading %s: %s", strings.Join(event.Paths, ", "), event.Reason))
		} else {
			m.task.status = "Writing the answer"
			m.addEntry(entryNote, "Answering: "+event.Reason)
		}
	case agent.EventRead:
		for _, issue := range event.Issues {
			m.addEntry(entryNote, "Skipped "+issue)
		}
	case agent.EventAnswerStart:
		m.task.status = "Writing the answer"
		m.live = ""
		m.refresh()
	case agent.EventAnswerChunk:
		m.live += event.Text
		m.refresh()
	case agent.EventChecking:
		m.task.status = "Checking the answer against the code"
	case agent.EventRetry:
		m.task.status = "Revising the answer"
		m.live = ""
		m.addEntry(entryWarning, "That answer didn't hold up, revising it:\n"+bullets(event.Issues))
	}
}
