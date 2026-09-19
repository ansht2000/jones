// Package actorflow is a Go port of PocketFlow (https://github.com/The-Pocket/PocketFlow).
//
// Work is split into nodes that each run in three steps:
//
//   - Prep reads what the node needs from the shared store
//   - Exec does the work (API calls, file reads) without touching the store,
//     and is retried on failure
//   - Post writes the results back to the store and returns an Action
//
// A Flow links nodes into a graph, and the Action returned by each node picks
// the one that runs next, which allows branching and loops (agents).
// A BatchNode runs Exec once per item, optionally in parallel. Since Exec never
// sees the shared store, running items in parallel needs no locking.
//
// Nodes keep no state between runs, so a flow can be run many times, even
// concurrently, as long as each run gets its own shared store.
//
//	decide.On("search", search).Then(decide)
//	decide.On("answer", answer)
//	flow := &Flow[Store]{Start: decide, MaxSteps: 20}
//	action, err := flow.Run(ctx, &store)
package actorflow

import (
	"context"

	"github.com/ansht2000/jones/internal/retry"
)

// An Action is returned by a node to pick which node runs next
type Action string

// The action used by Then, and by nodes that return an empty action
const Default Action = "default"

// A Runnable is a node or flow that can be linked into a flow
type Runnable[S any] interface {
	// Run against the shared store, returning the action that picks the next node
	Run(ctx context.Context, shared *S) (Action, error)
	// Node to run after this one returns action, nil ends the flow
	Successor(action Action) Runnable[S]
	// Run next_node after this one returns action,
	// returns next_node so calls can be chained
	On(action Action, next_node Runnable[S]) Runnable[S]
	// Shorthand for On(Default, next_node)
	Then(next_node Runnable[S]) Runnable[S]
}

var (
	_ Runnable[struct{}] = (*Node[struct{}, int, int])(nil)
	_ Runnable[struct{}] = (*BatchNode[struct{}, int, int])(nil)
	_ Runnable[struct{}] = (*Flow[struct{}])(nil)
)

// holds a node's successors, embedded in every node type
type transitions[S any] struct {
	successors map[Action]Runnable[S]
}

func (t *transitions[S]) On(action Action, next_node Runnable[S]) Runnable[S] {
	if t.successors == nil {
		t.successors = make(map[Action]Runnable[S])
	}
	t.successors[normalizeAction(action)] = next_node
	return next_node
}

func (t *transitions[S]) Then(next_node Runnable[S]) Runnable[S] {
	return t.On(Default, next_node)
}

func (t *transitions[S]) Successor(action Action) Runnable[S] {
	return t.successors[normalizeAction(action)]
}

func normalizeAction(action Action) Action {
	if action == "" {
		return Default
	}
	return action
}

type execFunc[P, E any] func(ctx context.Context, prep_res P) (E, error)

type fallbackFunc[P, E any] func(ctx context.Context, prep_res P, err error) (E, error)

type postFunc[S, P, E any] func(ctx context.Context, shared *S, prep_res P, exec_res E) (Action, error)

// run exec with retries, using the fallback if it still fails
func runExec[P, E any](ctx context.Context, exec execFunc[P, E], fallback fallbackFunc[P, E], prep_res P, retry_config retry.RetryConfig) (E, error) {
	var exec_res E
	if ctx.Err() != nil {
		return exec_res, context.Cause(ctx)
	}
	if exec == nil {
		return exec_res, nil
	}

	var err error
	// the zero config means no retries, calling exec directly
	// returns its errors without the retry library wrapping them
	if retry_config.MaxRetries == 0 && retry_config.MaxDuration == 0 {
		exec_res, err = exec(ctx, prep_res)
	} else {
		exec_res, err = retry.RetryWithValue(ctx, func(ctx context.Context) (E, error) {
			return exec(ctx, prep_res)
		}, retry_config)
	}

	// cancellation should stop the flow instead of falling back
	if err != nil && fallback != nil && ctx.Err() == nil {
		return fallback(ctx, prep_res, err)
	}
	return exec_res, err
}

func runPost[S, P, E any](ctx context.Context, post postFunc[S, P, E], shared *S, prep_res P, exec_res E) (Action, error) {
	if post == nil {
		return Default, nil
	}

	action, err := post(ctx, shared, prep_res, exec_res)
	if err != nil {
		return "", err
	}
	return normalizeAction(action), nil
}
