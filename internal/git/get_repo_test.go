package git

import (
	"testing"
	"log"
)

func TestGetRepo(t *testing.T) {
	cases := []struct{
		repo_url string
		expected_user string
		expected_repo string
		expected_err error
	}{
		{
			repo_url: "https://github.com/ansht2000/anki-mcp.git",
			expected_user: "ansht2000",
			expected_repo: "anki-mcp",
			expected_err: nil,
		},
		{
			repo_url: "git@github.com:ansht2000/anki-mcp.git",
			expected_user: "ansht2000",
			expected_repo: "anki-mcp",
			expected_err: nil,
		},
		{
			repo_url: "haha.funny.poops.git",
			expected_user: "",
			expected_repo: "",
			expected_err: ErrInvalidRepoURL,
		},
	}

	for _, c := range cases {
		actual_user, actual_repo, err := parseRepoName(c.repo_url)
		if actual_user != c.expected_user {
			log.Printf("Got incorrect user %s for repo url: %s", actual_user, c.repo_url)
		}
		if actual_repo != c.expected_repo {
			log.Printf("Got incorrect repo name %s for repo url: %s", actual_repo, c.repo_url)
		}
		if err != c.expected_err {
			log.Printf("Function did not throw expected error %v, threw %v instead", c.expected_err, err)
		}
	}
}