package repo

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

var ErrFailedDirRead = errors.New("failed to read items in directory")

// TODO: look into turning this into a function that returns a map
// research which one is more idiomatic/performant
var IGNORE_LIST = map[string]struct{}{
	".git": {},
}

type RepoItem struct {
	ItemName string      `json:"item_name"`
	ItemPath string      `json:"item_path"`
	IsDir    bool        `json:"is_dir"`
	Summary  string      `json:"summary"`
	Children []*RepoItem `json:"children"`
	Err      string      `json:"err"`
}

func scanRepo(repo_item *RepoItem, repo_wg *sync.WaitGroup) {
	repo_dir_entries, err := os.ReadDir(repo_item.ItemPath)
	if err != nil {
		err_string := ErrFailedDirRead.Error() + ": " + err.Error()
		repo_item.Err = err_string
		return
	}

	for _, entry := range repo_dir_entries {
		if _, ok := IGNORE_LIST[entry.Name()]; ok {
			continue
		}
		if entry.IsDir() {
			child_dir_item := RepoItem{
				ItemName: entry.Name(),
				ItemPath: filepath.Join(repo_item.ItemPath, entry.Name()),
				IsDir:    true,
				Children: []*RepoItem{},
			}
			repo_item.Children = append(repo_item.Children, &child_dir_item)
			repo_wg.Add(1)
			go func() {
				defer repo_wg.Done()
				scanRepo(&child_dir_item, repo_wg)
			}()
		} else {
			child_file_item := RepoItem{
				ItemName: entry.Name(),
				ItemPath: filepath.Join(repo_item.ItemPath, entry.Name()),
				IsDir:    false,
			}
			repo_item.Children = append(repo_item.Children, &child_file_item)
		}
	}
}

func BuildRepoTree(repo_name, repo_path string) *RepoItem {
	root_item := RepoItem{
		ItemName: repo_name,
		ItemPath: repo_path,
		IsDir:    true,
		Children: []*RepoItem{},
	}

	var repo_wg sync.WaitGroup
	scanRepo(&root_item, &repo_wg)

	repo_wg.Wait()
	return &root_item
}

func scanRepoFolder(repo_string *string, repo_folder *RepoItem, indent int) {
	for _, repo_item := range repo_folder.Children {
		*repo_string += "|"
		for range indent {
			*repo_string += "\t|"
		}

		*repo_string += fmt.Sprintf("---%s\n", repo_item.ItemName)
		if repo_item.IsDir {
			scanRepoFolder(repo_string, repo_item, indent + 1)
		}
	}
}

func BuildRepoString(repo *RepoItem) string {
	repo_string := fmt.Sprintf("%s\n", repo.ItemName)
	indent := 0
	scanRepoFolder(&repo_string, repo, indent)
	return repo_string
}
