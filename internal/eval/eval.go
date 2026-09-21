// Package eval measures how well jones answers questions about real repos.
// Each question comes with a reference answer written from the repo's code
// and phrases a correct answer has to mention. jones' agent is compared with
// a baseline that answers in one call from the repo's summaries, like a tool
// that never reads the code would, scoring whether answers are correct,
// whether their citations point at real lines, and how long they take.
package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ansht2000/jones/internal/agent"
	"github.com/ansht2000/jones/internal/analysis"
	"github.com/ansht2000/jones/internal/llm"
)

type Repo struct {
	// Short name, used as the name of the repo's analysis
	Name string `json:"name"`
	// Go module and version, so every run evaluates the same code
	Module  string `json:"module"`
	Version string `json:"version"`
}

type Question struct {
	Repo     string `json:"repo"`
	Question string `json:"question"`
	// A correct answer, written from the repo's code
	Reference string `json:"reference"`
	// A correct answer mentions one of the phrases in each group, ignoring case
	Required [][]string `json:"required"`
	// Files that hold the answer
	Evidence []string `json:"evidence"`
}

type Dataset struct {
	Repos     []Repo     `json:"repos"`
	Questions []Question `json:"questions"`
}

func LoadDataset(path string) (*Dataset, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var dataset Dataset
	if err := json.Unmarshal(data, &dataset); err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	return &dataset, dataset.Validate()
}

// Check that every question is about a known repo and can be scored
func (d *Dataset) Validate() error {
	repos := map[string]bool{}
	for _, repo := range d.Repos {
		if repo.Name == "" || repo.Module == "" || repo.Version == "" {
			return fmt.Errorf("repo %q needs a name, module, and version", repo.Name)
		}
		repos[repo.Name] = true
	}
	for _, question := range d.Questions {
		if !repos[question.Repo] {
			return fmt.Errorf("question %q is about unknown repo %q", question.Question, question.Repo)
		}
		if question.Reference == "" || len(question.Required) == 0 {
			return fmt.Errorf("question %q needs a reference answer and required phrases", question.Question)
		}
		for _, group := range question.Required {
			if len(group) == 0 {
				return fmt.Errorf("question %q has an empty group of required phrases", question.Question)
			}
		}
	}
	return nil
}

// A way of answering questions
type System string

const (
	// One call with the repo's overview and file summaries, without reading any code
	Baseline System = "baseline"
	// jones' agent, keeping its first answer without checking it
	Agent System = "agent"
	// jones' agent as it normally runs, checking its answer and redoing it if needed
	Verified System = "agent + verify"
)

var SYSTEMS = []System{Baseline, Agent, Verified}

const BASELINE_PROMPT = `You answer questions about a code repository.

You are given an overview of the repository and a list of its files and directories with a short summary of each. Answer the question, citing the relevant code as path:line or path:start-end.`

type Options struct {
	Client llm.Client
	// Where analyses are saved while evaluating, it should start empty so
	// the first analysis of each repo is timed from scratch
	WorkDir string
	Systems []System
	// Have the model judge each answer against the reference
	Judge bool
	// Also time analyzing each repo with one model call at a time
	Sequential bool
	// Called with a line describing each step
	Log func(string)
}

type RepoResult struct {
	Name  string `json:"name"`
	Files int    `json:"files"`
	Dirs  int    `json:"dirs"`
	// Analyzing the repo from scratch, and again with nothing changed
	FirstRun  time.Duration `json:"first_run"`
	CachedRun time.Duration `json:"cached_run"`
	// Analyzing the repo from scratch one model call at a time, with Options.Sequential
	SequentialRun time.Duration `json:"sequential_run,omitempty"`
}

type AnswerResult struct {
	Repo     string        `json:"repo"`
	Question string        `json:"question"`
	System   System        `json:"system"`
	Answer   string        `json:"answer"`
	Error    string        `json:"error,omitempty"`
	Latency  time.Duration `json:"latency"`
	Score    Score         `json:"score"`
}

type Result struct {
	Repos   []RepoResult   `json:"repos"`
	Answers []AnswerResult `json:"answers"`
}

// Analyze each repo, then answer every question about it with each system.
// repo_paths has where each repo's code is. A question that fails is recorded
// and the evaluation carries on, but failing to analyze a repo stops it.
func Run(ctx context.Context, dataset *Dataset, repo_paths map[string]string, options Options) (*Result, error) {
	log := options.Log
	if log == nil {
		log = func(string) {}
	}

	result := &Result{}
	for _, repo := range dataset.Repos {
		repo_path, ok := repo_paths[repo.Name]
		if !ok {
			return nil, fmt.Errorf("no code for repo %s", repo.Name)
		}

		repo_result := RepoResult{Name: repo.Name}
		analysis_options := analysis.DefaultOptions()
		analysis_options.SaveDir = filepath.Join(options.WorkDir, "analysis")
		if options.Sequential {
			log(fmt.Sprintf("Analyzing %s one call at a time", repo.Name))
			sequential_options := analysis_options
			sequential_options.SaveDir = ""
			sequential_options.Concurrency = 1
			start := time.Now()
			if _, err := analysis.Analyze(ctx, options.Client, repo.Name, repo_path, sequential_options); err != nil {
				return nil, fmt.Errorf("analyzing %s: %w", repo.Name, err)
			}
			repo_result.SequentialRun = time.Since(start)
		}

		log(fmt.Sprintf("Analyzing %s", repo.Name))
		start := time.Now()
		repo_analysis, err := analysis.Analyze(ctx, options.Client, repo.Name, repo_path, analysis_options)
		if err != nil {
			return nil, fmt.Errorf("analyzing %s: %w", repo.Name, err)
		}
		repo_result.FirstRun = time.Since(start)
		repo_result.Files, repo_result.Dirs = len(repo_analysis.Files), len(repo_analysis.Dirs)

		start = time.Now()
		if _, err := analysis.Analyze(ctx, options.Client, repo.Name, repo_path, analysis_options); err != nil {
			return nil, fmt.Errorf("analyzing %s again: %w", repo.Name, err)
		}
		repo_result.CachedRun = time.Since(start)
		result.Repos = append(result.Repos, repo_result)

		for _, question := range dataset.Questions {
			if question.Repo != repo.Name {
				continue
			}
			for _, system := range options.Systems {
				if ctx.Err() != nil {
					return nil, context.Cause(ctx)
				}
				log(fmt.Sprintf("%s: %s (%s)", repo.Name, question.Question, system))
				result.Answers = append(result.Answers, evaluate(ctx, system, question, repo_analysis, repo_path, options))
			}
		}
	}
	return result, nil
}

// Answer a question with a system and score the answer
func evaluate(ctx context.Context, system System, question Question, repo *analysis.Analysis, repo_path string, options Options) AnswerResult {
	result := AnswerResult{Repo: question.Repo, Question: question.Question, System: system}
	start := time.Now()
	text, verified, err := answer(ctx, system, options.Client, repo, repo_path, question.Question)
	result.Latency = time.Since(start)
	if err != nil {
		result.Error = err.Error()
		return result
	}

	result.Answer = text
	result.Score = scoreAnswer(question, text, repo, repo_path)
	result.Score.Verified = verified
	if options.Judge {
		correct, err := judge(ctx, options.Client, question, text)
		if err != nil {
			result.Error = "judging: " + err.Error()
		} else {
			result.Score.Correct = &correct
		}
	}
	return result
}

// Answer a question with a system, verified says whether the agent verified its answer
func answer(ctx context.Context, system System, client llm.Client, repo *analysis.Analysis, repo_path, question string) (text string, verified bool, err error) {
	if system == Baseline {
		text, err = client.Generate(ctx, llm.Request{
			System:         BASELINE_PROMPT,
			Prompt:         fmt.Sprintf("Question: %s\n\nRepository overview:\n%s\n\nFiles and directories (path: summary):\n%s", question, repo.Overview, agent.FormatFileList(repo)),
			ThinkingBudget: llm.Ptr[int32](0),
		})
		return text, false, err
	}

	options := agent.DefaultOptions()
	if system == Agent {
		options.VerifyWithModel = false
		options.MaxRetries = 0
	}
	result, err := agent.Ask(ctx, client, repo, repo_path, question, options)
	if err != nil {
		return "", false, err
	}
	return result.Text, system == Verified && result.Verified, nil
}
