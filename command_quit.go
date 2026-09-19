package main

import tea "github.com/charmbracelet/bubbletea"

func commandQuit(model *Model, args ...string) tea.Cmd {
	model.cancelTask()
	return tea.Quit
}
