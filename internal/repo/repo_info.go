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
	var repo_part string
	// check if url uses http format
	// http format: https://github.com/{user}/{repo}.git
	if rest, ok := strings.CutPrefix(repo_url, "https://"); ok {
		// drop the host
		_, repo_part, ok = strings.Cut(rest, "/")
		if !ok {
			return "", "", ErrInvalidRepoURL
		}
		// check if url uses ssh format
		// ssh format: git@github.com:{user}/{repo}.git
	} else if strings.HasPrefix(repo_url, "git@") {
		var ok bool
		_, repo_part, ok = strings.Cut(repo_url, ":")
		if !ok {
			return "", "", ErrInvalidRepoURL
		}
	} else {
		return "", "", ErrInvalidRepoURL
	}

	url_repo_parts := strings.Split(strings.Trim(repo_part, "/"), "/")
	if len(url_repo_parts) != 2 {
		return "", "", ErrInvalidRepoURL
	}
	user_name = url_repo_parts[0]
	repo_name = strings.TrimSuffix(url_repo_parts[1], ".git")
	if user_name == "" || repo_name == "" {
		return "", "", ErrInvalidRepoURL
	}
	return user_name, repo_name, nil
}
