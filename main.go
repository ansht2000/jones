package main

import (
	"fmt"
	// "io"
	// "log"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	// "github.com/chzyer/readline"
)

func main() {
	// l, err := readline.NewEx(&readline.Config{
	// 	Prompt: "> ",
	// 	HistoryFile: "/tmp/readline.tmp",
	// 	InterruptPrompt: "Force Quit...Exiting",
	// 	EOFPrompt: "Goodbye!",
	// })
	// if err != nil {
	// 	log.Fatalf("Failed to initialize input reader: %v\n", err)
	// }
	// defer l.Close()

	// cfg := &programConfig{}
	// fmt.Println("Enter the url of a git repo: ")
	// for {
	// 	line, err := l.Readline()
	// 	if err == readline.ErrInterrupt {
	// 		if len(line) == 0 {
	// 			break
	// 		} else {
	// 			continue
	// 		}
	// 	} else if err == io.EOF {
	// 		break
	// 	}

	// 	args := []string{}
	// 	switch line {
	// 	case "help":
	// 		commandHelp(cfg, args...)
	// 	}
	// }
	p := tea.NewProgram(initialModel())
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error encountered while running bubbletea: %v", err)
		os.Exit(1)
	}
}