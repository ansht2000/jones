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
	parts := strings.Fields(input)
	if len(parts) == 0 {
		return "", nil
	}
	command := parts[0]
	args := parts[1:]
	return command, args
}

func getClonedRepos(repo_root string) map[string]string {
	repo_list := make(map[string]string)
	repos, _ := os.ReadDir(repo_root)
	for _, repo := range repos {
		// only directories are repos
		if repo.IsDir() {
			repo_list[repo.Name()] = filepath.Join(repo_root, repo.Name())
		}
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

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// Each item on its own line, as a list
func bullets(items []string) string {
	lines := make([]string, len(items))
	for i, item := range items {
		lines[i] = "- " + item
	}
	return strings.Join(lines, "\n")
}
