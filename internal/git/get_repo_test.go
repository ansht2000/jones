package git

import (
	"testing"
	"log"
)

func TestParseRepoName(t *testing.T) {
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
		repo_info, err := parseRepoName(c.repo_url)
		if repo_info.user != c.expected_user {
			log.Printf("Got incorrect user %s for repo url: %s", repo_info.user, c.repo_url)
		}
		if repo_info.repo != c.expected_repo {
			log.Printf("Got incorrect repo name %s for repo url: %s", repo_info.repo, c.repo_url)
		}
		if err != c.expected_err {
			log.Printf("Function did not throw expected error %v, threw %v instead", c.expected_err, err)
		}
	}
}