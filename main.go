package main

import (
	"fmt"
	"log"

	// "io"
	// "log"
	"os"

	"github.com/ansht2000/jones/internal/repo"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/joho/godotenv"
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
	godotenv.Load(".env")

	repo_root := os.Getenv("REPO_ROOT")
	if repo_root == "" {
		repo_root = repo.DefaultRepoHome()
		log.Printf("Repo root not set in environment, using default: %s", repo_root)
	}

	tree_root := os.Getenv("TREE_ROOT")
	if tree_root == "" {
		tree_root = repo.DefaultTreeHome()
		log.Printf("Tree root not set in environment, using default: %s", tree_root)
	}

	repo_manager := repo.NewRepoManager(repo_root, tree_root)

	p := tea.NewProgram(initialModel(repo_manager))
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error encountered while running bubbletea: %v", err)
		os.Exit(1)
	}
}
