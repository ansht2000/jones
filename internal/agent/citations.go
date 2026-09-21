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

// Find the citations in an answer. Cited paths are matched to files in the
// repo, and the ones clearly meant as files that aren't in it are returned
// in unknown. Each citation and unknown path is listed once, in order.
func ParseCitations(answer string, repo_files map[string]*analysis.FileAnalysis) (citations []Citation, unknown []string) {
	seen_citations, seen_unknown := map[Citation]bool{}, map[string]bool{}
	eachCitation(answer, nil, repo_files, func(citation Citation, cited string, ok bool) {
		if !ok {
			if !seen_unknown[cited] {
				seen_unknown[cited] = true
				unknown = append(unknown, cited)
			}
			return
		}
		if !seen_citations[citation] {
			seen_citations[citation] = true
			citations = append(citations, citation)
		}
	})
	return citations, unknown
}

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

	eachCitation(answer, read_lines, repo_files, func(citation Citation, cited string, ok bool) {
		if !ok {
			add_issue(fmt.Sprintf("%s is cited but isn't a file in the repository.", cited))
			return
		}
		if seen_citations[citation] {
			return
		}
		seen_citations[citation] = true
		citations = append(citations, citation)

		lines, was_read := read_lines[citation.Path]
		switch {
		case !was_read:
			add_issue(fmt.Sprintf("%s is cited but wasn't read, so the claim can't come from its code.", citation))
		case citation.Start < 1 || citation.End < citation.Start:
			add_issue(fmt.Sprintf("%s isn't a valid range of lines.", citation))
		case citation.End > lines:
			add_issue(fmt.Sprintf("%s is cited but only %d lines of %s were read.", citation, lines, citation.Path))
		}
	})
	return citations, issues
}

// Calls found for each citation in an answer, in order. When a cited path is
// clearly meant as a file but isn't one in the repo, ok is false and only
// cited is set.
func eachCitation(answer string, read_lines map[string]int, repo_files map[string]*analysis.FileAnalysis, found func(citation Citation, cited string, ok bool)) {
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
				found(Citation{}, cited, false)
			}
			continue
		}
		found(Citation{Path: file_path, Start: start, End: end}, cited, true)
	}
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
