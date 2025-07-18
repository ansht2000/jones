package git

import (
	"errors"
	"os/exec"
	"strings"
)

const (
	GIT = "git"
	CLONE = "clone"
)

var ErrInvalidRepoURL = errors.New("invalid git repo URL")

type GitRepoInfo struct {
	// full repo URL
	repo_url string
	// just the user, for convenience
	user string
	// just the repo, for convenience
	repo string
}

func parseRepoName(repo_url string) (GitRepoInfo, error) {
	repo_info := GitRepoInfo{repo_url: repo_url}
	// check if url uses http format
	// http format: https://github.com/{user}/{repo}.git
	if strings.HasPrefix(repo_url, "https:") {
		url_parts := strings.Split(repo_url, "/")
		repo_info.user = url_parts[len(url_parts) - 2]
		repo_info.repo = strings.TrimRight(url_parts[len(url_parts) - 1], ".git")
		return repo_info, nil
	// check if url uses ssh format
	// ssh format: git@github.com:{user}/{repo}.git	
	} else if strings.HasPrefix(repo_url, "git@github.com") {
		url_parts := strings.Split(repo_url, ":")
		url_repo_parts := strings.Split(url_parts[len(url_parts) - 1], "/")
		repo_info.user = url_repo_parts[len(url_repo_parts) - 1]
		repo_info.repo = strings.TrimRight(url_repo_parts[len(url_repo_parts) - 1], ".git")
		return repo_info, nil
	} else {
		return GitRepoInfo{}, ErrInvalidRepoURL
	}
}

func CloneRepo(repo_url string) error {
	repo_info, err := parseRepoName(repo_url)
	if err != nil {
		return err
	}

	clone_cmd := exec.Command(GIT, CLONE, repo_info.repo_url)
	clone_cmd.Run()
	return nil
}