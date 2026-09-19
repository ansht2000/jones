// Package analysis summarizes a repository with a language model: every
// file, then every directory from the deepest up, then the whole repo, with
// the calls in each step running in parallel. Analyses are saved, and later
// runs only redo the files and directories that changed.
package analysis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ansht2000/jones/internal/llm"
	"github.com/ansht2000/jones/internal/repo"
)

// Bump when the saved format or the prompts change, so older analyses are redone
const Version = 1

var (
	ErrTooManyFiles    = errors.New("too many files to summarize")
	ErrInvalidRepoName = errors.New("invalid repo name")
	ErrOutdated        = errors.New("saved analysis is from an older version of jones")
)

// What the model writes about each file
type FileSummary struct {
	Purpose      string   `json:"purpose" description:"what the file does and why it exists, in one or two sentences"`
	KeySymbols   []string `json:"key_symbols" description:"the most important functions, types, and constants defined in the file, exactly as written, at most 10"`
	Dependencies []string `json:"dependencies" description:"packages, modules, and files in this repository that the file imports or relies on"`
}

type FileAnalysis struct {
	// SHA-256 of the file's contents
	Hash    string      `json:"hash"`
	Size    int64       `json:"size"`
	Summary FileSummary `json:"summary"`
	// Why the file wasn't sent to the model, like being binary or a lockfile
	Skipped string `json:"skipped,omitempty"`
	// Why the model couldn't summarize the file, it is tried again on the next run
	Error string `json:"error,omitempty"`
}

type DirAnalysis struct {
	// Hash of everything in the directory, changes when any file under it does
	Hash    string `json:"hash"`
	Summary string `json:"summary"`
	// Set when something in the directory couldn't be summarized,
	// so the summary is redone on the next run
	Stale bool `json:"stale,omitempty"`
}

// Everything learned about a repository
type Analysis struct {
	Version int    `json:"version"`
	Name    string `json:"name"`
	// Hash of the whole repo, changes when any file does
	Hash     string `json:"hash"`
	Overview string `json:"overview"`
	// Set when something in the repo couldn't be summarized
	Stale bool `json:"stale,omitempty"`
	// Keyed by path relative to the repo root, with forward slashes
	Files map[string]*FileAnalysis `json:"files"`
	// Keyed like Files, without the root directory, which Overview covers
	Dirs map[string]*DirAnalysis `json:"dirs"`
}

type Stage string

const (
	StageScan     Stage = "scanning files"
	StageFiles    Stage = "summarizing files"
	StageDirs     Stage = "summarizing directories"
	StageOverview Stage = "writing overview"
)

// Reported when a stage starts and each time an item in it finishes
type Progress struct {
	Stage Stage
	// Items finished and the total, leaving out items reused from an earlier run
	Done, Total int
	// The item that just finished, empty when the stage starts
	Path string
}

type Options struct {
	// Directory analyses are saved to and loaded from, empty turns saving off
	SaveDir string
	// Maximum model calls in flight at once, 0 or 1 makes them one at a time
	Concurrency int
	// Maximum files to send to the model in one run, 0 means no limit. When
	// more files than this need summaries, Analyze fails with
	// ErrTooManyFiles before calling the model.
	MaxFiles int
	// Tokens the model can spend thinking per call, see llm.Request
	ThinkingBudget *int32
	// Called as work finishes, from several goroutines but never at the same time
	OnProgress func(Progress)
}

func DefaultOptions() Options {
	return Options{
		SaveDir:     repo.DefaultAnalysisHome(),
		Concurrency: 16,
		MaxFiles:    2000,
		// summaries don't need thinking and it makes every call slower,
		// set to nil for models that can't turn thinking off
		ThinkingBudget: llm.Ptr[int32](0),
	}
}

// Summarize the repo at repo_path. Summaries saved by an earlier run are
// reused for files and directories that haven't changed.
func Analyze(ctx context.Context, client llm.Client, repo_name, repo_path string, options Options) (*Analysis, error) {
	if err := checkRepoName(repo_name); err != nil {
		return nil, err
	}

	store := &repoStore{name: repo_name, path: repo_path}
	if _, err := newAnalysisFlow(client, options).Run(ctx, store); err != nil {
		return nil, err
	}
	return store.result, nil
}

// Load the analysis saved for a repo
func LoadAnalysis(save_dir, repo_name string) (*Analysis, error) {
	if err := checkRepoName(repo_name); err != nil {
		return nil, err
	}

	data, err := os.ReadFile(SavePath(save_dir, repo_name))
	if err != nil {
		return nil, err
	}
	var analysis Analysis
	if err := json.Unmarshal(data, &analysis); err != nil {
		return nil, fmt.Errorf("reading saved analysis of %s: %w", repo_name, err)
	}
	if analysis.Version != Version {
		return nil, ErrOutdated
	}
	return &analysis, nil
}

func saveAnalysis(save_dir string, analysis *Analysis) error {
	if err := os.MkdirAll(save_dir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(analysis, "", "\t")
	if err != nil {
		return err
	}

	// written to a temporary file and renamed into place,
	// so a crash can't leave a half written analysis behind
	tmp, err := os.CreateTemp(save_dir, analysis.Name+".*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), SavePath(save_dir, analysis.Name))
}

// Where the analysis of a repo is saved
func SavePath(save_dir, repo_name string) string {
	return filepath.Join(save_dir, repo_name+".json")
}

// repo names become file names, so they can't contain paths
func checkRepoName(repo_name string) error {
	if repo_name == "" || repo_name == "." || repo_name == ".." || strings.ContainsAny(repo_name, `/\`) {
		return fmt.Errorf("%w: %q", ErrInvalidRepoName, repo_name)
	}
	return nil
}
