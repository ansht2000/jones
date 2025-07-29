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

	repo := repo.BuildRepoTree(repo_name, model.repo_list[repo_name])
	repo_data, err := marshalRepoToJSON(repo)
	if err != nil {
		return err.Error()
	}

	os.WriteFile("repo.json", repo_data, 0644)
	return "Wrote repo tree to repo.json\n"
}