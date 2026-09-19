package main

import (
	"errors"
	"fmt"
	"io/fs"

	"github.com/ansht2000/jones/internal/analysis"
	tea "github.com/charmbracelet/bubbletea"
)

func commandUse(model *Model, args ...string) tea.Cmd {
	if len(args) != 1 {
		model.addEntry(entryError, fmt.Sprintf("Use command expects 1 argument, received %d", len(args)))
		return nil
	}
	repo_name := args[0]
	if _, ok := model.repo_list[repo_name]; !ok {
		model.addEntry(entryError, fmt.Sprintf("Could not find repo %s, please clone it using the clone command first.", repo_name))
		return nil
	}

	repo_analysis, ok := model.analyses[repo_name]
	if !ok {
		saved, err := analysis.LoadAnalysis(model.repo_manager.Analysis, repo_name)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			model.addEntry(entryWarning, fmt.Sprintf("%s hasn't been analyzed yet, run analyze %s first.", repo_name, repo_name))
			return nil
		case errors.Is(err, analysis.ErrOutdated):
			model.addEntry(entryWarning, fmt.Sprintf("%s was analyzed by an older version of jones, run analyze %s again.", repo_name, repo_name))
			return nil
		case err != nil:
			model.addEntry(entryError, "Error: "+err.Error())
			return nil
		}
		repo_analysis = saved
		model.analyses[repo_name] = saved
	}

	model.active_repo = repo_name
	model.addEntry(entrySuccess, fmt.Sprintf("Using %s, ask away!", repo_name))
	model.addEntry(entryOutput, repo_analysis.Overview)
	return nil
}
