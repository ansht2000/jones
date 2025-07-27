package main

import (
	"fmt"

	"github.com/ansht2000/jones/internal/repo"
)

func commandClone(model *Model, args ...string) string {
	if len(args) != 1 {
		return fmt.Sprintf("Clone command expects 1 argument, received %d\n", len(args))
	}
	repo_name, repo_path, err := repo.CloneRepo(args[0], model.repo_manager.Root, model.repo_list)
	if err != nil {
		return err.Error() + "\n"
	}

	model.repo_list[repo_name] = repo_path
	return "Cloned repo successfully!\n"
}