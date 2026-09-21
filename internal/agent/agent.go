// Package agent answers questions about a repository. Using the repo's
// analysis, the agent decides which files to read, reads them, and answers
// from their contents with a citation for every claim. The answer is then
// checked: citations must point at lines the agent actually read, and the
// model confirms the cited code backs up each claim. An answer that fails
// is sent back to be redone.
package agent

import (
	"context"
	"errors"
	"fmt"

	"github.com/ansht2000/jones/internal/analysis"
	"github.com/ansht2000/jones/internal/llm"
)

var ErrNoAnalysis = errors.New("repo hasn't been analyzed")

type Options struct {
	// Most rounds of reading files before the agent has to answer
	MaxReadRounds int
	// Most files read in one round
	MaxFilesPerRound int
	// Most bytes of file contents read in total, 0 means no limit
	MaxContextBytes int
	// Times an answer that fails verification is redone
	MaxRetries int
	// Have the model check that the cited code backs up each claim, on top
	// of checking that citations point at lines that were read
	VerifyWithModel bool
	// Tokens the model can spend thinking per call, see llm.Request
	ThinkingBudget *int32
	// Called as the agent works, from one goroutine at a time
	OnEvent func(Event)
}

func DefaultOptions() Options {
	return Options{
		MaxReadRounds:    4,
		MaxFilesPerRound: 5,
		MaxContextBytes:  256 * 1024,
		MaxRetries:       1,
		VerifyWithModel:  true,
		ThinkingBudget:   llm.Ptr[int32](0),
	}
}

func (o Options) emit(event Event) {
	if o.OnEvent != nil {
		o.OnEvent(event)
	}
}

type EventKind string

const (
	// The agent chose to read Paths, or to answer when Paths is empty, because of Reason
	EventDecided EventKind = "decided"
	// The agent read Paths, and Issues lists files it couldn't read and why
	EventRead EventKind = "read"
	// An answer is starting, replacing anything streamed before it
	EventAnswerStart EventKind = "answer_start"
	// Text is the next part of the answer
	EventAnswerChunk EventKind = "answer_chunk"
	// The finished answer is being checked
	EventChecking EventKind = "checking"
	// The answer failed verification because of Issues and will be redone
	EventRetry EventKind = "retry"
	// Verification is done, and Issues lists problems left in the final answer
	EventVerified EventKind = "verified"
)

type Event struct {
	Kind   EventKind
	Reason string
	Paths  []string
	Text   string
	Issues []string
}

type Answer struct {
	Text      string
	Citations []Citation
	// Files the agent read, in the order it read them
	FilesRead []string
	// Whether the citations point at lines that were read and, with
	// VerifyWithModel, the model found every claim backed by the code
	Verified bool
	// Problems verification found in the answer
	Issues []string
}

// A range of lines in a file that an answer cites
type Citation struct {
	Path  string
	Start int
	End   int
}

func (c Citation) String() string {
	if c.Start == c.End {
		return fmt.Sprintf("%s:%d", c.Path, c.Start)
	}
	return fmt.Sprintf("%s:%d-%d", c.Path, c.Start, c.End)
}

// What the model decides to do next
type decision struct {
	Reason string   `json:"reason" description:"why this is the best next step, in one sentence"`
	Action string   `json:"action" enum:"read,answer"`
	Paths  []string `json:"paths,omitempty" description:"files to read, copied exactly from the file list, when the action is read"`
}

// What the model finds when it checks an answer
type verdict struct {
	Issues    []string `json:"issues" description:"each claim the cited code doesn't back up and what is wrong with it, empty if there are none"`
	Supported bool     `json:"supported" description:"true when every claim is backed by the code it cites"`
}

// Answer a question about the repo at repo_path, using its analysis to
// decide which files to read
func Ask(ctx context.Context, client llm.Client, repo *analysis.Analysis, repo_path, question string, options Options) (*Answer, error) {
	if repo == nil {
		return nil, ErrNoAnalysis
	}

	store := &qaStore{
		question:   question,
		repo_path:  repo_path,
		repo:       repo,
		file_list:  FormatFileList(repo),
		read_set:   map[string]*readFile{},
		max_rounds: options.MaxReadRounds,
	}
	if _, err := newAgentFlow(client, options).Run(ctx, store); err != nil {
		return nil, err
	}

	answer := &Answer{
		Text:      store.answer,
		Citations: store.citations,
		Verified:  store.verified,
		Issues:    store.issues,
	}
	for _, file := range store.read {
		answer.FilesRead = append(answer.FilesRead, file.path)
	}
	return answer, nil
}
