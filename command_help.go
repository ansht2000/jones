package main

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

func commandHelp(model *Model, args ...string) tea.Cmd {
	var help strings.Builder
	help.WriteString("Usage:\n\n")
	for _, command_name := range slices.Sorted(maps.Keys(model.commands)) {
		command := model.commands[command_name]
		fmt.Fprintf(&help, "%s: %s\n", command.name, command.description)
	}
	help.WriteString("\nup/down and pgup/pgdn scroll, esc cancels what's running, ctrl+c quits")
	model.addEntry(entryOutput, help.String())
	return nil
}
