package actorflow

import (
	"context"
	"errors"
	"fmt"
)

var (
	ErrNoStartNode = errors.New("flow has no start node")
	ErrMaxSteps    = errors.New("flow ran more than its maximum number of steps")
)

// A Flow runs nodes starting from Start, following each node's action to the
// next node until an action has no successor. The flow returns the last
// action, so a flow can be used as a node in a bigger flow, where its last
// action picks what runs after it.
type Flow[S any] struct {
	transitions[S]
	Start Runnable[S]
	// Maximum number of nodes to run before failing with ErrMaxSteps,
	// guards against loops that never exit, 0 means no limit
	MaxSteps int
}

func (f *Flow[S]) Run(ctx context.Context, shared *S) (Action, error) {
	if f.Start == nil {
		return "", ErrNoStartNode
	}

	current := f.Start
	action := Default
	for steps := 0; current != nil; steps++ {
		if f.MaxSteps > 0 && steps >= f.MaxSteps {
			return "", fmt.Errorf("%w (%d)", ErrMaxSteps, f.MaxSteps)
		}
		if ctx.Err() != nil {
			return "", context.Cause(ctx)
		}

		var err error
		if action, err = current.Run(ctx, shared); err != nil {
			return "", err
		}
		current = current.Successor(action)
	}

	return action, nil
}
