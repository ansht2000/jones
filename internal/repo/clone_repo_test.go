package repo

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestCloneRepoCleansUpFailures(t *testing.T) {
	if _, err := exec.LookPath(GIT); err != nil {
		t.Skip("git is not installed")
	}
	repo_root := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// nothing listens on port 1, so the clone fails right away without prompting
	_, _, err := CloneRepo(ctx, "https://127.0.0.1:1/foo/demo.git", repo_root, map[string]string{})
	if !errors.Is(err, ErrCloneFailed) {
		t.Errorf("Expected error %v, got %v\n", ErrCloneFailed, err)
	}
	if _, err := os.Lstat(filepath.Join(repo_root, "demo")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Expected the failed clone to be removed, got %v\n", err)
	}
}

func TestCloneRepoAlreadyThere(t *testing.T) {
	repo_root := t.TempDir()
	existing := filepath.Join(repo_root, "demo")
	if err := os.WriteFile(filepath.Join(repo_root, "keep.txt"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(existing, 0755); err != nil {
		t.Fatal(err)
	}

	for _, repo_list := range []map[string]string{{"demo": existing}, {}} {
		if _, _, err := CloneRepo(context.Background(), "https://github.com/foo/demo.git", repo_root, repo_list); !errors.Is(err, ErrRepoAlreadyFound) {
			t.Errorf("Expected error %v, got %v\n", ErrRepoAlreadyFound, err)
		}
	}
	// nothing that was already there is touched
	if _, err := os.Stat(existing); err != nil {
		t.Errorf("Expected the existing directory to be left alone, got %v\n", err)
	}
}

func TestCloneRepoCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	repo_root := t.TempDir()

	_, _, err := CloneRepo(ctx, "https://127.0.0.1:1/foo/demo.git", repo_root, map[string]string{})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Expected error %v, got %v\n", context.Canceled, err)
	}
	if _, err := os.Lstat(filepath.Join(repo_root, "demo")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Expected nothing to be left behind, got %v\n", err)
	}
}
