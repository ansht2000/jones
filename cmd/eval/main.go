// Command eval measures how well jones answers questions about pinned
// versions of real repos, using the Gemini API with the key from the
// environment or .env. It prints a Markdown report and saves it, with the raw
// results, to -out.
//
//	go run ./cmd/eval            run the whole evaluation
//	go run ./cmd/eval -dry-run   check the setup and estimate the API calls, without making any
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/ansht2000/jones/internal/eval"
	"github.com/ansht2000/jones/internal/llm"
	"github.com/ansht2000/jones/internal/repo"
	"github.com/joho/godotenv"
)

func main() {
	questions_path := flag.String("questions", "eval/questions.json", "the questions to ask, see internal/eval for the format")
	out := flag.String("out", "eval/results", "where to save the report and the raw results, as .md and .json")
	only := flag.String("repos", "", "comma separated names of the repos to evaluate, all of them by default")
	sequential := flag.Bool("sequential", false, "also time analyzing each repo one call at a time, which doubles the cost of analyzing")
	judge := flag.Bool("judge", true, "have the model judge each answer against the reference answer")
	dry_run := flag.Bool("dry-run", false, "find the repos and estimate the number of API calls, without making any")
	flag.Parse()
	godotenv.Load(".env")

	dataset, err := eval.LoadDataset(*questions_path)
	if err == nil && *only != "" {
		dataset, err = onlyRepos(dataset, strings.Split(*only, ","))
	}
	if err == nil {
		err = run(dataset, *out, *sequential, *judge, *dry_run)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "eval: %v\n", err)
		os.Exit(1)
	}
}

func run(dataset *eval.Dataset, out string, sequential, judge, dry_run bool) error {
	repo_paths := map[string]string{}
	for _, eval_repo := range dataset.Repos {
		dir, err := moduleDir(eval_repo.Module, eval_repo.Version)
		if err != nil {
			return fmt.Errorf("downloading %s@%s: %w", eval_repo.Module, eval_repo.Version, err)
		}
		repo_paths[eval_repo.Name] = dir
	}
	if dry_run {
		printPlan(dataset, repo_paths, sequential, judge)
		return nil
	}

	config, err := llm.GeminiConfigFromEnv()
	if err != nil {
		return err
	}
	config.Retry.OnRetry = func(attempt int, err error, delay time.Duration) {
		fmt.Fprintf(os.Stderr, "  retrying in %s after: %v\n", delay.Round(time.Second), err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	client, err := llm.NewGeminiClient(ctx, config)
	if err != nil {
		return err
	}

	// analyses start from scratch, so the first run of each is timed fairly
	work_dir, err := os.MkdirTemp("", "jones-eval-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work_dir)

	start := time.Now()
	result, err := eval.Run(ctx, dataset, repo_paths, eval.Options{
		Client:     client,
		WorkDir:    work_dir,
		Systems:    eval.SYSTEMS,
		Judge:      judge,
		Sequential: sequential,
		Log: func(line string) {
			fmt.Fprintln(os.Stderr, line)
		},
	})
	if err != nil {
		return err
	}

	header := fmt.Sprintf("Model %s, %d questions about %d repos, run on %s in %s.",
		config.Model, len(dataset.Questions), len(dataset.Repos), time.Now().Format("2006-01-02"), time.Since(start).Round(time.Second))
	report := result.Markdown(eval.SYSTEMS, header)
	raw, err := json.MarshalIndent(result, "", "\t")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(out), 0755); err != nil {
		return err
	}
	if err := os.WriteFile(out+".md", []byte(report), 0644); err != nil {
		return err
	}
	if err := os.WriteFile(out+".json", raw, 0644); err != nil {
		return err
	}
	fmt.Print(report)
	fmt.Fprintf(os.Stderr, "\nSaved the report to %s.md and the raw results to %s.json\n", out, out)
	return nil
}

// Download a module version into the module cache, returning its directory
func moduleDir(module, version string) (string, error) {
	cmd := exec.Command("go", "mod", "download", "-json", module+"@"+version)
	// run outside any module, so no go.mod or go.sum is changed
	cmd.Dir = os.TempDir()
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=")
	output, run_err := cmd.Output()

	var info struct {
		Dir   string
		Error string
	}
	if err := json.Unmarshal(output, &info); err != nil {
		return "", errors.Join(run_err, err)
	}
	if info.Error != "" {
		return "", errors.New(info.Error)
	}
	if run_err != nil {
		return "", run_err
	}
	return info.Dir, nil
}

// Keep only the named repos and the questions about them
func onlyRepos(dataset *eval.Dataset, names []string) (*eval.Dataset, error) {
	filtered := &eval.Dataset{}
	for _, eval_repo := range dataset.Repos {
		if slices.Contains(names, eval_repo.Name) {
			filtered.Repos = append(filtered.Repos, eval_repo)
		}
	}
	if len(filtered.Repos) != len(names) {
		return nil, fmt.Errorf("unknown repo in %v", names)
	}
	for _, question := range dataset.Questions {
		if slices.Contains(names, question.Repo) {
			filtered.Questions = append(filtered.Questions, question)
		}
	}
	return filtered, nil
}

// Show what would be evaluated and about how many API calls it takes
func printPlan(dataset *eval.Dataset, repo_paths map[string]string, sequential, judge bool) {
	analysis_calls := 0
	for _, eval_repo := range dataset.Repos {
		files, dirs := countTree(repo.BuildRepoTree(eval_repo.Name, repo_paths[eval_repo.Name]))
		questions := 0
		for _, question := range dataset.Questions {
			if question.Repo == eval_repo.Name {
				questions++
			}
		}
		fmt.Printf("%s: %s@%s in %s\n  files: %d, directories: %d, questions: %d\n",
			eval_repo.Name, eval_repo.Module, eval_repo.Version, repo_paths[eval_repo.Name], files, dirs, questions)

		// a call per file, directory, and the overview, fewer when files are skipped
		calls := files + dirs + 1
		if sequential {
			calls *= 2
		}
		analysis_calls += calls
	}

	// the baseline takes one call, the agent about three, and with verification about four
	per_question := 1 + 3 + 4
	if judge {
		per_question += 3
	}
	fmt.Printf("\nAt most %d API calls to analyze the repos, and about %d to answer %d questions.\n",
		analysis_calls, per_question*len(dataset.Questions), len(dataset.Questions))
}

// Files and directories in a tree, not counting its root
func countTree(item *repo.RepoItem) (files, dirs int) {
	for _, child := range item.Children {
		if child.IsDir {
			child_files, child_dirs := countTree(child)
			files += child_files
			dirs += child_dirs + 1
		} else {
			files++
		}
	}
	return files, dirs
}
