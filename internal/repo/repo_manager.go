package repo

import (
	"path/filepath"

	"github.com/adrg/xdg"
)

type RepoManager struct {
	Root string
}

func defaultRepoHome() string {
	return filepath.Join(xdg.DataHome, "jones", "repos")
}

func NewDefaultRepoManager() *RepoManager {
	return &RepoManager{
		Root: defaultRepoHome(),
	}
}

func NewRepoManager(root string) *RepoManager {
	return &RepoManager{
		Root: root,
	}
}