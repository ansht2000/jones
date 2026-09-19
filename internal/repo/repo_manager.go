package repo

import (
	"path/filepath"

	"github.com/adrg/xdg"
)

type RepoManager struct {
	Root string
	Tree string
}

func DefaultRepoHome() string {
	return filepath.Join(xdg.DataHome, "jones", "repos")
}

func DefaultTreeHome() string {
	return filepath.Join(xdg.DataHome, "jones", "trees")
}

func DefaultAnalysisHome() string {
	return filepath.Join(xdg.DataHome, "jones", "analysis")
}

func NewRepoManager(root, tree string) *RepoManager {
	return &RepoManager{
		Root: root,
		Tree: tree,
	}
}
