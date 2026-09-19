package agent

import (
	"fmt"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/ansht2000/jones/internal/analysis"
)

var (
	// a path followed by a line or a range of lines, like main.go:12 or internal/app.go:3-7
	citation_pattern = regexp.MustCompile(`([A-Za-z0-9_./\-]+):(\d+)(?:-(\d+))?`)
	file_extension   = regexp.MustCompile(`\.[A-Za-z][A-Za-z0-9]*$`)
)

// Find the citations in an answer, and problems with any that don't point
// at lines the agent read. read_lines has the number of lines read from
// each file, and repo_files is every file in the repo.
func checkCitations(answer string, read_lines map[string]int, repo_files map[string]*analysis.FileAnalysis) ([]Citation, []string) {
	var citations []Citation
	var issues []string
	seen_citations, seen_issues := map[Citation]bool{}, map[string]bool{}
	add_issue := func(issue string) {
		if !seen_issues[issue] {
			seen_issues[issue] = true
			issues = append(issues, issue)
		}
	}

	for _, match := range citation_pattern.FindAllStringSubmatch(answer, -1) {
		cited := match[1]
		// part of a URL, like http://localhost:8080
		if strings.Contains(cited, "//") {
			continue
		}
		cited = strings.TrimPrefix(cited, "./")
		start, _ := strconv.Atoi(match[2])
		end := start
		if match[3] != "" {
			end, _ = strconv.Atoi(match[3])
		}

		file_path, ok := resolveCitedPath(cited, read_lines, repo_files)
		if !ok {
			// things like hosts with ports look like citations too, so only
			// something clearly meant as a file path counts as a bad citation
			if strings.Contains(cited, "/") && file_extension.MatchString(path.Base(cited)) {
				add_issue(fmt.Sprintf("%s is cited but isn't a file in the repository.", cited))
			}
			continue
		}

		citation := Citation{Path: file_path, Start: start, End: end}
		if seen_citations[citation] {
			continue
		}
		seen_citations[citation] = true
		citations = append(citations, citation)

		lines, was_read := read_lines[file_path]
		switch {
		case !was_read:
			add_issue(fmt.Sprintf("%s is cited but wasn't read, so the claim can't come from its code.", citation))
		case start < 1 || end < start:
			add_issue(fmt.Sprintf("%s isn't a valid range of lines.", citation))
		case end > lines:
			add_issue(fmt.Sprintf("%s is cited but only %d lines of %s were read.", citation, lines, file_path))
		}
	}
	return citations, issues
}

// The repo file a citation refers to. Answers sometimes cite only the end of
// a path, which is fine when exactly one file that was read ends with it, or
// failing that, exactly one file in the repo.
func resolveCitedPath(cited string, read_lines map[string]int, repo_files map[string]*analysis.FileAnalysis) (string, bool) {
	if _, ok := repo_files[cited]; ok {
		return cited, true
	}
	if match, ok := uniqueSuffixMatch(cited, read_lines); ok {
		return match, true
	}
	return uniqueSuffixMatch(cited, repo_files)
}

func uniqueSuffixMatch[V any](cited string, files map[string]V) (string, bool) {
	match, matches := "", 0
	for file_path := range files {
		if strings.HasSuffix(file_path, "/"+cited) {
			match = file_path
			matches++
		}
	}
	return match, matches == 1
}
