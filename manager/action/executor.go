package action

import (
	"context"
	"fmt"
)

// Executor is the interpreter for the action instruction set.
// A single implementation (manager/executor.go) dispatches each
// action to the appropriate I/O subsystem via one type switch.
type Executor interface {
	// Execute runs a single action, discarding any result.
	Execute(ctx context.Context, a Action) error

	// ExecuteResult runs a single action and returns its result.
	// Actions that produce no value return (nil, error).
	ExecuteResult(ctx context.Context, a Action) (any, error)
}

// Produce executes an action and returns the typed result. It
// provides compile-time type safety over the raw any return from
// ExecuteResult.
func Produce[T any](ctx context.Context, exec Executor, a Action) (T, error) {
	result, err := exec.ExecuteResult(ctx, a)
	if err != nil {
		var zero T
		return zero, err
	}

	if result == nil {
		var zero T
		return zero, nil
	}

	typed, ok := result.(T)
	if !ok {
		var zero T
		return zero, fmt.Errorf("action %T produced %T, want %T", a, result, zero)
	}
	return typed, nil
}
