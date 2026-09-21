package agent

import (
	"fmt"
	"slices"
	"strings"

	"github.com/ansht2000/jones/internal/analysis"
)

const (
	// Most characters of a summary shown in the file list
	maxListSummary = 120
	// Most key symbols shown for each file in the file list
	maxListSymbols = 4
)

// One line per file and directory in the repo, sorted by path, with the
// start of its summary. Files also list their key symbols, which helps the
// model find where something is defined.
func FormatFileList(repo *analysis.Analysis) string {
	paths := make([]string, 0, len(repo.Files)+len(repo.Dirs))
	for dir_path := range repo.Dirs {
		paths = append(paths, dir_path)
	}
	for file_path := range repo.Files {
		paths = append(paths, file_path)
	}
	slices.Sort(paths)

	var list strings.Builder
	for _, entry_path := range paths {
		if dir, ok := repo.Dirs[entry_path]; ok {
			fmt.Fprintf(&list, "%s/: %s\n", entry_path, shorten(dir.Summary))
			continue
		}
		summary := repo.Files[entry_path].Summary
		fmt.Fprintf(&list, "%s: %s", entry_path, shorten(summary.Purpose))
		if len(summary.KeySymbols) > 0 {
			fmt.Fprintf(&list, " (%s)", strings.Join(summary.KeySymbols[:min(len(summary.KeySymbols), maxListSymbols)], ", "))
		}
		list.WriteString("\n")
	}
	return list.String()
}

// The first sentence of a summary, cut to maxListSummary characters
func shorten(summary string) string {
	summary = strings.Join(strings.Fields(summary), " ")
	if end := strings.Index(summary, ". "); end >= 0 {
		summary = summary[:end+1]
	}
	if runes := []rune(summary); len(runes) > maxListSummary {
		summary = string(runes[:maxListSummary]) + "..."
	}
	return summary
}

func decidePrompt(s *qaStore, options Options) string {
	var prompt strings.Builder
	fmt.Fprintf(&prompt, "Question: %s\n\n", s.question)
	fmt.Fprintf(&prompt, "Repository overview:\n%s\n\n", s.repo.Overview)
	fmt.Fprintf(&prompt, "Files and directories (path: summary):\n%s\n", s.file_list)
	fmt.Fprintf(&prompt, "Files you have read:\n%s", orNone(formatFiles(s.read), "None yet.\n"))
	if len(s.notes) > 0 {
		prompt.WriteString("\nNotes:\n")
		for _, note := range s.notes {
			fmt.Fprintf(&prompt, "- %s\n", note)
		}
	}
	fmt.Fprintf(&prompt, "\nYou can read files %d more times, up to %d at a time.", s.max_rounds-s.rounds, options.MaxFilesPerRound)
	return prompt.String()
}

func answerPrompt(s *qaStore) string {
	var prompt strings.Builder
	fmt.Fprintf(&prompt, "Question: %s\n\n", s.question)
	fmt.Fprintf(&prompt, "Files:\n%s", orNone(formatFiles(s.read), "No files were read.\n"))
	if len(s.feedback) > 0 {
		prompt.WriteString("\nAn earlier answer was rejected because of these problems:\n")
		for _, problem := range s.feedback {
			fmt.Fprintf(&prompt, "- %s\n", problem)
		}
	}
	return prompt.String()
}

func verifyPrompt(question, answer, files string) string {
	return fmt.Sprintf("Question: %s\n\nAnswer:\n<answer>\n%s\n</answer>\n\nFiles:\n%s", question, answer, orNone(files, "No files were read.\n"))
}

// The files read, with line numbers, each wrapped in a file tag
func formatFiles(files []*readFile) string {
	var formatted strings.Builder
	for _, file := range files {
		fmt.Fprintf(&formatted, "<file path=%q", file.path)
		if file.truncated {
			fmt.Fprintf(&formatted, " note=\"only the first %d lines are shown\"", file.lines)
		}
		fmt.Fprintf(&formatted, ">\n%s</file>\n", file.numbered)
	}
	return formatted.String()
}

func orNone(s, none string) string {
	if s == "" {
		return none
	}
	return s
}
