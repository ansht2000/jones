package retry

import "time"

type BackoffType int

const (
	Constant BackoffType = iota
	Exponential
	Fibonacci
	Custom
)

type BackoffFunc func(yield func(time.Duration) bool)

// TODO: look into turning this into a function that returns a map
// research which one is more idiomatic/performant
var backoffMap = map[BackoffType]BackoffFunc{
	Constant: constantBackoff,
	Exponential: exponentialBackoff,
	Fibonacci: fibonacciBackoff,
}

func constantBackoff(yield func(time.Duration) bool) {}

func exponentialBackoff(yield func(time.Duration) bool) {}

func fibonacciBackoff(yield func(time.Duration) bool) {}