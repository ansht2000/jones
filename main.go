package main

import (
	"fmt"
	"log"

	// "io"
	// "log"
	"os"

	"github.com/adrg/xdg"
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

	// this seems convoluted for just holding a root url
	// but in case i want to add support for custom filesystems
	// it will be useful
	var repo_manager *repo.RepoManager
	if repo_root == "" {
		log.Printf("Repo cloning root not set, using default: %s\n", xdg.DataHome)
		repo_manager = repo.DefaultRepoManager()
	} else {
		log.Printf("Using repo cloning root: %s", repo_root)
		repo_manager = repo.NewRepoManager(repo_root)
	}

	p := tea.NewProgram(initialModel(repo_manager))
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error encountered while running bubbletea: %v", err)
		os.Exit(1)
	}
}
