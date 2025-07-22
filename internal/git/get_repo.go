package git

import (
	"errors"
	"os"
	"os/exec"
	"strings"
)

const (
	GIT = "git"
	CLONE = "clone"
)

var ErrInvalidRepoURL = errors.New("invalid git repo URL")
var ErrFailedDirRead = errors.New("failed to read items in directory")

type RepoInfo struct {
	// full repo URL
	repo_url string
	// just the user, for convenience
	user_name string
	// just the repo, for convenience
	repo_name string
}

type RepoItem struct {
	item_name string
	is_dir bool
	summary string 
	children []*RepoItem
	parent *RepoItem
	err *string
}

func addRepoTree(repo_item *RepoItem) {
	repo_dir_entries, err := os.ReadDir(repo_item.item_name)
	if err != nil {
		err_string := ErrFailedDirRead.Error() + ": " + err.Error()
		repo_item.err = &err_string
		return
	}

	// TODO: add an ignore list for files that are unimportant to the codebase later
	for _, entry := range repo_dir_entries {
		if entry.IsDir() {
			child_dir_item := RepoItem{
				item_name: entry.Name(),
				is_dir: true,
				children: []*RepoItem{},
				parent: repo_item,
			}
			repo_item.children = append(repo_item.children, &child_dir_item)
			go addRepoTree(&child_dir_item)
		} else {
			child_file_item := RepoItem{
				item_name: entry.Name(),
				is_dir: false,
				parent: repo_item,
			}
			repo_item.children = append(repo_item.children, &child_file_item)
		}
	}
}

func buildRepoTree(repo_info RepoInfo) *RepoItem {
	root_item := RepoItem{
		item_name: repo_info.repo_name,
		is_dir: true,
		children: []*RepoItem{},
		parent: nil,
	}

	go addRepoTree(&root_item)
	return &root_item
}

func parseRepoName(repo_url string) (RepoInfo, error) {
	repo_info := RepoInfo{repo_url: repo_url}
	// check if url uses http format
	// http format: https://github.com/{user}/{repo}.git
	if strings.HasPrefix(repo_url, "https:") {
		url_parts := strings.Split(repo_url, "/")
		repo_info.user_name = url_parts[len(url_parts) - 2]
		repo_info.repo_name = strings.TrimRight(url_parts[len(url_parts) - 1], ".git")
		return repo_info, nil
	// check if url uses ssh format
	// ssh format: git@github.com:{user}/{repo}.git	
	} else if strings.HasPrefix(repo_url, "git@github.com") {
		url_parts := strings.Split(repo_url, ":")
		url_repo_parts := strings.Split(url_parts[len(url_parts) - 1], "/")
		repo_info.user_name = url_repo_parts[len(url_repo_parts) - 1]
		repo_info.repo_name = strings.TrimRight(url_repo_parts[len(url_repo_parts) - 1], ".git")
		return repo_info, nil
	} else {
		return RepoInfo{}, ErrInvalidRepoURL
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