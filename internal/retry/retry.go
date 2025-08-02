package retry

import "context"

type RetryFunc func(ctx context.Context) error

type RetryFuncWithReturn[T any] func(ctx context.Context) (T, error)

func Retry(ctx context.Context, retry_func RetryFunc, retry_config RetryConfig) error {
	return nil
}
