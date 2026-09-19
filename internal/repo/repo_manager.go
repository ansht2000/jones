package repo

import (
	"path/filepath"

	"github.com/adrg/xdg"
)

type RepoManager struct {
	Root     string
	Tree     string
	Analysis string
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

func NewRepoManager(root, tree, analysis string) *RepoManager {
	return &RepoManager{
		Root:     root,
		Tree:     tree,
		Analysis: analysis,
	}
}
