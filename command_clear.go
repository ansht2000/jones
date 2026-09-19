package main

import tea "github.com/charmbracelet/bubbletea"

func commandClear(model *Model, args ...string) tea.Cmd {
	model.transcript = nil
	model.rerender()
	return nil
}
