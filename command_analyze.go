package main

import (
	"context"
	"fmt"
	"time"

	"github.com/ansht2000/jones/internal/analysis"
	tea "github.com/charmbracelet/bubbletea"
)

func commandAnalyze(model *Model, args ...string) tea.Cmd {
	if len(args) != 1 {
		model.addEntry(entryError, fmt.Sprintf("Analyze command expects 1 argument, received %d", len(args)))
		return nil
	}
	repo_name := args[0]
	repo_path, ok := model.repo_list[repo_name]
	if !ok {
		model.addEntry(entryError, fmt.Sprintf("Could not find repo %s, please clone it using the clone command first.", repo_name))
		return nil
	}
	if model.client == nil {
		model.addEntry(entryError, "Can't analyze without an API key: "+model.client_err.Error())
		return nil
	}

	client := model.client
	options := analysis.DefaultOptions()
	options.SaveDir = model.repo_manager.Analysis
	return model.startTask("Analyzing "+repo_name, func(ctx context.Context, send func(tea.Msg)) taskDoneMsg {
		options.OnProgress = func(progress analysis.Progress) {
			send(progressMsg{progress: progress})
		}
		start := time.Now()
		result, err := analysis.Analyze(ctx, client, repo_name, repo_path, options)
		if err != nil {
			return failed(err)
		}

		return taskDoneMsg{
			entries: []entry{
				analysisSummary(repo_name, result, time.Since(start)),
				{kind: entryOutput, text: result.Overview},
			},
			apply: func(m *Model) {
				m.analyses[repo_name] = result
				m.active_repo = repo_name
			},
		}
	})
}

func analysisSummary(repo_name string, result *analysis.Analysis, elapsed time.Duration) entry {
	skipped, failed := 0, 0
	for _, file := range result.Files {
		if file.Skipped != "" {
			skipped++
		}
		if file.Error != "" {
			failed++
		}
	}

	summary := fmt.Sprintf("Analyzed %s in %s: %d files, %d of them not sent to the model, and %d directories.",
		repo_name, elapsed.Round(100*time.Millisecond), len(result.Files), skipped, len(result.Dirs))
	if failed > 0 {
		return entry{kind: entryWarning, text: summary + fmt.Sprintf(" %d files couldn't be summarized, run analyze %s again to retry them.", failed, repo_name)}
	}
	return entry{kind: entrySuccess, text: summary + " Ask away!"}
}
