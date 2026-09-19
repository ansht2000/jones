package actorflow

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ansht2000/jones/internal/retry"
)

var errTest = errors.New("test error")

type testStore struct {
	log     []string
	count   int
	input   int
	output  int
	results []int
}

type none = struct{}

// a node that records its name in the store's log and returns action
func logNode(name string, action Action) *Node[testStore, none, none] {
	return &Node[testStore, none, none]{
		Post: func(ctx context.Context, s *testStore, _ none, _ none) (Action, error) {
			s.log = append(s.log, name)
			return action, nil
		},
	}
}

func runFlow(t *testing.T, start Runnable[testStore], expected_log []string) Action {
	t.Helper()
	s := testStore{}
	action, err := (&Flow[testStore]{Start: start}).Run(context.Background(), &s)
	if err != nil {
		t.Fatalf("Unexpected error %v\n", err)
	}
	if !slices.Equal(s.log, expected_log) {
		t.Errorf("Expected nodes %v to run, got %v\n", expected_log, s.log)
	}
	return action
}

func TestNodeLifecycle(t *testing.T) {
	node := &Node[testStore, int, string]{
		Prep: func(ctx context.Context, s *testStore) (int, error) {
			return s.input, nil
		},
		Exec: func(ctx context.Context, input int) (string, error) {
			return fmt.Sprint(input * 2), nil
		},
		Post: func(ctx context.Context, s *testStore, input int, doubled string) (Action, error) {
			s.log = append(s.log, fmt.Sprintf("%d->%s", input, doubled))
			return "next", nil
		},
	}

	s := testStore{input: 21}
	action, err := node.Run(context.Background(), &s)
	if err != nil {
		t.Fatalf("Unexpected error %v\n", err)
	}
	if action != "next" {
		t.Errorf("Expected action next, got %s\n", action)
	}
	if !slices.Equal(s.log, []string{"21->42"}) {
		t.Errorf("Expected log [21->42], got %v\n", s.log)
	}
}

func TestNodeNilSteps(t *testing.T) {
	action, err := (&Node[testStore, int, int]{}).Run(context.Background(), &testStore{})
	if err != nil || action != Default {
		t.Errorf("Expected default action and no error, got %s, %v\n", action, err)
	}
}

func TestNodePrepError(t *testing.T) {
	exec_called := false
	node := &Node[testStore, none, none]{
		Prep: func(ctx context.Context, s *testStore) (none, error) {
			return none{}, errTest
		},
		Exec: func(ctx context.Context, _ none) (none, error) {
			exec_called = true
			return none{}, nil
		},
	}
	if _, err := node.Run(context.Background(), &testStore{}); err != errTest {
		t.Errorf("Expected error %v, got %v\n", errTest, err)
	}
	if exec_called {
		t.Error("Exec should not run when Prep fails")
	}
}

func TestLinearFlow(t *testing.T) {
	a, b, c := logNode("a", Default), logNode("b", Default), logNode("c", "end")
	a.Then(b).Then(c)

	if action := runFlow(t, a, []string{"a", "b", "c"}); action != "end" {
		t.Errorf("Expected the flow to return the last action end, got %s\n", action)
	}
}

func TestEmptyActionIsDefault(t *testing.T) {
	first := logNode("first", "")
	first.Then(logNode("second", Default))
	runFlow(t, first, []string{"first", "second"})
}

func TestBranchingFlow(t *testing.T) {
	counter := &Node[testStore, none, none]{
		Post: func(ctx context.Context, s *testStore, _ none, _ none) (Action, error) {
			s.count++
			s.log = append(s.log, "count")
			if s.count < 3 {
				return "again", nil
			}
			return "done", nil
		},
	}
	counter.On("again", counter)
	counter.On("done", logNode("finish", Default))

	runFlow(t, counter, []string{"count", "count", "count", "finish"})
}

func TestNestedFlow(t *testing.T) {
	x, y := logNode("x", Default), logNode("y", "inner_done")
	x.Then(y)
	inner := &Flow[testStore]{Start: x}

	start, end := logNode("start", Default), logNode("end", Default)
	// the inner flow's last action picks what runs after it
	start.Then(inner).On("inner_done", end)

	runFlow(t, start, []string{"start", "x", "y", "end"})
}

func TestFlowEndsWithoutSuccessor(t *testing.T) {
	a := logNode("a", "unknown")
	a.On("known", logNode("b", Default))

	if action := runFlow(t, a, []string{"a"}); action != "unknown" {
		t.Errorf("Expected the flow to return action unknown, got %s\n", action)
	}
}

func TestFlowNoStart(t *testing.T) {
	_, err := (&Flow[testStore]{}).Run(context.Background(), &testStore{})
	if !errors.Is(err, ErrNoStartNode) {
		t.Errorf("Expected error %v, got %v\n", ErrNoStartNode, err)
	}
}

func TestFlowMaxSteps(t *testing.T) {
	loop := logNode("loop", Default)
	loop.Then(loop)

	s := testStore{}
	_, err := (&Flow[testStore]{Start: loop, MaxSteps: 5}).Run(context.Background(), &s)
	if !errors.Is(err, ErrMaxSteps) {
		t.Errorf("Expected error %v, got %v\n", ErrMaxSteps, err)
	}
	if len(s.log) != 5 {
		t.Errorf("Expected 5 steps to run, got %d\n", len(s.log))
	}
}

func TestFlowCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	first := &Node[testStore, none, none]{
		Post: func(ctx context.Context, s *testStore, _ none, _ none) (Action, error) {
			s.log = append(s.log, "first")
			cancel()
			return Default, nil
		},
	}
	first.Then(logNode("second", Default))

	s := testStore{}
	_, err := (&Flow[testStore]{Start: first}).Run(ctx, &s)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Expected error %v, got %v\n", context.Canceled, err)
	}
	if !slices.Equal(s.log, []string{"first"}) {
		t.Errorf("Expected only the first node to run, got %v\n", s.log)
	}
}

func TestFlowStopsOnError(t *testing.T) {
	calls := 0
	failing := &Node[testStore, none, none]{
		Exec: func(ctx context.Context, _ none) (none, error) {
			calls++
			return none{}, errTest
		},
		Post: func(ctx context.Context, s *testStore, _ none, _ none) (Action, error) {
			s.log = append(s.log, "failing")
			return Default, nil
		},
	}
	a := logNode("a", Default)
	a.Then(failing).Then(logNode("c", Default))

	s := testStore{}
	_, err := (&Flow[testStore]{Start: a}).Run(context.Background(), &s)
	// without a retry config the error comes back unwrapped
	if err != errTest {
		t.Errorf("Expected error %v, got %v\n", errTest, err)
	}
	if calls != 1 {
		t.Errorf("Expected Exec to be called once without a retry config, got %d\n", calls)
	}
	if !slices.Equal(s.log, []string{"a"}) {
		t.Errorf("Expected only node a to finish, got %v\n", s.log)
	}
}

func TestNodeRetry(t *testing.T) {
	calls := 0
	node := &Node[testStore, none, int]{
		Exec: func(ctx context.Context, _ none) (int, error) {
			calls++
			if calls < 3 {
				return 0, errTest
			}
			return 42, nil
		},
		Post: func(ctx context.Context, s *testStore, _ none, result int) (Action, error) {
			s.output = result
			return Default, nil
		},
		Retry: retry.NewRetryConfig(retry.WithInitialDelay(time.Millisecond), retry.WithMaxRetries(3)),
	}

	s := testStore{}
	if _, err := node.Run(context.Background(), &s); err != nil {
		t.Fatalf("Unexpected error %v\n", err)
	}
	if calls != 3 || s.output != 42 {
		t.Errorf("Expected 42 after 3 calls, got %d after %d calls\n", s.output, calls)
	}
}

func TestNodeRetryExhausted(t *testing.T) {
	node := &Node[testStore, none, none]{
		Exec: func(ctx context.Context, _ none) (none, error) {
			return none{}, errTest
		},
		Retry: retry.NewRetryConfig(retry.WithInitialDelay(time.Millisecond), retry.WithMaxRetries(2)),
	}

	_, err := node.Run(context.Background(), &testStore{})
	if !errors.Is(err, retry.ErrFunctionNotCompletedMaxRetries) || !errors.Is(err, errTest) {
		t.Errorf("Expected error wrapping %v and %v, got %v\n", retry.ErrFunctionNotCompletedMaxRetries, errTest, err)
	}
}

func TestNodeFallback(t *testing.T) {
	calls := 0
	var fallback_err error
	node := &Node[testStore, int, int]{
		Prep: func(ctx context.Context, s *testStore) (int, error) {
			return s.input, nil
		},
		Exec: func(ctx context.Context, input int) (int, error) {
			calls++
			return 0, errTest
		},
		Fallback: func(ctx context.Context, input int, err error) (int, error) {
			fallback_err = err
			return -input, nil
		},
		Post: func(ctx context.Context, s *testStore, input int, result int) (Action, error) {
			s.output = result
			return Default, nil
		},
		Retry: retry.NewRetryConfig(retry.WithInitialDelay(time.Millisecond), retry.WithMaxRetries(2)),
	}

	s := testStore{input: 5}
	if _, err := node.Run(context.Background(), &s); err != nil {
		t.Fatalf("Unexpected error %v\n", err)
	}
	if calls != 3 {
		t.Errorf("Expected Exec to be retried before falling back, got %d calls\n", calls)
	}
	if !errors.Is(fallback_err, errTest) {
		t.Errorf("Expected fallback to get error %v, got %v\n", errTest, fallback_err)
	}
	if s.output != -5 {
		t.Errorf("Expected the fallback result -5, got %d\n", s.output)
	}
}

func TestNodeFallbackError(t *testing.T) {
	errFallback := errors.New("fallback error")
	node := &Node[testStore, none, none]{
		Exec: func(ctx context.Context, _ none) (none, error) {
			return none{}, errTest
		},
		Fallback: func(ctx context.Context, _ none, err error) (none, error) {
			return none{}, errFallback
		},
	}
	if _, err := node.Run(context.Background(), &testStore{}); err != errFallback {
		t.Errorf("Expected error %v, got %v\n", errFallback, err)
	}
}

func TestNodeFallbackSkippedOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fallback_called := false
	node := &Node[testStore, none, none]{
		Exec: func(ctx context.Context, _ none) (none, error) {
			cancel()
			return none{}, ctx.Err()
		},
		Fallback: func(ctx context.Context, _ none, err error) (none, error) {
			fallback_called = true
			return none{}, nil
		},
	}

	if _, err := node.Run(ctx, &testStore{}); !errors.Is(err, context.Canceled) {
		t.Errorf("Expected error %v, got %v\n", context.Canceled, err)
	}
	if fallback_called {
		t.Error("Fallback should not run after cancellation")
	}
}

func TestBatchNodeOrder(t *testing.T) {
	for _, concurrency := range []int{0, 1, 4, -1} {
		t.Run(fmt.Sprintf("concurrency %d", concurrency), func(t *testing.T) {
			node := &BatchNode[testStore, int, int]{
				Prep: func(ctx context.Context, s *testStore) ([]int, error) {
					return []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}, nil
				},
				Exec: func(ctx context.Context, item int) (int, error) {
					// later items finish first when running in parallel
					time.Sleep(time.Duration(10-item) * time.Millisecond)
					return item * item, nil
				},
				Post: func(ctx context.Context, s *testStore, items []int, results []int) (Action, error) {
					s.results = results
					return Default, nil
				},
				Concurrency: concurrency,
			}

			s := testStore{}
			if _, err := node.Run(context.Background(), &s); err != nil {
				t.Fatalf("Unexpected error %v\n", err)
			}
			expected := []int{0, 1, 4, 9, 16, 25, 36, 49, 64, 81}
			if !slices.Equal(s.results, expected) {
				t.Errorf("Expected results %v, got %v\n", expected, s.results)
			}
		})
	}
}

func TestBatchNodeConcurrencyLimit(t *testing.T) {
	cases := []struct {
		concurrency  int
		max_parallel int32
	}{
		{concurrency: 1, max_parallel: 1},
		{concurrency: 3, max_parallel: 3},
		{concurrency: -1, max_parallel: 10},
	}

	for _, c := range cases {
		t.Run(fmt.Sprintf("concurrency %d", c.concurrency), func(t *testing.T) {
			var in_flight, max_in_flight, started atomic.Int32
			all_started := make(chan struct{})

			node := &BatchNode[testStore, int, int]{
				Prep: func(ctx context.Context, s *testStore) ([]int, error) {
					return make([]int, 10), nil
				},
				Exec: func(ctx context.Context, item int) (int, error) {
					current := in_flight.Add(1)
					defer in_flight.Add(-1)
					for {
						prev := max_in_flight.Load()
						if current <= prev || max_in_flight.CompareAndSwap(prev, current) {
							break
						}
					}

					// the first items wait for each other, so getting past this
					// proves that many items can run at the same time
					if n := started.Add(1); n <= c.max_parallel {
						if n == c.max_parallel {
							close(all_started)
						}
						select {
						case <-all_started:
						case <-time.After(time.Second):
							return 0, errors.New("items did not run concurrently")
						}
					}
					time.Sleep(time.Millisecond)
					return item, nil
				},
				Concurrency: c.concurrency,
			}

			if _, err := node.Run(context.Background(), &testStore{}); err != nil {
				t.Fatalf("Unexpected error %v\n", err)
			}
			if got := max_in_flight.Load(); got != c.max_parallel {
				t.Errorf("Expected at most %d items running at once, got %d\n", c.max_parallel, got)
			}
		})
	}
}

func TestBatchNodeErrorCancelsSiblings(t *testing.T) {
	post_called := false
	node := &BatchNode[testStore, int, int]{
		Prep: func(ctx context.Context, s *testStore) ([]int, error) {
			return []int{0, 1, 2, 3, 4}, nil
		},
		Exec: func(ctx context.Context, item int) (int, error) {
			if item == 0 {
				return 0, errTest
			}
			select {
			case <-ctx.Done():
				return 0, context.Cause(ctx)
			case <-time.After(time.Second):
				return 0, errors.New("item was not cancelled")
			}
		},
		Post: func(ctx context.Context, s *testStore, items []int, results []int) (Action, error) {
			post_called = true
			return Default, nil
		},
		Concurrency: -1,
	}

	start := time.Now()
	_, err := node.Run(context.Background(), &testStore{})
	if !errors.Is(err, errTest) {
		t.Errorf("Expected error %v, got %v\n", errTest, err)
	}
	if elapsed := time.Since(start); elapsed >= time.Second {
		t.Errorf("Expected the other items to be cancelled, took %v\n", elapsed)
	}
	if post_called {
		t.Error("Post should not run when an item fails")
	}
}

func TestBatchNodeSequentialStopsOnError(t *testing.T) {
	executed := []int{}
	node := &BatchNode[testStore, int, int]{
		Prep: func(ctx context.Context, s *testStore) ([]int, error) {
			return []int{0, 1, 2}, nil
		},
		Exec: func(ctx context.Context, item int) (int, error) {
			executed = append(executed, item)
			if item == 1 {
				return 0, errTest
			}
			return item, nil
		},
	}

	if _, err := node.Run(context.Background(), &testStore{}); err != errTest {
		t.Errorf("Expected error %v, got %v\n", errTest, err)
	}
	if !slices.Equal(executed, []int{0, 1}) {
		t.Errorf("Expected items [0 1] to run, got %v\n", executed)
	}
}

func TestBatchNodeRetryPerItem(t *testing.T) {
	var mu sync.Mutex
	attempts := map[int]int{}
	node := &BatchNode[testStore, int, int]{
		Prep: func(ctx context.Context, s *testStore) ([]int, error) {
			return []int{0, 1, 2, 3, 4, 5, 6, 7}, nil
		},
		Exec: func(ctx context.Context, item int) (int, error) {
			mu.Lock()
			attempts[item]++
			attempt := attempts[item]
			mu.Unlock()
			// every item fails its first attempt
			if attempt == 1 {
				return 0, errTest
			}
			return item * 10, nil
		},
		Post: func(ctx context.Context, s *testStore, items []int, results []int) (Action, error) {
			s.results = results
			return Default, nil
		},
		Retry:       retry.NewRetryConfig(retry.WithInitialDelay(time.Millisecond), retry.WithMaxRetries(1)),
		Concurrency: 4,
	}

	s := testStore{}
	if _, err := node.Run(context.Background(), &s); err != nil {
		t.Fatalf("Unexpected error %v\n", err)
	}
	for item, n := range attempts {
		if n != 2 {
			t.Errorf("Expected item %d to be attempted twice, got %d\n", item, n)
		}
	}
	expected := []int{0, 10, 20, 30, 40, 50, 60, 70}
	if !slices.Equal(s.results, expected) {
		t.Errorf("Expected results %v, got %v\n", expected, s.results)
	}
}

func TestBatchNodeFallbackPerItem(t *testing.T) {
	node := &BatchNode[testStore, int, int]{
		Prep: func(ctx context.Context, s *testStore) ([]int, error) {
			return []int{0, 1, 2, 3}, nil
		},
		Exec: func(ctx context.Context, item int) (int, error) {
			if item == 2 {
				return 0, errTest
			}
			return item, nil
		},
		Fallback: func(ctx context.Context, item int, err error) (int, error) {
			return -1, nil
		},
		Post: func(ctx context.Context, s *testStore, items []int, results []int) (Action, error) {
			s.results = results
			return Default, nil
		},
		Concurrency: 4,
	}

	s := testStore{}
	if _, err := node.Run(context.Background(), &s); err != nil {
		t.Fatalf("Unexpected error %v\n", err)
	}
	if expected := []int{0, 1, -1, 3}; !slices.Equal(s.results, expected) {
		t.Errorf("Expected results %v, got %v\n", expected, s.results)
	}
}

func TestBatchNodeEmpty(t *testing.T) {
	exec_called, post_called := false, false
	node := &BatchNode[testStore, int, int]{
		Exec: func(ctx context.Context, item int) (int, error) {
			exec_called = true
			return item, nil
		},
		Post: func(ctx context.Context, s *testStore, items []int, results []int) (Action, error) {
			post_called = true
			if len(results) != 0 {
				t.Errorf("Expected no results, got %v\n", results)
			}
			return Default, nil
		},
		Concurrency: 4,
	}

	if _, err := node.Run(context.Background(), &testStore{}); err != nil {
		t.Fatalf("Unexpected error %v\n", err)
	}
	if exec_called || !post_called {
		t.Errorf("Expected only Post to run, Exec ran: %v, Post ran: %v\n", exec_called, post_called)
	}
}

func TestFlowReuseConcurrently(t *testing.T) {
	// squares 1..input in parallel and sums them
	square := &BatchNode[testStore, int, int]{
		Prep: func(ctx context.Context, s *testStore) ([]int, error) {
			items := []int{}
			for i := 1; i <= s.input; i++ {
				items = append(items, i)
			}
			return items, nil
		},
		Exec: func(ctx context.Context, item int) (int, error) {
			return item * item, nil
		},
		Post: func(ctx context.Context, s *testStore, items []int, results []int) (Action, error) {
			for _, result := range results {
				s.output += result
			}
			return Default, nil
		},
		Concurrency: 4,
	}
	double := &Node[testStore, int, int]{
		Prep: func(ctx context.Context, s *testStore) (int, error) {
			return s.output, nil
		},
		Exec: func(ctx context.Context, sum int) (int, error) {
			return sum * 2, nil
		},
		Post: func(ctx context.Context, s *testStore, sum int, doubled int) (Action, error) {
			s.output = doubled
			return Default, nil
		},
	}
	square.Then(double)
	flow := &Flow[testStore]{Start: square}

	// the same flow runs at the same time against separate stores
	stores := make([]testStore, 8)
	var wg sync.WaitGroup
	for i := range stores {
		stores[i].input = i + 1
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := flow.Run(context.Background(), &stores[i]); err != nil {
				t.Errorf("Unexpected error %v\n", err)
			}
		}()
	}
	wg.Wait()

	for i, s := range stores {
		n := i + 1
		if expected := n * (n + 1) * (2*n + 1) / 6 * 2; s.output != expected {
			t.Errorf("Expected store %d to have output %d, got %d\n", i, expected, s.output)
		}
	}
}

func ExampleFlow() {
	type store struct {
		numbers []int
		total   int
	}

	// squares every number in parallel and adds them up
	square := &BatchNode[store, int, int]{
		Prep: func(ctx context.Context, s *store) ([]int, error) {
			return s.numbers, nil
		},
		Exec: func(ctx context.Context, number int) (int, error) {
			return number * number, nil
		},
		Post: func(ctx context.Context, s *store, numbers []int, squares []int) (Action, error) {
			for _, square := range squares {
				s.total += square
			}
			return Default, nil
		},
		Concurrency: 4,
	}

	// halves the total until it is at most 10
	halve := &Node[store, int, int]{
		Prep: func(ctx context.Context, s *store) (int, error) {
			return s.total, nil
		},
		Exec: func(ctx context.Context, total int) (int, error) {
			return total / 2, nil
		},
		Post: func(ctx context.Context, s *store, total int, half int) (Action, error) {
			s.total = half
			if half > 10 {
				return "again", nil
			}
			return "done", nil
		},
	}

	square.Then(halve).On("again", halve)
	flow := &Flow[store]{Start: square, MaxSteps: 10}

	s := store{numbers: []int{1, 2, 3, 4}}
	action, err := flow.Run(context.Background(), &s)
	fmt.Println(s.total, action, err)
	// Output: 7 done <nil>
}
