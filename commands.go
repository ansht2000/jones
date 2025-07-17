package main

type command struct {
	name string
	description string
	callback func(*programConfig, ...string) error
}

func getCommands() commandMap {
	return commandMap{
		"help":  {
			name: "help",
			description: "Displays a help message",
			callback: commandHelp,
		},
	}
}