package repo

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/adrg/xdg"
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
			expected_err:  nil,
		},
		{
			repo_url:      "git@github.com:ansht2000/anki-mcp.git",
			expected_user: "ansht2000",
			expected_repo: "anki-mcp",
			expected_err:  nil,
		},
		{
			repo_url:      "haha.funny.poops.git",
			expected_user: "",
			expected_repo: "",
			expected_err:  ErrInvalidRepoURL,
		},
	}

	for _, c := range cases {
		repo_info, err := newRepoInfo(c.repo_url, "")
		if repo_info.user_name != c.expected_user {
			t.Errorf("Got incorrect user %s for repo url: %s", repo_info.user_name, c.repo_url)
		}
		if repo_info.repo_name != c.expected_repo {
			t.Errorf("Got incorrect repo name %s for repo url: %s", repo_info.repo_name, c.repo_url)
		}
		if err != c.expected_err {
			t.Errorf("Function did not throw expected error %v, threw %v instead", c.expected_err, err)
		}
	}
}

func TestBuildRepoTree(t *testing.T) {
	repo := BuildRepoTree("hollywood", filepath.Join(xdg.DataHome, "jones", "repos"))
	// s := "\n"
	// for _, child := range repo.children {
	// 	s += fmt.Sprintf("%s\n", child.item_name)
	// }
	// t.Errorf("%s", s)
	repo_data, _ := json.MarshalIndent(repo,"", "\t")
	os.WriteFile("repo.json", repo_data, 0644)
}
