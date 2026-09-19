package main

import tea "github.com/charmbracelet/bubbletea"

// Commands that take a while return a tea.Cmd that runs them in the
// background, quick ones change the model and return nil
type Command struct {
	name        string
	description string
	callback    func(*Model, ...string) tea.Cmd
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
			description: "Clones a git repo",
			callback:    commandClone,
		},
		"tree": {
			name:        "tree <repo_name>",
			description: "Builds a tree representation of the selected repo",
			callback:    commandTree,
		},
		"list": {
			name:        "list",
			description: "List available repos",
			callback:    commandList,
		},
		"analyze": {
			name:        "analyze <repo_name>",
			description: "Summarizes a repo so you can ask questions about it, rerun it to catch up on changes",
			callback:    commandAnalyze,
		},
		"use": {
			name:        "use <repo_name>",
			description: "Picks the analyzed repo to ask questions about",
			callback:    commandUse,
		},
		"ask": {
			name:        "ask <question>",
			description: "Asks a question about the repo in use, anything that isn't a command is asked too",
			callback:    commandAsk,
		},
		"clear": {
			name:        "clear",
			description: "Clears the screen",
			callback:    commandClear,
		},
		"quit": {
			name:        "quit",
			description: "Quits jones",
			callback:    commandQuit,
		},
	}
}
