package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/adrg/xdg"
	"github.com/ansht2000/jones/internal/llm"
	"github.com/ansht2000/jones/internal/repo"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/joho/godotenv"
)

func main() {
	godotenv.Load(".env")

	// the TUI owns the terminal, so logs, including the Gemini SDK's, go to a file
	log_file, err := setupLogging()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to set up logging: %v\n", err)
		os.Exit(1)
	}
	defer log_file.Close()

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

	analysis_root := os.Getenv("ANALYSIS_ROOT")
	if analysis_root == "" {
		analysis_root = repo.DefaultAnalysisHome()
		log.Printf("Analysis root not set in environment, using default: %s", analysis_root)
	}

	// made before the TUI starts, since a failure can't be shown once it's running
	for _, dir := range []string{repo_root, tree_root, analysis_root} {
		if err := os.MkdirAll(dir, 0777); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to create directory %s: %v\n", dir, err)
			os.Exit(1)
		}
	}
	repo_manager := repo.NewRepoManager(repo_root, tree_root, analysis_root)

	// set before the program runs, so retries during tasks can be shown in it
	var program *tea.Program
	client, model_name, client_err := newLLMClient(func(msg tea.Msg) {
		program.Send(msg)
	})
	if client_err != nil {
		log.Printf("Gemini client not available: %v", client_err)
	}

	program = tea.NewProgram(initialModel(repo_manager, client, model_name, client_err), tea.WithAltScreen())
	if _, err := program.Run(); err != nil {
		fmt.Printf("Error encountered while running bubbletea: %v", err)
		os.Exit(1)
	}
}

func logPath() string {
	return filepath.Join(xdg.StateHome, "jones", "jones.log")
}

func setupLogging() (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(logPath()), 0755); err != nil {
		return nil, err
	}
	return tea.LogToFile(logPath(), "jones")
}

// Create the Gemini client from the environment. If that fails the error is
// returned instead of stopping jones, since cloning and browsing still work.
func newLLMClient(notify func(tea.Msg)) (llm.Client, string, error) {
	config, err := llm.GeminiConfigFromEnv()
	if err != nil {
		return nil, "", err
	}
	config.Retry.OnRetry = func(attempt int, err error, delay time.Duration) {
		log.Printf("Retrying a Gemini call in %v, attempt %d: %v", delay, attempt, err)
		notify(retryMsg{attempt: attempt, delay: delay, err: err})
	}

	client, err := llm.NewGeminiClient(context.Background(), config)
	if err != nil {
		// returned as a nil interface, not a nil *GeminiClient inside one
		return nil, config.Model, err
	}
	return client, config.Model, nil
}
