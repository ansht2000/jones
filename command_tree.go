package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ansht2000/jones/internal/repo"
	tea "github.com/charmbracelet/bubbletea"
)

func commandTree(model *Model, args ...string) tea.Cmd {
	if len(args) != 1 {
		model.addEntry(entryError, fmt.Sprintf("Tree command expects 1 argument, received %d", len(args)))
		return nil
	}

	repo_name := args[0]
	repo_path, ok := model.repo_list[repo_name]
	if !ok {
		model.addEntry(entryError, fmt.Sprintf("Could not find repo %s, please clone it using the clone command first.", repo_name))
		return nil
	}

	tree_root := model.repo_manager.Tree
	return model.startTask("Building the tree of "+repo_name, func(ctx context.Context, send func(tea.Msg)) taskDoneMsg {
		repo_tree := repo.BuildRepoTree(repo_name, repo_path)
		repo_data, err := marshalRepoToJSON(repo_tree)
		if err != nil {
			return failed(err)
		}

		repo_JSON_filename := fmt.Sprintf("%s.json", repo_name)
		repo_JSON_path := filepath.Join(tree_root, repo_JSON_filename)
		err = os.WriteFile(repo_JSON_path, repo_data, 0644)
		if err != nil {
			return failed(err)
		}

		repo_string := repo.BuildRepoString(repo_tree)
		repo_string_filename := fmt.Sprintf("%s_tree.txt", repo_name)
		err = os.WriteFile(repo_string_filename, []byte(repo_string), 0644)
		if err != nil {
			return failed(err)
		}

		return taskDoneMsg{entries: []entry{{
			kind: entrySuccess,
			text: fmt.Sprintf("Wrote repo tree JSON to %s\nWrote repo tree string to %s", repo_JSON_path, repo_string_filename),
		}}}
	})
}
