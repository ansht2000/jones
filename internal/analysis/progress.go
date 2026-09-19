package analysis

import "sync"

// Reports the progress of one stage, shared by the items in it. Reports are
// made one at a time, so they arrive in order and OnProgress doesn't need
// to be safe for concurrent use.
type tracker struct {
	stage       Stage
	total       int
	on_progress func(Progress)

	mu      sync.Mutex
	done    int
	started bool
}

func newTracker(stage Stage, total int, on_progress func(Progress)) *tracker {
	return &tracker{stage: stage, total: total, on_progress: on_progress}
}

// Report the stage starting, only the first call reports anything
func (t *tracker) start() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.started {
		return
	}
	t.started = true
	t.report(Progress{Stage: t.stage, Total: t.total})
}

// Report an item finishing
func (t *tracker) finish(rel_path string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.done++
	t.report(Progress{Stage: t.stage, Done: t.done, Total: t.total, Path: rel_path})
}

// t.mu must be held
func (t *tracker) report(progress Progress) {
	if t.on_progress != nil {
		t.on_progress(progress)
	}
}
