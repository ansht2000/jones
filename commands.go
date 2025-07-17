package main

import tea "github.com/charmbracelet/bubbletea"

type Command struct {
	name string
	description string
	callback func(*Model, ...string) string
}

type CommandMap map[string]Command

type CommandMsg string

func sendCommandMsg(command_name string) tea.Cmd {
	return func() tea.Msg {
		return CommandMsg(command_name)
	}
} 

func getCommands() CommandMap {
	return CommandMap{
		"help":  {
			name: "help",
			description: "Displays a help message",
			callback: commandHelp,
		},
	}
}