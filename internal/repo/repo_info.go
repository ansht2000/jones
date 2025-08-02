package repo

import (
	"errors"
	"path/filepath"
	"strings"
)

type repoInfo struct {
	// full repo git URL
	repo_url string
	// just the user, for convenience
	user_name string
	// just the repo, for convenience
	repo_name string
	// absolute path for a repo
	repo_path string
}

var ErrInvalidRepoURL = errors.New("invalid git repo URL")

func newRepoInfo(repo_url, clone_root string) (repoInfo, error) {
	user_name, repo_name, err := parseRepoNameFromURL(repo_url)
	if err != nil {
		return repoInfo{}, err
	}

	repo_path := filepath.Join(clone_root, repo_name)

	return repoInfo{
		repo_url:  repo_url,
		user_name: user_name,
		repo_name: repo_name,
		repo_path: repo_path,
	}, nil
}

func parseRepoNameFromURL(repo_url string) (user_name, repo_name string, err error) {
	// check if url uses http format
	// http format: https://github.com/{user}/{repo}.git
	if strings.HasPrefix(repo_url, "https:") {
		url_parts := strings.Split(repo_url, "/")
		user_name = url_parts[len(url_parts)-2]
		repo_name = strings.TrimRight(url_parts[len(url_parts)-1], ".git")
		return user_name, repo_name, nil
		// check if url uses ssh format
		// ssh format: git@github.com:{user}/{repo}.git
	} else if strings.HasPrefix(repo_url, "git@github.com") {
		url_parts := strings.Split(repo_url, ":")
		url_repo_parts := strings.Split(url_parts[len(url_parts)-1], "/")
		user_name = url_repo_parts[len(url_repo_parts)-2]
		repo_name = strings.TrimRight(url_repo_parts[len(url_repo_parts)-1], ".git")
		return user_name, repo_name, nil
	} else {
		return "", "", ErrInvalidRepoURL
	}
}
