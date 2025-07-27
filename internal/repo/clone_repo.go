package repo

import (
	"errors"
	"os/exec"
)

const (
	GIT = "git"
	CLONE = "clone"
)

var ErrRepoAlreadyFound = errors.New("repo already cloned")

func CloneRepo(repo_url string, repo_root string, repo_list map[string]string) (repo_name, repo_path string, err error) {
	repo_info, err := newRepoInfo(repo_url, repo_root)
	if err != nil {
		return "", "", err
	}

	if _, ok := repo_list[repo_info.repo_name]; ok {
		return "", "", ErrRepoAlreadyFound
	}

	clone_cmd := exec.Command(GIT, CLONE, repo_info.repo_url, repo_info.repo_path)
	clone_cmd.Run()
	return repo_info.repo_name, repo_info.repo_path, nil
}