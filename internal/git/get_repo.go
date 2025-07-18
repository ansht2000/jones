package git

import (
	"errors"
	"os"
	"os/exec"
	"strings"
)

const CLONE_COMMAND = "git clone"

var ErrInvalidRepoURL = errors.New("Invalid git repo URL")

type GitRepoURL struct {
	// full repo URL
	repo_url string
	// just the user, for convenience
	user_name string
	// just the repo, for convenience
	repo_name string
}

func parseRepoName(repo_url string) (user, repo string, err error) {
	// check if url uses http format
	// http format: https://github.com/{user}/{repo}.git
	if strings.HasPrefix(repo_url, "https:") {
		url_parts := strings.Split(repo_url, "/")
		user = url_parts[len(url_parts) - 2]
		repo = strings.TrimRight(url_parts[len(url_parts) - 1], ".git")
		return user, repo, nil
	// check if url uses ssh format
	// ssh format: git@github.com:{user}/{repo}.git	
	} else if strings.HasPrefix(repo_url, "git@github.com") {
		url_parts := strings.Split(repo_url, ":")
		url_repo_parts := strings.Split(url_parts[len(url_parts) - 1], "/")
		user = url_repo_parts[len(url_repo_parts) - 1]
		repo = strings.TrimRight(url_repo_parts[len(url_repo_parts) - 1], ".git")
		return user, repo, nil
	} else {
		return "", "", ErrInvalidRepoURL
	}
}

func GetRepo(repo_url string) exec.Cmd {
	os.Chmod("ppop", os.ModeAppend)
	cmd := exec.Cmd{}
	return cmd
}