package operation

import (
	"context"
	"log/slog"
	"time"

	"github.com/bpfman/bpfman/manager/action"
)

// Run executes a plan and returns the bindings on success. On
// failure it rolls back completed steps and returns the error.
func Run(
	ctx context.Context,
	logger *slog.Logger,
	exec action.Executor,
	plan Plan,
) (*Bindings, error) {
	bindings := newBindings()

	undos, opErr := interpret(ctx, logger, exec, plan, bindings)

	if opErr != nil {
		executeRollback(ctx, logger, exec, undos)
		return nil, opErr
	}

	return bindings, nil
}

// Run0 executes a plan that produces no needed result bindings.
func Run0(
	ctx context.Context,
	logger *slog.Logger,
	exec action.Executor,
	plan Plan,
) error {
	_, err := Run(ctx, logger, exec, plan)
	return err
}

// interpret walks the plan nodes in order. On the first failure it
// sets the error and skips all remaining nodes. Returns the
// accumulated undo actions (in forward order; the caller reverses
// them for rollback).
func interpret(
	ctx context.Context,
	logger *slog.Logger,
	exec action.Executor,
	plan Plan,
	bindings *Bindings,
) (undos [][]action.Action, opErr error) {
	for _, n := range plan.nodes {
		if opErr != nil {
			continue
		}

		stepStart := time.Now()
		switch n.flavour {
		case flavourDo:
			err := n.execFn(ctx, exec, bindings)
			logger.DebugContext(ctx, "step", "label", n.label, "target", n.target, "ms", time.Since(stepStart).Milliseconds())
			if err != nil {
				opErr = err
			} else {
				undos = appendUndos(undos, &n, bindings)
			}

		case flavourProduce:
			val, err := n.produceFn(ctx, exec, bindings)
			logger.DebugContext(ctx, "step", "label", n.label, "target", n.target, "ms", time.Since(stepStart).Milliseconds())
			if err != nil {
				opErr = err
			} else {
				bindings.m[n.bindKey] = val
				undos = appendUndos(undos, &n, bindings)
			}

		case flavourTry:
			if err := n.execFn(ctx, exec, bindings); err != nil {
				logger.DebugContext(ctx, "try node failed (non-fatal)", "label", n.label, "target", n.target, "error", err)
			}
			logger.DebugContext(ctx, "step", "label", n.label, "target", n.target, "ms", time.Since(stepStart).Milliseconds())
		}
	}

	return undos, opErr
}

// appendUndos evaluates late-bind undo closures. Called only on
// successful execution of a node. Each node's undo actions form a
// single group (slice).
func appendUndos(undos [][]action.Action, n *node, bindings *Bindings) [][]action.Action {
	if n.undoFn != nil {
		if actions := n.undoFn(bindings); len(actions) > 0 {
			return append(undos, actions)
		}
	}
	return undos
}

// executeRollback walks the undo groups in reverse order, executing
// every action regardless of individual failures. Failures are logged
// at error level.
func executeRollback(
	ctx context.Context,
	logger *slog.Logger,
	exec action.Executor,
	undos [][]action.Action,
) {
	for i := len(undos) - 1; i >= 0; i-- {
		for _, a := range undos[i] {
			if err := exec.Execute(ctx, a); err != nil {
				logger.Log(ctx, slog.LevelError, "rollback failed", "action", a, "error", err)
			}
		}
	}
}
