package analysis

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestReadRepoFile(t *testing.T) {
	root := makeFixture(t)

	content, err := ReadRepoFile(root, "internal/util/util.go", 12)
	if err != nil || string(content) != "package util" {
		t.Errorf("Expected the first 12 bytes of util.go, got %q, %v\n", content, err)
	}

	for _, rel_path := range []string{"", ".", "..", "../demo/main.go", "internal/../../main.go", "/etc/passwd"} {
		if _, err := ReadRepoFile(root, rel_path, 100); !errors.Is(err, ErrOutsideRepo) {
			t.Errorf("Expected error %v for %q, got %v\n", ErrOutsideRepo, rel_path, err)
		}
	}
	for _, rel_path := range []string{"missing.go", "internal", "main.go/extra"} {
		if _, err := ReadRepoFile(root, rel_path, 100); err == nil {
			t.Errorf("Expected an error for %q\n", rel_path)
		}
	}
}

func TestReadRepoFileSymlinks(t *testing.T) {
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("TOP SECRET"), 0644); err != nil {
		t.Fatal(err)
	}
	root := makeFixture(t)
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(root, "link.txt")); err != nil {
		t.Skipf("can't create symlinks: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "linked_dir")); err != nil {
		t.Fatal(err)
	}

	// a symlink at the end of the path, and one partway along it
	for _, rel_path := range []string{"link.txt", "linked_dir/secret.txt"} {
		if content, err := ReadRepoFile(root, rel_path, 100); err == nil {
			t.Errorf("Expected %s not to be read through a symlink, got %q\n", rel_path, content)
		}
	}
}
