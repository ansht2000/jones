package agent

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/ansht2000/jones/internal/actorflow"
	"github.com/ansht2000/jones/internal/analysis"
	"github.com/ansht2000/jones/internal/llm"
	"github.com/ansht2000/jones/internal/retry"
)

const (
	actionRead   actorflow.Action = "read"
	actionAnswer actorflow.Action = "answer"
	actionRetry  actorflow.Action = "retry"
	actionDone   actorflow.Action = "done"
)

// Most of one file the agent reads, longer files are cut off at a line break
const maxReadFileBytes = 64 * 1024

// The client only retries a stream before its first chunk, so an answer that
// breaks off partway is started over, and EventAnswerStart clears what was shown
var answerRetry = retry.NewRetryConfig(
	retry.WithInitialDelay(time.Second),
	retry.WithMaxRetries(2),
	retry.WithRetryIf(func(err error) bool {
		return errors.Is(err, llm.ErrStreamInterrupted)
	}),
)

// The shared store for answering a question
type qaStore struct {
	question  string
	repo_path string
	repo      *analysis.Analysis
	// the repo's files and directories with their summaries
	file_list string
	// files read so far, in the order they were read
	read       []*readFile
	read_set   map[string]*readFile
	read_bytes int
	rounds     int
	max_rounds int
	// things to tell the model when deciding, like files that couldn't be read
	notes    []string
	decision decision
	// problems with the last answer, for the next one to fix
	feedback  []string
	retries   int
	answer    string
	citations []Citation
	issues    []string
	verified  bool
}

type readFile struct {
	path string
	// the contents with line numbers
	numbered string
	lines    int
	// whether the file was cut off
	truncated bool
	bytes     int
}

func newAgentFlow(client llm.Client, options Options) *actorflow.Flow[qaStore] {
	decide := decideNode(client, options)
	read := readNode(options)
	answer := answerNode(client, options)
	verify := verifyNode(client, options)

	decide.On(actionRead, read).Then(decide)
	decide.On(actionAnswer, answer).Then(verify)
	verify.On(actionRetry, decide)
	// every path through the flow is bounded by the options,
	// this only guards against a mistake causing an endless loop
	max_steps := 10 * (options.MaxReadRounds + options.MaxRetries + 2)
	return &actorflow.Flow[qaStore]{Start: decide, MaxSteps: max_steps}
}

type decideJob struct {
	prompt string
	// out of reading rounds or room, so answer without asking the model
	forced bool
}

// Decide whether to read more files or answer
func decideNode(client llm.Client, options Options) *actorflow.Node[qaStore, decideJob, decision] {
	return &actorflow.Node[qaStore, decideJob, decision]{
		Prep: func(ctx context.Context, s *qaStore) (decideJob, error) {
			context_full := options.MaxContextBytes > 0 && s.read_bytes >= options.MaxContextBytes
			if s.rounds >= s.max_rounds || context_full {
				return decideJob{forced: true}, nil
			}
			return decideJob{prompt: decidePrompt(s, options)}, nil
		},
		Exec: func(ctx context.Context, job decideJob) (decision, error) {
			if job.forced {
				return decision{Reason: "Read as many files as allowed.", Action: string(actionAnswer)}, nil
			}
			var next decision
			err := client.GenerateJSON(ctx, llm.Request{
				System:         llm.DECIDE_PROMPT,
				Prompt:         job.prompt,
				ThinkingBudget: options.ThinkingBudget,
			}, &next)
			return next, err
		},
		Fallback: func(ctx context.Context, job decideJob, err error) (decision, error) {
			// answering with what has been read beats failing
			if llm.IsResponseProblem(err) {
				return decision{Reason: "Couldn't decide what to read next.", Action: string(actionAnswer)}, nil
			}
			return decision{}, err
		},
		Post: func(ctx context.Context, s *qaStore, job decideJob, next decision) (actorflow.Action, error) {
			// anything but a read with paths to read means answering
			if next.Action != string(actionRead) || len(next.Paths) == 0 {
				next.Action = string(actionAnswer)
				next.Paths = nil
			}
			s.decision = next
			options.emit(Event{Kind: EventDecided, Reason: next.Reason, Paths: next.Paths})
			return actorflow.Action(next.Action), nil
		},
	}
}

type readJob struct {
	path      string
	repo_path string
	// why the file can't be read, empty if it can
	reject string
}

type readResult struct {
	file   *readFile
	reject string
}

// Read the files the agent chose, all at once
func readNode(options Options) *actorflow.BatchNode[qaStore, readJob, readResult] {
	return &actorflow.BatchNode[qaStore, readJob, readResult]{
		Prep: func(ctx context.Context, s *qaStore) ([]readJob, error) {
			jobs := []readJob{}
			seen := map[string]bool{}
			planned_bytes := s.read_bytes
			for _, requested := range s.decision.Paths {
				if len(jobs) == options.MaxFilesPerRound {
					break
				}
				file_path, reject := s.checkPath(requested)
				if seen[file_path] {
					continue
				}
				seen[file_path] = true

				if reject == "" && options.MaxContextBytes > 0 {
					planned_bytes += int(min(s.repo.Files[file_path].Size, maxReadFileBytes))
					if planned_bytes > options.MaxContextBytes {
						reject = "wasn't read, there's no room left for more files"
					}
				}
				jobs = append(jobs, readJob{path: file_path, repo_path: s.repo_path, reject: reject})
			}
			return jobs, nil
		},
		Exec: func(ctx context.Context, job readJob) (readResult, error) {
			if job.reject != "" {
				return readResult{reject: job.reject}, nil
			}
			// one byte past the limit shows whether the file was cut off
			content, err := analysis.ReadRepoFile(job.repo_path, job.path, maxReadFileBytes+1)
			if err != nil {
				return readResult{reject: "couldn't be read"}, nil
			}
			return readResult{file: newReadFile(job.path, content)}, nil
		},
		Post: func(ctx context.Context, s *qaStore, jobs []readJob, results []readResult) (actorflow.Action, error) {
			read, problems := []string{}, []string{}
			for i, job := range jobs {
				if file := results[i].file; file != nil {
					s.read = append(s.read, file)
					s.read_set[file.path] = file
					s.read_bytes += file.bytes
					read = append(read, file.path)
				} else {
					problems = append(problems, job.path+" "+results[i].reject)
				}
			}
			s.notes = append(s.notes, problems...)
			s.rounds++
			options.emit(Event{Kind: EventRead, Paths: read, Issues: problems})
			return actorflow.Default, nil
		},
		// the model picks a handful of files, so they can all be read at once
		Concurrency: -1,
	}
}

// The repo file a requested path refers to, or why it can't be read
func (s *qaStore) checkPath(requested string) (string, string) {
	file_path := path.Clean(strings.TrimSpace(requested))
	if path.IsAbs(file_path) || file_path == ".." || strings.HasPrefix(file_path, "../") {
		return requested, "is outside the repository"
	}
	if s.read_set[file_path] != nil {
		return file_path, "was already read, its contents are above"
	}
	if _, ok := s.repo.Dirs[file_path]; ok {
		return file_path, "is a directory, read the files in it instead"
	}
	file, ok := s.repo.Files[file_path]
	if !ok {
		return file_path, "doesn't exist, only paths from the file list can be read"
	}
	switch file.Skipped {
	case "", analysis.SkipLockfile:
		return file_path, ""
	case analysis.SkipEmpty:
		return file_path, "is empty"
	default:
		return file_path, fmt.Sprintf("can't be read (%s)", file.Skipped)
	}
}

// Number a file's lines for citing, cutting it off at a line break when it
// is longer than maxReadFileBytes
func newReadFile(file_path string, content []byte) *readFile {
	truncated := len(content) > maxReadFileBytes
	if truncated {
		content = content[:maxReadFileBytes]
		if end := bytes.LastIndexByte(content, '\n'); end >= 0 {
			content = content[:end+1]
		}
	}

	text := strings.TrimSuffix(string(bytes.ToValidUTF8(content, []byte("�"))), "\n")
	lines := strings.Split(text, "\n")
	var numbered strings.Builder
	for i, line := range lines {
		fmt.Fprintf(&numbered, "%d| %s\n", i+1, strings.TrimSuffix(line, "\r"))
	}
	return &readFile{
		path:      file_path,
		numbered:  numbered.String(),
		lines:     len(lines),
		truncated: truncated,
		bytes:     len(content),
	}
}

// Answer from the files read, streaming the answer as it is written
func answerNode(client llm.Client, options Options) *actorflow.Node[qaStore, string, string] {
	return &actorflow.Node[qaStore, string, string]{
		Prep: func(ctx context.Context, s *qaStore) (string, error) {
			return answerPrompt(s), nil
		},
		Exec: func(ctx context.Context, prompt string) (string, error) {
			options.emit(Event{Kind: EventAnswerStart})
			var answer strings.Builder
			for chunk, err := range client.Stream(ctx, llm.Request{
				System:         llm.ANSWER_PROMPT,
				Prompt:         prompt,
				ThinkingBudget: options.ThinkingBudget,
			}) {
				if err != nil {
					return "", err
				}
				answer.WriteString(chunk)
				options.emit(Event{Kind: EventAnswerChunk, Text: chunk})
			}
			return answer.String(), nil
		},
		Post: func(ctx context.Context, s *qaStore, prompt string, answer string) (actorflow.Action, error) {
			s.answer = answer
			return actorflow.Default, nil
		},
		Retry: answerRetry,
	}
}

type verifyJob struct {
	question string
	answer   string
	// lines of each file that were read
	read_lines map[string]int
	repo_files map[string]*analysis.FileAnalysis
	// the files read, formatted for the model
	files string
}

type verifyResult struct {
	citations []Citation
	issues    []string
	// why the model couldn't check the answer, empty if it did or wasn't asked to
	unchecked string
}

// Check the answer's citations, then have the model check its claims
func verifyNode(client llm.Client, options Options) *actorflow.Node[qaStore, verifyJob, verifyResult] {
	return &actorflow.Node[qaStore, verifyJob, verifyResult]{
		Prep: func(ctx context.Context, s *qaStore) (verifyJob, error) {
			job := verifyJob{
				question:   s.question,
				answer:     s.answer,
				read_lines: map[string]int{},
				repo_files: s.repo.Files,
				files:      formatFiles(s.read),
			}
			for _, file := range s.read {
				job.read_lines[file.path] = file.lines
			}
			return job, nil
		},
		Exec: func(ctx context.Context, job verifyJob) (verifyResult, error) {
			options.emit(Event{Kind: EventChecking})
			citations, issues := checkCitations(job.answer, job.read_lines, job.repo_files)
			// a call to check the claims isn't worth it when the citations are already wrong
			if len(issues) > 0 || !options.VerifyWithModel {
				return verifyResult{citations: citations, issues: issues}, nil
			}

			var result verdict
			err := client.GenerateJSON(ctx, llm.Request{
				System:         llm.VERIFY_PROMPT,
				Prompt:         verifyPrompt(job.question, job.answer, job.files),
				ThinkingBudget: options.ThinkingBudget,
			}, &result)
			if err != nil {
				if ctx.Err() != nil {
					return verifyResult{}, context.Cause(ctx)
				}
				// the answer is still worth giving, just not as verified
				return verifyResult{citations: citations, unchecked: err.Error()}, nil
			}
			if !result.Supported {
				issues = result.Issues
				if len(issues) == 0 {
					issues = []string{"Some claims aren't backed by the code they cite."}
				}
			}
			return verifyResult{citations: citations, issues: issues}, nil
		},
		Post: func(ctx context.Context, s *qaStore, job verifyJob, result verifyResult) (actorflow.Action, error) {
			s.citations = result.citations
			if len(result.issues) > 0 && s.retries < options.MaxRetries {
				s.retries++
				s.feedback = result.issues
				// another round of reading, in case what's missing is in a file not read yet
				s.max_rounds++
				s.notes = append(s.notes, "An earlier answer was rejected: "+strings.Join(result.issues, " "))
				options.emit(Event{Kind: EventRetry, Issues: result.issues})
				return actionRetry, nil
			}

			s.issues = result.issues
			if result.unchecked != "" {
				s.issues = append(s.issues, "The answer's claims couldn't be checked: "+result.unchecked)
			}
			s.verified = len(s.issues) == 0
			options.emit(Event{Kind: EventVerified, Issues: s.issues})
			return actionDone, nil
		},
	}
}
