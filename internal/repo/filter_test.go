package repo

import (
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"testing"
)

// the paths of every file in a tree, relative to its root
func treeFiles(item *RepoItem, rel_path string) []string {
	files := []string{}
	for _, child := range item.Children {
		child_rel_path := path.Join(rel_path, child.ItemName)
		if child.IsDir {
			files = append(files, treeFiles(child, child_rel_path)...)
		} else {
			files = append(files, child_rel_path)
		}
	}
	return files
}

func writeFiles(t *testing.T, root string, files ...string) {
	t.Helper()
	for _, file := range files {
		file_path := filepath.Join(root, filepath.FromSlash(file))
		if err := os.MkdirAll(filepath.Dir(file_path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file_path, []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	if _, err := exec.LookPath(GIT); err != nil {
		t.Skip("git is not installed")
	}
	if output, err := exec.Command(GIT, append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, output)
	}
}

func TestBuildRepoTreeTrackedFiles(t *testing.T) {
	root := filepath.Join(t.TempDir(), "tracked")
	writeFiles(t, root, "main.go", "internal/util.go", "vendor/dep/dep.go", "docs/guide.md")
	runGit(t, root, "init", "-q")
	runGit(t, root, "add", ".")
	// created after git add, so they aren't tracked
	writeFiles(t, root, "build/output.bin", "internal/scratch.go", "notes.txt")

	// vendor is tracked but on the ignore list
	expected := []string{"docs/guide.md", "internal/util.go", "main.go"}
	if files := treeFiles(BuildRepoTree("tracked", root), ""); !slices.Equal(files, expected) {
		t.Errorf("Expected files %v, got %v\n", expected, files)
	}
}

func TestBuildRepoTreeIgnoreList(t *testing.T) {
	// not a git repo, so everything except the ignore list is scanned
	root := filepath.Join(t.TempDir(), "plain")
	writeFiles(t, root, "main.go", "node_modules/pkg/index.js", "src/__pycache__/app.pyc", "src/app.py")

	expected := []string{"main.go", "src/app.py"}
	if files := treeFiles(BuildRepoTree("plain", root), ""); !slices.Equal(files, expected) {
		t.Errorf("Expected files %v, got %v\n", expected, files)
	}
}

func TestBuildRepoTreeInsideOtherRepo(t *testing.T) {
	// a plain directory inside a git repo shouldn't be limited to the
	// outer repo's tracked files, none of which are in it
	outer := t.TempDir()
	runGit(t, outer, "init", "-q")
	inner := filepath.Join(outer, "inner")
	writeFiles(t, inner, "main.go")

	expected := []string{"main.go"}
	if files := treeFiles(BuildRepoTree("inner", inner), ""); !slices.Equal(files, expected) {
		t.Errorf("Expected files %v, got %v\n", expected, files)
	}
}
