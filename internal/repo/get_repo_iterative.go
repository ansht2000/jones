package repo

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/ansht2000/jones/internal/llm"
)


func addRepoTree(repo_item *RepoItem) {
	// TODO: this seems brittle, find a better way to do this
	repo_dir_entries, err := os.ReadDir(repo_item.item_path)
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
				item_path: filepath.Join(repo_item.item_path, entry.Name()),
				is_dir: true,
				children: []*RepoItem{},
				parent: repo_item,
			}
			repo_item.children = append(repo_item.children, &child_dir_item)
			addRepoTree(&child_dir_item)
		} else {
			child_file_item := RepoItem{
				item_name: entry.Name(),
				item_path: filepath.Join(repo_item.item_path, entry.Name()),
				is_dir: false,
				parent: repo_item,
				summary: llm.MockLLMCall(),
			}
			repo_item.children = append(repo_item.children, &child_file_item)
		}
	}
}

func BuildRepoTree(repo_info RepoInfo) *RepoItem {
	root_item_path := fmt.Sprintf("../../%s", repo_info.repo_name)

	root_item := RepoItem{
		item_name: repo_info.repo_name,
		item_path: root_item_path,
		is_dir: true,
		children: []*RepoItem{},
		parent: nil,
	}

	addRepoTree(&root_item)
	return &root_item
}