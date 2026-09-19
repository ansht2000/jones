package actorflow

import (
	"context"

	"github.com/ansht2000/jones/internal/retry"
	"golang.org/x/sync/errgroup"
)

// A Node runs Prep, then Exec, then Post, any of which can be nil.
// A nil Post returns Default.
//
// S is the shared store, P is what Prep passes to Exec, and E is what Exec returns.
type Node[S, P, E any] struct {
	transitions[S]
	Prep func(ctx context.Context, shared *S) (P, error)
	// Retried according to Retry, should not touch the shared store
	Exec func(ctx context.Context, prep_res P) (E, error)
	// Called with the last error if Exec still fails after retrying,
	// its result is used in place of Exec's
	Fallback func(ctx context.Context, prep_res P, err error) (E, error)
	Post     func(ctx context.Context, shared *S, prep_res P, exec_res E) (Action, error)
	// Retry settings for Exec, the zero value calls Exec once
	Retry retry.RetryConfig
}

func (n *Node[S, P, E]) Run(ctx context.Context, shared *S) (Action, error) {
	var prep_res P
	if n.Prep != nil {
		var err error
		if prep_res, err = n.Prep(ctx, shared); err != nil {
			return "", err
		}
	}

	exec_res, err := runExec(ctx, n.Exec, n.Fallback, prep_res, n.Retry)
	if err != nil {
		return "", err
	}

	return runPost(ctx, n.Post, shared, prep_res, exec_res)
}

// A BatchNode runs Exec once per item returned by Prep, and Post gets the
// results in the same order as the items. If any item fails, the batch stops
// and Post is not called. Items run one at a time unless Concurrency is set.
//
// S is the shared store, P is the type of each item, and E is the result for each item.
type BatchNode[S, P, E any] struct {
	transitions[S]
	Prep func(ctx context.Context, shared *S) ([]P, error)
	// Called once per item and retried according to Retry, should not touch
	// the shared store since items can run concurrently
	Exec func(ctx context.Context, item P) (E, error)
	// Called with an item's last error if Exec still fails after retrying,
	// its result is used in place of Exec's
	Fallback func(ctx context.Context, item P, err error) (E, error)
	Post     func(ctx context.Context, shared *S, items []P, results []E) (Action, error)
	// Retry settings for each item's Exec, the zero value calls Exec once
	Retry retry.RetryConfig
	// Maximum items to run at once, 0 or 1 runs them one at a time,
	// a negative value runs every item at once
	Concurrency int
}

func (n *BatchNode[S, P, E]) Run(ctx context.Context, shared *S) (Action, error) {
	var items []P
	if n.Prep != nil {
		var err error
		if items, err = n.Prep(ctx, shared); err != nil {
			return "", err
		}
	}

	results := make([]E, len(items))
	if n.Concurrency == 0 || n.Concurrency == 1 {
		for i, item := range items {
			result, err := runExec(ctx, n.Exec, n.Fallback, item, n.Retry)
			if err != nil {
				return "", err
			}
			results[i] = result
		}
	} else {
		// the group's context is cancelled when any item fails,
		// stopping the rest of the batch
		group, group_ctx := errgroup.WithContext(ctx)
		group.SetLimit(n.Concurrency)
		for i, item := range items {
			if group_ctx.Err() != nil {
				break
			}
			group.Go(func() error {
				result, err := runExec(group_ctx, n.Exec, n.Fallback, item, n.Retry)
				if err != nil {
					return err
				}
				// each item writes to its own index, so no lock is needed
				results[i] = result
				return nil
			})
		}
		if err := group.Wait(); err != nil {
			return "", err
		}
	}

	return runPost(ctx, n.Post, shared, items, results)
}
