package main

import (
	"context"
	"fmt"
	"maps"

	"github.com/ansht2000/jones/internal/repo"
	tea "github.com/charmbracelet/bubbletea"
)

func commandClone(model *Model, args ...string) tea.Cmd {
	if len(args) != 1 {
		model.addEntry(entryError, fmt.Sprintf("Clone command expects 1 argument, received %d", len(args)))
		return nil
	}

	repo_url := args[0]
	repo_root := model.repo_manager.Root
	// the task gets its own copy, since the model's list can change while it runs
	repo_list := maps.Clone(model.repo_list)
	return model.startTask("Cloning "+repo_url, func(ctx context.Context, send func(tea.Msg)) taskDoneMsg {
		repo_name, repo_path, err := repo.CloneRepo(ctx, repo_url, repo_root, repo_list)
		if err != nil {
			return failed(err)
		}
		return taskDoneMsg{
			entries: []entry{{kind: entrySuccess, text: fmt.Sprintf("Cloned %s! Run analyze %s to ask questions about it.", repo_name, repo_name)}},
			apply: func(m *Model) {
				m.repo_list[repo_name] = repo_path
			},
		}
	})
}
