package main

import tea "github.com/charmbracelet/bubbletea"

type Command struct {
	name        string
	description string
	callback    func(*Model, ...string) string
}

type CommandMap map[string]Command

type CommandMsg struct {
	command_name string
	command_args []string
}

func CommandMessage(command_name string, command_args []string) CommandMsg {
	return CommandMsg{command_name: command_name, command_args: command_args}
}

func sendCommandMsg(command_name string, command_args []string) tea.Cmd {
	return func() tea.Msg {
		return CommandMessage(command_name, command_args)
	}
}

func getCommands() CommandMap {
	return CommandMap{
		"help": {
			name:        "help",
			description: "Displays a help message",
			callback:    commandHelp,
		},
		"clone": {
			name:        "clone <repo_url>",
			description: "Clones a git repo into the working directory",
			callback:    commandClone,
		},
		"tree": {
			name: "tree <repo_name>",
			description: "Builds a tree representation of the selected repo",
			callback: commandTree,
		},
		"list": {
			name: "list",
			description: "List available repos",
			callback: commandList,
		},
	}
}