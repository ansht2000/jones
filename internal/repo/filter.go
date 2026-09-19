package repo

import (
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
)

// Decides which files and directories in a repo get scanned
type repoFilter struct {
	// paths of files tracked by git and every directory above them, relative
	// to the repo root with forward slashes, nil when the repo isn't a git repo
	tracked map[string]struct{}
}

// Build a filter for the repo at repo_path. For git repos only tracked files
// are scanned, which leaves out build output, installed dependencies, and
// anything else covered by .gitignore.
func newRepoFilter(repo_path string) *repoFilter {
	// only use git when repo_path is a repo's root, a directory inside some
	// other repo would otherwise get that repo's file list
	if _, err := os.Stat(filepath.Join(repo_path, ".git")); err != nil {
		return &repoFilter{}
	}
	output, err := exec.Command(GIT, "-C", repo_path, "ls-files", "-z").Output()
	if err != nil {
		return &repoFilter{}
	}

	tracked := map[string]struct{}{}
	for _, file := range strings.Split(string(output), "\x00") {
		// add the file and the directories above it, stopping at the first
		// directory already added since everything above it is too
		for tracked_path := file; tracked_path != "" && tracked_path != "."; tracked_path = path.Dir(tracked_path) {
			if _, ok := tracked[tracked_path]; ok {
				break
			}
			tracked[tracked_path] = struct{}{}
		}
	}
	return &repoFilter{tracked: tracked}
}

// rel_path is relative to the repo root with forward slashes
func (f *repoFilter) include(rel_path string) bool {
	if _, ok := IGNORE_LIST[path.Base(rel_path)]; ok {
		return false
	}
	if f.tracked == nil {
		return true
	}
	_, ok := f.tracked[rel_path]
	return ok
}
