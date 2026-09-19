package main

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Colors come from the terminal's own palette, so they fit its theme
var (
	command_style = lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Bold(true)
	note_style    = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	success_style = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	warning_style = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	error_style   = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	status_style  = lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Background(lipgloss.Color("4"))
)

type entryKind int

const (
	// what the user typed
	entryCommand entryKind = iota
	entryOutput
	// progress along the way, like which files the agent read
	entryNote
	entrySuccess
	entryWarning
	entryError
)

// One block of text in the transcript
type entry struct {
	kind entryKind
	text string
}

// The entry styled and wrapped to width
func (e entry) render(width int) string {
	style := lipgloss.NewStyle()
	text := e.text
	switch e.kind {
	case entryCommand:
		style = command_style
		text = "> " + text
	case entryNote:
		style = note_style
	case entrySuccess:
		style = success_style
	case entryWarning:
		style = warning_style
	case entryError:
		style = error_style
	}
	return style.Width(width).Render(text)
}

// Add an entry to the end of the transcript
func (m *Model) addEntry(kind entryKind, text string) {
	new_entry := entry{kind: kind, text: strings.TrimRight(text, "\n")}
	m.transcript = append(m.transcript, new_entry)
	m.rendered_transcript = appendBlock(m.rendered_transcript, new_entry, m.viewport.Width)
	m.refresh()
}

// Render the whole transcript again, like after the window is resized
func (m *Model) rerender() {
	m.rendered_transcript = ""
	for _, old_entry := range m.transcript {
		m.rendered_transcript = appendBlock(m.rendered_transcript, old_entry, m.viewport.Width)
	}
	m.refresh()
}

func appendBlock(rendered string, new_entry entry, width int) string {
	block := new_entry.render(width)
	if rendered == "" {
		return block
	}
	// a blank line before each command separates it from the one before
	if new_entry.kind == entryCommand {
		return rendered + "\n\n" + block
	}
	return rendered + "\n" + block
}

// Show the transcript and the answer being streamed, following new output
// unless the user has scrolled up to read something
func (m *Model) refresh() {
	at_bottom := m.viewport.AtBottom()
	content := m.rendered_transcript
	if m.live != "" {
		content = appendBlock(content, entry{kind: entryOutput, text: m.live}, m.viewport.Width)
	}
	m.viewport.SetContent(content)
	if at_bottom {
		m.viewport.GotoBottom()
	}
}
