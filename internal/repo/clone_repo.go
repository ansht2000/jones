package repo

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

const (
	GIT   = "git"
	CLONE = "clone"
)

var ErrRepoAlreadyFound = errors.New("repo already cloned")
var ErrCloneFailed = errors.New("failed to clone repo")

// TODO: look into using go-git (https://github.com/go-git/go-git) for cloning repos instead
// may be worth it, may not be
func CloneRepo(ctx context.Context, repo_url string, repo_root string, repo_list map[string]string) (repo_name, repo_path string, err error) {
	repo_info, err := newRepoInfo(repo_url, repo_root)
	if err != nil {
		return "", "", err
	}

	if _, ok := repo_list[repo_info.repo_name]; ok {
		return "", "", ErrRepoAlreadyFound
	}
	// a failed clone is cleaned up below, which must never touch
	// something that was already there
	if _, err := os.Lstat(repo_info.repo_path); err == nil {
		return "", "", ErrRepoAlreadyFound
	}

	// shallow clone, history isn't needed to explore the code
	clone_cmd := exec.CommandContext(ctx, GIT, CLONE, "--depth", "1", repo_info.repo_url, repo_info.repo_path)
	// jones runs in a TUI, so git has to fail instead of asking for credentials
	clone_cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if os.Getenv("GIT_SSH_COMMAND") == "" {
		clone_cmd.Env = append(clone_cmd.Env, "GIT_SSH_COMMAND=ssh -o BatchMode=yes")
	}
	if output, err := clone_cmd.CombinedOutput(); err != nil {
		// a failed or cancelled clone can leave a partial copy behind
		os.RemoveAll(repo_info.repo_path)
		if ctx.Err() != nil {
			return "", "", context.Cause(ctx)
		}
		return "", "", fmt.Errorf("%w: %w: %s", ErrCloneFailed, err, strings.TrimSpace(string(output)))
	}
	return repo_info.repo_name, repo_info.repo_path, nil
}
