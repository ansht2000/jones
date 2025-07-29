package main

import "fmt"

func commandHelp(model *Model, args ...string) string {
	s := "Usage:\n\n"
	for _, cmd := range getCommands() {
		s += fmt.Sprintf("%s: %s\n", cmd.name, cmd.description)
	}
	s += "\n"
	return s
}
