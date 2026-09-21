package eval

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/ansht2000/jones/internal/agent"
	"github.com/ansht2000/jones/internal/analysis"
	"github.com/ansht2000/jones/internal/llm"
)

// Largest file read when checking citations
const maxCitedFileBytes = 16 * 1024 * 1024

type Score struct {
	// Groups of required phrases the answer mentions, out of all of them
	Mentioned int `json:"mentioned"`
	Required  int `json:"required"`
	// Whether the judge found the answer correct, nil when it wasn't judged
	Correct   *bool `json:"correct,omitempty"`
	Citations int   `json:"citations"`
	// Citations of files that don't exist, or of lines they don't have
	BadCitations int `json:"bad_citations"`
	// Whether the agent verified its answer, only ever set for Verified
	Verified bool `json:"verified"`
}

// Whether the answer mentions every group of required phrases
func (s Score) Complete() bool {
	return s.Mentioned == s.Required
}

func scoreAnswer(question Question, text string, repo *analysis.Analysis, repo_path string) Score {
	score := Score{Required: len(question.Required)}
	lower := strings.ToLower(text)
	for _, group := range question.Required {
		for _, phrase := range group {
			if strings.Contains(lower, strings.ToLower(phrase)) {
				score.Mentioned++
				break
			}
		}
	}
	score.Citations, score.BadCitations = checkCitations(text, repo, repo_path)
	return score
}

// Count an answer's citations, and the ones that don't point at real lines
func checkCitations(text string, repo *analysis.Analysis, repo_path string) (total, bad int) {
	citations, unknown := agent.ParseCitations(text, repo.Files)
	total, bad = len(citations)+len(unknown), len(unknown)
	for _, citation := range citations {
		lines, err := countLines(repo_path, citation.Path)
		if err != nil || citation.Start < 1 || citation.End < citation.Start || citation.End > lines {
			bad++
		}
	}
	return total, bad
}

// Lines in a file, counted the way the agent numbers them
func countLines(repo_path, rel_path string) (int, error) {
	content, err := analysis.ReadRepoFile(repo_path, rel_path, maxCitedFileBytes)
	if err != nil {
		return 0, err
	}
	lines := bytes.Count(content, []byte("\n"))
	if len(content) > 0 && content[len(content)-1] != '\n' {
		lines++
	}
	return lines, nil
}

const JUDGE_PROMPT = `You grade answers to questions about a code repository.

You are given a question, a reference answer written from the repository's code, and an answer to grade.

The answer is correct if it agrees with the reference on the key points and says nothing that contradicts it. It doesn't need the same wording, and extra detail is fine as long as it doesn't contradict the reference. An answer that says it couldn't find the information is not correct.`

// What the judge decides about an answer
type judgement struct {
	Reasoning string `json:"reasoning" description:"how the answer compares with the reference, in one or two sentences"`
	Correct   bool   `json:"correct" description:"true when the answer agrees with the reference on its key points and doesn't contradict it"`
}

// Have the model decide whether an answer agrees with the reference
func judge(ctx context.Context, client llm.Client, question Question, text string) (bool, error) {
	var result judgement
	err := client.GenerateJSON(ctx, llm.Request{
		System: JUDGE_PROMPT,
		Prompt: fmt.Sprintf("Question: %s\n\nReference answer:\n%s\n\nAnswer to grade:\n<answer>\n%s\n</answer>", question.Question, question.Reference, text),
		// grading is a judgement call, so the model gets its default thinking
	}, &result)
	return result.Correct, err
}
