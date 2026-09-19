package analysis

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strings"

	"github.com/ansht2000/jones/internal/actorflow"
	"github.com/ansht2000/jones/internal/llm"
	"github.com/ansht2000/jones/internal/repo"
)

// Files hashed at once, mostly waiting on the disk
const hashConcurrency = 32

// Returned by the directory node while there are levels left to summarize
const nextLevel actorflow.Action = "next_level"

// Used in place of a summary the model couldn't write
const failedSummary = "Could not be summarized."

// The shared store for an analysis
type repoStore struct {
	name string
	path string
	// the analysis saved by an earlier run, nil if there isn't a usable one
	previous *Analysis
	root     *dirEntry
	// every file, and every directory listed after its children
	files []*fileEntry
	dirs  []*dirEntry
	// directories below the root left to summarize, by depth, deepest first
	dir_levels  [][]*dirEntry
	dir_tracker *tracker
	result      *Analysis
}

type fileEntry struct {
	// relative to the repo root with forward slashes
	rel_path string
	abs_path string
	fileInfo
}

type dirEntry struct {
	// relative to the repo root with forward slashes, empty for the root
	rel_path string
	depth    int
	dirs     []*dirEntry
	files    []*fileEntry
	hash     string
	// whether the directory needs a new summary this run
	needs_summary bool
}

func (d *dirEntry) empty() bool {
	return len(d.dirs) == 0 && len(d.files) == 0
}

// the result of summarizing one item, err is set when the model couldn't
type summaryResult[T any] struct {
	summary T
	err     string
}

func newAnalysisFlow(client llm.Client, options Options) *actorflow.Flow[repoStore] {
	load := loadNode(options)
	scan := scanNode()
	hash := hashNode(options)
	files := summarizeFilesNode(client, options)
	dirs := summarizeDirsNode(client, options)
	overview := overviewNode(client, options)
	save := saveNode(options)

	load.Then(scan).Then(hash).Then(files).Then(dirs)
	// runs once per level of directories, deepest first,
	// since a directory's summary is built from its children's
	dirs.On(nextLevel, dirs)
	dirs.Then(overview).Then(save)
	return &actorflow.Flow[repoStore]{Start: load}
}

// Load the analysis saved by an earlier run. A missing, outdated, or
// unreadable one just means starting from scratch.
func loadNode(options Options) *actorflow.Node[repoStore, string, *Analysis] {
	return &actorflow.Node[repoStore, string, *Analysis]{
		Prep: func(ctx context.Context, s *repoStore) (string, error) {
			return s.name, nil
		},
		Exec: func(ctx context.Context, repo_name string) (*Analysis, error) {
			if options.SaveDir == "" {
				return nil, nil
			}
			previous, err := LoadAnalysis(options.SaveDir, repo_name)
			if err != nil {
				return nil, nil
			}
			return previous, nil
		},
		Post: func(ctx context.Context, s *repoStore, repo_name string, previous *Analysis) (actorflow.Action, error) {
			s.previous = previous
			return actorflow.Default, nil
		},
	}
}

type repoLocation struct {
	name string
	path string
}

type scanResult struct {
	root  *dirEntry
	files []*fileEntry
	dirs  []*dirEntry
}

// List the repo's files and directories
func scanNode() *actorflow.Node[repoStore, repoLocation, scanResult] {
	return &actorflow.Node[repoStore, repoLocation, scanResult]{
		Prep: func(ctx context.Context, s *repoStore) (repoLocation, error) {
			return repoLocation{name: s.name, path: s.path}, nil
		},
		Exec: func(ctx context.Context, location repoLocation) (scanResult, error) {
			tree := repo.BuildRepoTree(location.name, location.path)
			if tree.Err != "" {
				return scanResult{}, fmt.Errorf("scanning %s: %s", location.name, tree.Err)
			}
			var scan scanResult
			scan.root = flatten(tree, "", 0, &scan)
			return scan, nil
		},
		Post: func(ctx context.Context, s *repoStore, location repoLocation, scan scanResult) (actorflow.Action, error) {
			s.root, s.files, s.dirs = scan.root, scan.files, scan.dirs
			s.result = &Analysis{
				Version: Version,
				Name:    s.name,
				Files:   make(map[string]*FileAnalysis, len(scan.files)),
				Dirs:    make(map[string]*DirAnalysis, len(scan.dirs)),
			}
			return actorflow.Default, nil
		},
	}
}

// Add a tree's files and directories to scan, listing each directory after its children
func flatten(item *repo.RepoItem, rel_path string, depth int, scan *scanResult) *dirEntry {
	dir := &dirEntry{rel_path: rel_path, depth: depth}
	for _, child := range item.Children {
		child_rel_path := path.Join(rel_path, child.ItemName)
		if child.IsDir {
			dir.dirs = append(dir.dirs, flatten(child, child_rel_path, depth+1, scan))
		} else {
			file := &fileEntry{rel_path: child_rel_path, abs_path: child.ItemPath}
			dir.files = append(dir.files, file)
			scan.files = append(scan.files, file)
		}
	}
	scan.dirs = append(scan.dirs, dir)
	return dir
}

type hashJob struct {
	file    *fileEntry
	tracker *tracker
}

// Hash every file and decide which ones the model should see, then hash
// every directory and decide which need new summaries
func hashNode(options Options) *actorflow.BatchNode[repoStore, hashJob, fileInfo] {
	return &actorflow.BatchNode[repoStore, hashJob, fileInfo]{
		Prep: func(ctx context.Context, s *repoStore) ([]hashJob, error) {
			progress := newTracker(StageScan, len(s.files), options.OnProgress)
			progress.start()
			jobs := make([]hashJob, len(s.files))
			for i, file := range s.files {
				jobs[i] = hashJob{file: file, tracker: progress}
			}
			return jobs, nil
		},
		Exec: func(ctx context.Context, job hashJob) (fileInfo, error) {
			info := inspectFile(job.file.abs_path)
			job.tracker.finish(job.file.rel_path)
			return info, nil
		},
		Post: func(ctx context.Context, s *repoStore, jobs []hashJob, infos []fileInfo) (actorflow.Action, error) {
			for i, job := range jobs {
				job.file.fileInfo = infos[i]
			}
			s.planDirs(options)
			return actorflow.Default, nil
		},
		Concurrency: hashConcurrency,
	}
}

// Hash every directory, and group the ones below the root by depth,
// deepest first, marking which need new summaries
func (s *repoStore) planDirs(options Options) {
	levels := map[int][]*dirEntry{}
	max_depth, needed := 0, 0
	// children come before their parents, so they are hashed first
	for _, dir := range s.dirs {
		dir.hash = hashDir(dir)
		if dir.depth == 0 {
			continue
		}
		dir.needs_summary = !dir.empty() && s.previous.reusableDir(dir) == nil
		if dir.needs_summary {
			needed++
		}
		levels[dir.depth] = append(levels[dir.depth], dir)
		max_depth = max(max_depth, dir.depth)
	}

	for depth := max_depth; depth >= 1; depth-- {
		s.dir_levels = append(s.dir_levels, levels[depth])
	}
	s.dir_tracker = newTracker(StageDirs, needed, options.OnProgress)
}

type fileJob struct {
	rel_path string
	abs_path string
	size     int64
	tracker  *tracker
}

// Summarize every file that changed since the last run
func summarizeFilesNode(client llm.Client, options Options) *actorflow.BatchNode[repoStore, fileJob, summaryResult[FileSummary]] {
	return &actorflow.BatchNode[repoStore, fileJob, summaryResult[FileSummary]]{
		Prep: func(ctx context.Context, s *repoStore) ([]fileJob, error) {
			jobs := []fileJob{}
			for _, file := range s.files {
				if file.skipped == "" && s.previous.reusableFile(file) == nil {
					jobs = append(jobs, fileJob{rel_path: file.rel_path, abs_path: file.abs_path, size: file.size})
				}
			}
			if options.MaxFiles > 0 && len(jobs) > options.MaxFiles {
				return nil, fmt.Errorf("%w: %d files need summaries and the limit is %d", ErrTooManyFiles, len(jobs), options.MaxFiles)
			}

			progress := newTracker(StageFiles, len(jobs), options.OnProgress)
			progress.start()
			for i := range jobs {
				jobs[i].tracker = progress
			}
			return jobs, nil
		},
		Exec: func(ctx context.Context, job fileJob) (summaryResult[FileSummary], error) {
			content, err := readHead(job.abs_path, maxFileBytes)
			if err != nil {
				return summaryResult[FileSummary]{}, err
			}
			var summary FileSummary
			err = client.GenerateJSON(ctx, llm.Request{
				System:         llm.FILE_SUMMARY_PROMPT,
				Prompt:         filePrompt(job.rel_path, content, job.size),
				ThinkingBudget: options.ThinkingBudget,
			}, &summary)
			if err != nil {
				return summaryResult[FileSummary]{}, err
			}
			job.tracker.finish(job.rel_path)
			return summaryResult[FileSummary]{summary: summary}, nil
		},
		Fallback: func(ctx context.Context, job fileJob, err error) (summaryResult[FileSummary], error) {
			return fallback[FileSummary](err, job.tracker, job.rel_path)
		},
		Post: func(ctx context.Context, s *repoStore, jobs []fileJob, results []summaryResult[FileSummary]) (actorflow.Action, error) {
			summarized := make(map[string]summaryResult[FileSummary], len(jobs))
			for i, job := range jobs {
				summarized[job.rel_path] = results[i]
			}

			for _, file := range s.files {
				record := &FileAnalysis{Hash: file.hash, Size: file.size, Skipped: file.skipped}
				if file.skipped != "" {
					record.Summary.Purpose = SKIPPED_PURPOSES[file.skipped]
				} else if result, ok := summarized[file.rel_path]; ok {
					record.Summary = result.summary
					if result.err != "" {
						record.Summary.Purpose = failedSummary
						record.Error = result.err
					}
				} else {
					record.Summary = s.previous.Files[file.rel_path].Summary
				}
				s.result.Files[file.rel_path] = record
			}
			return actorflow.Default, nil
		},
		Concurrency: options.Concurrency,
	}
}

type dirJob struct {
	rel_path string
	prompt   string
	tracker  *tracker
}

// Summarize one level of directories from their children's summaries
func summarizeDirsNode(client llm.Client, options Options) *actorflow.BatchNode[repoStore, dirJob, summaryResult[string]] {
	return &actorflow.BatchNode[repoStore, dirJob, summaryResult[string]]{
		Prep: func(ctx context.Context, s *repoStore) ([]dirJob, error) {
			s.dir_tracker.start()
			if len(s.dir_levels) == 0 {
				return nil, nil
			}

			jobs := []dirJob{}
			for _, dir := range s.dir_levels[0] {
				if dir.needs_summary {
					jobs = append(jobs, dirJob{rel_path: dir.rel_path, prompt: dirPrompt(dir, s.result), tracker: s.dir_tracker})
				}
			}
			return jobs, nil
		},
		Exec: func(ctx context.Context, job dirJob) (summaryResult[string], error) {
			summary, err := client.Generate(ctx, llm.Request{
				System:         llm.DIR_SUMMARY_PROMPT,
				Prompt:         job.prompt,
				ThinkingBudget: options.ThinkingBudget,
			})
			if err != nil {
				return summaryResult[string]{}, err
			}
			job.tracker.finish(job.rel_path)
			return summaryResult[string]{summary: summary}, nil
		},
		Fallback: func(ctx context.Context, job dirJob, err error) (summaryResult[string], error) {
			return fallback[string](err, job.tracker, job.rel_path)
		},
		Post: func(ctx context.Context, s *repoStore, jobs []dirJob, results []summaryResult[string]) (actorflow.Action, error) {
			if len(s.dir_levels) == 0 {
				return actorflow.Default, nil
			}
			summarized := make(map[string]summaryResult[string], len(jobs))
			for i, job := range jobs {
				summarized[job.rel_path] = results[i]
			}

			for _, dir := range s.dir_levels[0] {
				record := &DirAnalysis{Hash: dir.hash}
				if result, ok := summarized[dir.rel_path]; ok {
					record.Summary = result.summary
					record.Stale = result.err != "" || s.result.hasProblems(dir)
					if result.err != "" {
						record.Summary = failedSummary
					}
				} else if dir.empty() {
					record.Summary = "Empty directory."
				} else {
					record = s.previous.Dirs[dir.rel_path]
				}
				s.result.Dirs[dir.rel_path] = record
			}

			s.dir_levels = s.dir_levels[1:]
			if len(s.dir_levels) > 0 {
				return nextLevel, nil
			}
			return actorflow.Default, nil
		},
		Concurrency: options.Concurrency,
	}
}

type overviewJob struct {
	repo_name string
	contents  string
	readme    *fileEntry
	// the saved overview, when nothing in the repo has changed
	saved   string
	tracker *tracker
}

// Write an overview of the whole repo from its top level summaries and README
func overviewNode(client llm.Client, options Options) *actorflow.Node[repoStore, overviewJob, summaryResult[string]] {
	return &actorflow.Node[repoStore, overviewJob, summaryResult[string]]{
		Prep: func(ctx context.Context, s *repoStore) (overviewJob, error) {
			job := overviewJob{repo_name: s.name, contents: contentsList(s.root, s.result), readme: findReadme(s.root)}
			if previous := s.previous; previous != nil && previous.Hash == s.root.hash && !previous.Stale && previous.Overview != "" {
				job.saved = previous.Overview
			}

			total := 1
			if job.saved != "" {
				total = 0
			}
			job.tracker = newTracker(StageOverview, total, options.OnProgress)
			job.tracker.start()
			return job, nil
		},
		Exec: func(ctx context.Context, job overviewJob) (summaryResult[string], error) {
			if job.saved != "" {
				return summaryResult[string]{summary: job.saved}, nil
			}

			var readme []byte
			if job.readme != nil {
				// the overview is still worth writing without the README
				readme, _ = readHead(job.readme.abs_path, maxReadmeBytes)
			}
			overview, err := client.Generate(ctx, llm.Request{
				System:         llm.REPO_OVERVIEW_PROMPT,
				Prompt:         overviewPrompt(job.repo_name, job.contents, job.readme, readme),
				ThinkingBudget: options.ThinkingBudget,
			})
			if err != nil {
				return summaryResult[string]{}, err
			}
			job.tracker.finish("")
			return summaryResult[string]{summary: overview}, nil
		},
		Fallback: func(ctx context.Context, job overviewJob, err error) (summaryResult[string], error) {
			return fallback[string](err, job.tracker, "")
		},
		Post: func(ctx context.Context, s *repoStore, job overviewJob, result summaryResult[string]) (actorflow.Action, error) {
			s.result.Hash = s.root.hash
			s.result.Overview = result.summary
			s.result.Stale = result.err != "" || s.result.hasProblems(s.root)
			if result.err != "" {
				s.result.Overview = failedSummary
			}
			return actorflow.Default, nil
		},
	}
}

// Save the analysis so the next run can reuse it
func saveNode(options Options) *actorflow.Node[repoStore, *Analysis, struct{}] {
	return &actorflow.Node[repoStore, *Analysis, struct{}]{
		Prep: func(ctx context.Context, s *repoStore) (*Analysis, error) {
			return s.result, nil
		},
		Exec: func(ctx context.Context, analysis *Analysis) (struct{}, error) {
			if options.SaveDir == "" {
				return struct{}{}, nil
			}
			return struct{}{}, saveAnalysis(options.SaveDir, analysis)
		},
	}
}

// Use a placeholder when an item's contents are the problem, and fail the
// whole analysis for anything else, like a bad API key or the network being down
func fallback[T any](err error, progress *tracker, rel_path string) (summaryResult[T], error) {
	if !isItemProblem(err) {
		return summaryResult[T]{}, err
	}
	progress.finish(rel_path)
	return summaryResult[T]{err: err.Error()}, nil
}

func isItemProblem(err error) bool {
	var path_err *fs.PathError
	return errors.Is(err, llm.ErrBlocked) ||
		errors.Is(err, llm.ErrTruncated) ||
		errors.Is(err, llm.ErrInvalidJSON) ||
		errors.Is(err, llm.ErrEmptyResponse) ||
		errors.As(err, &path_err)
}

// the README at the top of the repo, if there is one
func findReadme(root *dirEntry) *fileEntry {
	for _, file := range root.files {
		name := strings.ToLower(path.Base(file.rel_path))
		if file.skipped == "" && (name == "readme" || strings.HasPrefix(name, "readme.")) {
			return file
		}
	}
	return nil
}

// the saved analysis of a file, if it is still up to date
func (a *Analysis) reusableFile(file *fileEntry) *FileAnalysis {
	if a == nil {
		return nil
	}
	saved := a.Files[file.rel_path]
	if saved == nil || saved.Hash != file.hash || saved.Error != "" || saved.Skipped != "" {
		return nil
	}
	return saved
}

// the saved analysis of a directory, if it is still up to date
func (a *Analysis) reusableDir(dir *dirEntry) *DirAnalysis {
	if a == nil {
		return nil
	}
	saved := a.Dirs[dir.rel_path]
	if saved == nil || saved.Hash != dir.hash || saved.Stale {
		return nil
	}
	return saved
}

// whether anything directly in a directory couldn't be summarized
func (a *Analysis) hasProblems(dir *dirEntry) bool {
	for _, child := range dir.dirs {
		if a.Dirs[child.rel_path].Stale {
			return true
		}
	}
	for _, file := range dir.files {
		if a.Files[file.rel_path].Error != "" {
			return true
		}
	}
	return false
}
