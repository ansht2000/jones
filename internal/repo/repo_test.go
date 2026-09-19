package repo

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestParseRepoName(t *testing.T) {
	cases := []struct {
		repo_url      string
		expected_user string
		expected_repo string
		expected_err  error
	}{
		{
			repo_url:      "https://github.com/ansht2000/anki-mcp.git",
			expected_user: "ansht2000",
			expected_repo: "anki-mcp",
		},
		{
			repo_url:      "git@github.com:ansht2000/anki-mcp.git",
			expected_user: "ansht2000",
			expected_repo: "anki-mcp",
		},
		{
			// names ending in characters from ".git" must not be over-trimmed
			repo_url:      "https://github.com/foo/digit.git",
			expected_user: "foo",
			expected_repo: "digit",
		},
		{
			repo_url:      "git@github.com:foo/fit.git",
			expected_user: "foo",
			expected_repo: "fit",
		},
		{
			repo_url:      "https://github.com/charmbracelet/bubbletea",
			expected_user: "charmbracelet",
			expected_repo: "bubbletea",
		},
		{
			repo_url:      "https://github.com/charmbracelet/bubbletea/",
			expected_user: "charmbracelet",
			expected_repo: "bubbletea",
		},
		{
			repo_url:     "haha.funny.poops.git",
			expected_err: ErrInvalidRepoURL,
		},
		{
			repo_url:     "https:",
			expected_err: ErrInvalidRepoURL,
		},
		{
			repo_url:     "https://github.com",
			expected_err: ErrInvalidRepoURL,
		},
		{
			repo_url:     "https://github.com/onlyuser",
			expected_err: ErrInvalidRepoURL,
		},
		{
			repo_url:     "git@github.com",
			expected_err: ErrInvalidRepoURL,
		},
		{
			repo_url:     "git@github.com:foo/.git",
			expected_err: ErrInvalidRepoURL,
		},
		// names that would put the clone in the repo root or above it
		{
			repo_url:     "https://github.com/foo/..",
			expected_err: ErrInvalidRepoURL,
		},
		{
			repo_url:     "https://github.com/foo/..git",
			expected_err: ErrInvalidRepoURL,
		},
		{
			repo_url:     "git@github.com:foo/.",
			expected_err: ErrInvalidRepoURL,
		},
	}

	for _, c := range cases {
		repo_info, err := newRepoInfo(c.repo_url, "")
		if repo_info.user_name != c.expected_user {
			t.Errorf("Got incorrect user %q for repo url: %s", repo_info.user_name, c.repo_url)
		}
		if repo_info.repo_name != c.expected_repo {
			t.Errorf("Got incorrect repo name %q for repo url: %s", repo_info.repo_name, c.repo_url)
		}
		if !errors.Is(err, c.expected_err) {
			t.Errorf("Function did not throw expected error %v, threw %v instead for repo url: %s", c.expected_err, err, c.repo_url)
		}
	}
}

// makeFixtureRepo creates:
//
//	fixture/
//	  .git/HEAD   (ignored)
//	  a.txt
//	  dir/b.go
func makeFixtureRepo(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "fixture")
	files := []string{
		filepath.Join(root, ".git", "HEAD"),
		filepath.Join(root, "a.txt"),
		filepath.Join(root, "dir", "b.go"),
	}
	for _, f := range files {
		if err := os.MkdirAll(filepath.Dir(f), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f, []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestBuildRepoTree(t *testing.T) {
	root := makeFixtureRepo(t)
	tree := BuildRepoTree("fixture", root)

	if tree.Err != "" {
		t.Fatalf("Unexpected error building tree: %s", tree.Err)
	}
	if len(tree.Children) != 2 {
		t.Fatalf("Expected 2 children (.git ignored), got %d", len(tree.Children))
	}

	a, dir := tree.Children[0], tree.Children[1]
	if a.ItemName != "a.txt" || a.IsDir {
		t.Errorf("Expected file a.txt, got %+v", a)
	}
	if dir.ItemName != "dir" || !dir.IsDir {
		t.Errorf("Expected dir dir, got %+v", dir)
	}
	if len(dir.Children) != 1 || dir.Children[0].ItemName != "b.go" {
		t.Errorf("Expected dir to contain only b.go, got %+v", dir.Children)
	}
	if want := filepath.Join(root, "dir", "b.go"); dir.Children[0].ItemPath != want {
		t.Errorf("Expected path %s, got %s", want, dir.Children[0].ItemPath)
	}
}

func TestBuildRepoTreeMissingDir(t *testing.T) {
	tree := BuildRepoTree("missing", filepath.Join(t.TempDir(), "does-not-exist"))
	if tree.Err == "" {
		t.Error("Expected an error for a missing repo directory")
	}
}

func TestBuildRepoString(t *testing.T) {
	tree := BuildRepoTree("fixture", makeFixtureRepo(t))
	expected := "fixture\n|---a.txt\n|---dir\n|\t|---b.go\n"
	if got := BuildRepoString(tree); got != expected {
		t.Errorf("Got repo string:\n%s\nexpected:\n%s", got, expected)
	}
}
