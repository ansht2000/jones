package agent

import (
	"slices"
	"testing"

	"github.com/ansht2000/jones/internal/analysis"
)

func TestCheckCitations(t *testing.T) {
	read_lines := map[string]int{"main.go": 7, "greet/hello.go": 8}
	repo_files := map[string]*analysis.FileAnalysis{
		"main.go":        {},
		"greet/hello.go": {},
		"greet/bye.go":   {},
	}

	cases := []struct {
		answer    string
		citations []Citation
		issues    []string
	}{
		{"calls Hello (main.go:6)", []Citation{{"main.go", 6, 6}}, nil},
		{"see `greet/hello.go:6-8`.", []Citation{{"greet/hello.go", 6, 8}}, nil},
		{"./main.go:2 declares it", []Citation{{"main.go", 2, 2}}, nil},
		{"main.go:6 and again main.go:6", []Citation{{"main.go", 6, 6}}, nil},
		// just the end of a path, matched to the one file read that ends with it
		{"hello.go:7 prints", []Citation{{"greet/hello.go", 7, 7}}, nil},
		// or to the one file in the repo, which wasn't read
		{"bye.go:3 says bye", []Citation{{"greet/bye.go", 3, 3}}, []string{"greet/bye.go:3 is cited but wasn't read, so the claim can't come from its code."}},
		{"main.go:9", []Citation{{"main.go", 9, 9}}, []string{"main.go:9 is cited but only 7 lines of main.go were read."}},
		{"main.go:5-3", []Citation{{"main.go", 5, 3}}, []string{"main.go:5-3 isn't a valid range of lines."}},
		{"main.go:0", []Citation{{"main.go", 0, 0}}, []string{"main.go:0 isn't a valid range of lines."}},
		{"src/app.go:3 and src/app.go:4", nil, []string{"src/app.go is cited but isn't a file in the repository."}},
		// things that look like citations but aren't
		{"http://localhost:8080/api, 127.0.0.1:80, api.example.com:443, at 10:30, internal/cache:2", nil, nil},
	}

	for _, c := range cases {
		citations, issues := checkCitations(c.answer, read_lines, repo_files)
		if !slices.Equal(citations, c.citations) && !(len(citations) == 0 && len(c.citations) == 0) {
			t.Errorf("%q: expected citations %v, got %v\n", c.answer, c.citations, citations)
		}
		if !slices.Equal(issues, c.issues) && !(len(issues) == 0 && len(c.issues) == 0) {
			t.Errorf("%q: expected issues %q, got %q\n", c.answer, c.issues, issues)
		}
	}
}

func TestParseCitations(t *testing.T) {
	repo_files := map[string]*analysis.FileAnalysis{"main.go": {}, "greet/hello.go": {}}
	answer := "main calls Hello (main.go:6, hello.go:3-5), see src/app.go:2 and main.go:6 again, or src/app.go:9 and localhost:8080."

	citations, unknown := ParseCitations(answer, repo_files)
	if expected := []Citation{{"main.go", 6, 6}, {"greet/hello.go", 3, 5}}; !slices.Equal(citations, expected) {
		t.Errorf("Expected citations %v, got %v\n", expected, citations)
	}
	if !slices.Equal(unknown, []string{"src/app.go"}) {
		t.Errorf("Expected src/app.go to be the only unknown file, got %v\n", unknown)
	}
}

func TestCitationString(t *testing.T) {
	if s := (Citation{"main.go", 6, 6}).String(); s != "main.go:6" {
		t.Errorf("Expected main.go:6, got %s\n", s)
	}
	if s := (Citation{"main.go", 6, 8}).String(); s != "main.go:6-8" {
		t.Errorf("Expected main.go:6-8, got %s\n", s)
	}
}
