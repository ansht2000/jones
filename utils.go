package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/ansht2000/jones/internal/repo"
)

func (cm CommandMap) getKeys() []string {
	keys := []string{}
	for key := range cm {
		keys = append(keys, key)
	}
	return keys
}

func (cm CommandMap) getValues() []Command {
	values := []Command{}
	for _, value := range cm {
		values = append(values, value)
	}
	return values
}

func parseCommand(input string) (string, []string) {
	parts := strings.Split(input, " ")
	command := parts[0]
	args := parts[1:]
	return command, args
}

func getClonedRepos(repo_root string) map[string]string {
	repo_list := make(map[string]string)
	repos, _ := os.ReadDir(repo_root)
	for _, repo := range repos {
		repo_list[repo.Name()] = filepath.Join(repo_root, repo.Name())
	}
	return repo_list
}

func marshalRepoToJSON(repo *repo.RepoItem) ([]byte, error) {
	repo_data, err := json.MarshalIndent(repo, "", "\t")
	if err != nil {
		return nil, err
	}

	return repo_data, nil
}
