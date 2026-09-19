package main

import (
	"maps"
	"os"
	"slices"
	"strings"

	"github.com/ansht2000/jones/internal/analysis"
	tea "github.com/charmbracelet/bubbletea"
)

func commandList(model *Model, args ...string) tea.Cmd {
	if len(model.repo_list) == 0 {
		model.addEntry(entryOutput, "No repos found, clone some!")
		return nil
	}

	var list strings.Builder
	list.WriteString("Available repos:\n\n")
	for _, repo_name := range slices.Sorted(maps.Keys(model.repo_list)) {
		list.WriteString(repo_name)
		if _, err := os.Stat(analysis.SavePath(model.repo_manager.Analysis, repo_name)); err == nil {
			list.WriteString(" (analyzed)")
		}
		if repo_name == model.active_repo {
			list.WriteString(" (in use)")
		}
		list.WriteString("\n")
	}
	model.addEntry(entryOutput, list.String())
	return nil
}
