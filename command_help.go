package main

import "fmt"

func commandHelp(cfg *programConfig, args ...string) error {
	fmt.Println("Jones is here to help")
	fmt.Println("Usage:")
	fmt.Println("")
	for _, cmd := range getCommands() {
		fmt.Printf("%s: %s\n", cmd.name, cmd.description)
	}
	fmt.Println("")
	return nil
}