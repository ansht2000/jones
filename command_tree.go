package main

import (
	"fmt"
	"os"

	"github.com/ansht2000/jones/internal/repo"
)

func commandTree(model *Model, args ...string) string {
	if len(args) != 1 {
		return fmt.Sprintf("Tree command expects 1 argument, received %d\n", len(args))
	}

	repo_name := args[0]
	if _, ok := model.repo_list[repo_name]; !ok {
		return fmt.Sprintf("Could not find repo %s, please clone it using the clone command first.\n", repo_name)
	}

	repo_tree := repo.BuildRepoTree(repo_name, model.repo_list[repo_name])
	repo_data, err := marshalRepoToJSON(repo_tree)
	if err != nil {
		return err.Error()
	}

	repo_JSON_filename := fmt.Sprintf("%s_tree.json", repo_name)
	err = os.WriteFile(repo_JSON_filename, repo_data, 0644)
	if err != nil {
		return err.Error()
	}

	repo_string := repo.BuildRepoString(repo_tree)
	repo_string_filename := fmt.Sprintf("%s_tree.txt", repo_name)
	err = os.WriteFile(repo_string_filename, []byte(repo_string), 0644)
	if err != nil {
		return err.Error()
	}

	return fmt.Sprintf("Wrote repo tree JSON to %s\nWrote repo stree string to %s\n", repo_JSON_filename, repo_string_filename)
}
