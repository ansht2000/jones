package retry

type DurationType int

const (
	UntilSuccess DurationType = iota
	MaxDuration
	MaxRetries
)

type DurationFunc func()