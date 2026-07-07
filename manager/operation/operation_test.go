package operation

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"testing"

	"github.com/bpfman/bpfman/manager/action"
)

// testAction creates a labelled action for matching in the fake
// executor. It uses action.RemoveProgramDir as a structural stand-in
// because the action.Action interface is sealed within the action
// package; a local test-only action cannot implement it. The
// operation tests do not care about RemoveProgramDir's production
// semantics -- only that the action carries a string field that can
// serve as a label.
func testAction(label string) action.Action {
	return action.RemoveProgramDir{Path: label}
}

// fakeExecutor lets tests configure per-action success or failure.
type fakeExecutor struct {
	errs     map[string]error
	executed []string
}

func newFakeExecutor() *fakeExecutor {
	return &fakeExecutor{errs: make(map[string]error)}
}

func (f *fakeExecutor) failOn(label string, err error) {
	f.errs[label] = err
}

func (f *fakeExecutor) Execute(_ context.Context, a action.Action) error {
	rp, ok := a.(action.RemoveProgramDir)
	if !ok {
		return fmt.Errorf("unexpected action type: %T", a)
	}

	label := rp.Path
	f.executed = append(f.executed, label)
	if err, ok := f.errs[label]; ok {
		return err
	}
	return nil
}

func (f *fakeExecutor) ExecuteResult(ctx context.Context, a action.Action) (any, error) {
	return nil, f.Execute(ctx, a)
}

var _ action.Executor = (*fakeExecutor)(nil)

var errTest = errors.New("test error")

// staticUndo is a test helper that wraps fixed actions in an UndoFrom
// closure.
func staticUndo(actions ...action.Action) NodeOpt {
	return UndoFrom(func(_ *Bindings) []action.Action { return actions })
}

// ---------- Forward execution ----------

func TestDoSuccess(t *testing.T) {
	t.Parallel()

	exec := newFakeExecutor()
	plan := Build(
		Do("action", "t1", func(_ context.Context, _ action.Executor, _ *Bindings) error {
			return nil
		}),
	)

	_, err := Run(context.Background(), slog.Default(), exec, plan)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDoFailure(t *testing.T) {
	t.Parallel()

	exec := newFakeExecutor()
	plan := Build(
		Do("action", "t1", func(_ context.Context, _ action.Executor, _ *Bindings) error {
			return errTest
		}),
	)

	_, err := Run(context.Background(), slog.Default(), exec, plan)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestProduceSuccess(t *testing.T) {
	t.Parallel()

	exec := newFakeExecutor()
	key := NewKey[int]("produce-success-value")
	plan := Build(
		Produce(key, "t1", func(_ context.Context, _ action.Executor, _ *Bindings) (int, error) {
			return 42, nil
		}),
	)

	b, err := Run(context.Background(), slog.Default(), exec, plan)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v := Get(b, key); v != 42 {
		t.Fatalf("expected 42, got %d", v)
	}
}

func TestProduceFailure(t *testing.T) {
	t.Parallel()

	exec := newFakeExecutor()
	key := NewKey[int]("produce-failure-value")
	plan := Build(
		Produce(key, "t1", func(_ context.Context, _ action.Executor, _ *Bindings) (int, error) {
			return 0, errTest
		}),
	)

	_, err := Run(context.Background(), slog.Default(), exec, plan)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestTrySuccess(t *testing.T) {
	t.Parallel()

	exec := newFakeExecutor()
	plan := Build(
		Try("best-effort", "t1", func(_ context.Context, _ action.Executor, _ *Bindings) error {
			return nil
		}),
	)

	_, err := Run(context.Background(), slog.Default(), exec, plan)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestTryFailure(t *testing.T) {
	t.Parallel()

	exec := newFakeExecutor()
	plan := Build(
		Try("best-effort", "t1", func(_ context.Context, _ action.Executor, _ *Bindings) error {
			return errTest
		}),
	)

	_, err := Run(context.Background(), slog.Default(), exec, plan)
	if err != nil {
		t.Fatalf("unexpected error: Try failure should not fail operation")
	}
}

func TestTryAfterPriorFailure(t *testing.T) {
	t.Parallel()

	exec := newFakeExecutor()
	tryCalled := false
	plan := Build(
		Do("fail", "t1", func(_ context.Context, _ action.Executor, _ *Bindings) error {
			return errTest
		}),
		Try("best-effort", "t2", func(_ context.Context, _ action.Executor, _ *Bindings) error {
			tryCalled = true
			return nil
		}),
	)

	_, err := Run(context.Background(), slog.Default(), exec, plan)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if tryCalled {
		t.Fatal("Try node should have been skipped after prior failure")
	}
}

// ---------- Auto-skip ----------

func TestAutoSkipAfterDoFailure(t *testing.T) {
	t.Parallel()

	exec := newFakeExecutor()
	plan := Build(
		Do("first", "t1", func(_ context.Context, _ action.Executor, _ *Bindings) error {
			return errTest
		}),
		Do("second", "t2", func(_ context.Context, _ action.Executor, _ *Bindings) error {
			t.Fatal("should not be called")
			return nil
		}),
	)

	_, err := Run(context.Background(), slog.Default(), exec, plan)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestAutoSkipDoFailsSkipsDoAndTry(t *testing.T) {
	t.Parallel()

	exec := newFakeExecutor()
	plan := Build(
		Do("check", "t1", func(_ context.Context, _ action.Executor, _ *Bindings) error {
			return errTest
		}),
		Do("action", "t2", func(_ context.Context, _ action.Executor, _ *Bindings) error {
			t.Fatal("should not be called")
			return nil
		}),
		Try("try", "t3", func(_ context.Context, _ action.Executor, _ *Bindings) error {
			t.Fatal("should not be called")
			return nil
		}),
	)

	_, err := Run(context.Background(), slog.Default(), exec, plan)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestAutoSkipProduceFailsSkipsDo(t *testing.T) {
	t.Parallel()

	exec := newFakeExecutor()
	key := NewKey[int]("autoskip-val")
	plan := Build(
		Produce(key, "t1", func(_ context.Context, _ action.Executor, _ *Bindings) (int, error) {
			return 0, errTest
		}),
		Do("action", "t2", func(_ context.Context, _ action.Executor, _ *Bindings) error {
			t.Fatal("should not be called")
			return nil
		}),
	)

	_, err := Run(context.Background(), slog.Default(), exec, plan)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ---------- Bindings ----------

func TestProduceStoresBindingForLaterDo(t *testing.T) {
	t.Parallel()

	exec := newFakeExecutor()
	key := NewKey[string]("binding-msg")
	var captured string
	plan := Build(
		Produce(key, "t1", func(_ context.Context, _ action.Executor, _ *Bindings) (string, error) {
			return "hello", nil
		}),
		Do("use", "t2", func(_ context.Context, _ action.Executor, b *Bindings) error {
			captured = Get(b, key)
			return nil
		}),
	)

	_, err := Run(context.Background(), slog.Default(), exec, plan)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if captured != "hello" {
		t.Fatalf("expected hello, got %q", captured)
	}
}

func TestGetMissingKeyPanics(t *testing.T) {
	t.Parallel()

	b := newBindings()
	key := NewKey[int]("get-missing")
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic")
		}
		msg, ok := r.(string)
		if !ok {
			t.Fatalf("expected string panic, got %T", r)
		}
		if msg != `operation.Get: key "get-missing" not bound` {
			t.Fatalf("unexpected panic message: %s", msg)
		}
	}()
	Get(b, key)
}

func TestMultipleProduceBindings(t *testing.T) {
	t.Parallel()

	exec := newFakeExecutor()
	keyA := NewKey[int]("multi-a")
	keyB := NewKey[string]("multi-b")
	plan := Build(
		Produce(keyA, "t1", func(_ context.Context, _ action.Executor, _ *Bindings) (int, error) {
			return 1, nil
		}),
		Produce(keyB, "t2", func(_ context.Context, _ action.Executor, _ *Bindings) (string, error) {
			return "two", nil
		}),
	)

	b, err := Run(context.Background(), slog.Default(), exec, plan)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v := Get(b, keyA); v != 1 {
		t.Fatalf("expected 1, got %d", v)
	}
	if v := Get(b, keyB); v != "two" {
		t.Fatalf("expected two, got %q", v)
	}
}

// ---------- Undo registration ----------

func TestDoWithUndoOnSuccess(t *testing.T) {
	t.Parallel()

	exec := newFakeExecutor()
	// Set up: Do succeeds, then a subsequent Do fails to trigger rollback.
	plan := Build(
		Do("first", "t1", func(_ context.Context, _ action.Executor, _ *Bindings) error {
			return nil
		}, staticUndo(testAction("undo-a"))),
		Do("fail", "t2", func(_ context.Context, _ action.Executor, _ *Bindings) error {
			return errTest
		}),
	)

	_, err := Run(context.Background(), slog.Default(), exec, plan)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	// The undo action should have been executed during rollback.
	if len(exec.executed) != 1 || exec.executed[0] != "undo-a" {
		t.Fatalf("expected undo-a executed, got %v", exec.executed)
	}
}

func TestDoWithUndoOnFailure(t *testing.T) {
	t.Parallel()

	exec := newFakeExecutor()
	plan := Build(
		Do("fail", "t1", func(_ context.Context, _ action.Executor, _ *Bindings) error {
			return errTest
		}, staticUndo(testAction("undo-a"))),
	)

	_, err := Run(context.Background(), slog.Default(), exec, plan)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	// Undo should NOT have been accumulated (node failed).
	if len(exec.executed) != 0 {
		t.Fatalf("expected no undo execution, got %v", exec.executed)
	}
}

func TestProduceWithUndoFromOnSuccess(t *testing.T) {
	t.Parallel()

	exec := newFakeExecutor()
	key := NewKey[string]("undo-from-success-val")
	plan := Build(
		Produce(key, "t1", func(_ context.Context, _ action.Executor, _ *Bindings) (string, error) {
			return "produced", nil
		}, UndoFrom(func(b *Bindings) []action.Action {
			v := Get(b, key)
			return []action.Action{testAction("undo-" + v)}
		})),
		Do("fail", "t2", func(_ context.Context, _ action.Executor, _ *Bindings) error {
			return errTest
		}),
	)

	_, err := Run(context.Background(), slog.Default(), exec, plan)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if len(exec.executed) != 1 || exec.executed[0] != "undo-produced" {
		t.Fatalf("expected undo-produced executed, got %v", exec.executed)
	}
}

func TestProduceWithUndoFromOnFailure(t *testing.T) {
	t.Parallel()

	exec := newFakeExecutor()
	key := NewKey[string]("undo-from-failure-val")
	plan := Build(
		Produce(key, "t1", func(_ context.Context, _ action.Executor, _ *Bindings) (string, error) {
			return "", errTest
		}, UndoFrom(func(_ *Bindings) []action.Action {
			t.Fatal("UndoFrom should not be called on failure")
			return nil
		})),
	)

	_, err := Run(context.Background(), slog.Default(), exec, plan)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if len(exec.executed) != 0 {
		t.Fatalf("expected no undo execution, got %v", exec.executed)
	}
}

func TestDoWithoutUndoNeverAccumulatesUndo(t *testing.T) {
	t.Parallel()

	// Do nodes without undo options should not accumulate any
	// rollback actions.
	exec := newFakeExecutor()
	plan := Build(
		Do("check", "t1", func(_ context.Context, _ action.Executor, _ *Bindings) error {
			return nil
		}),
		Do("fail", "t2", func(_ context.Context, _ action.Executor, _ *Bindings) error {
			return errTest
		}),
	)

	_, err := Run(context.Background(), slog.Default(), exec, plan)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if len(exec.executed) != 0 {
		t.Fatalf("expected no undo execution, got %v", exec.executed)
	}
}

func TestTryNeverAccumulatesUndo(t *testing.T) {
	t.Parallel()

	// Try nodes have no undo. Even if they succeed, no undo entries
	// are accumulated.
	exec := newFakeExecutor()
	plan := Build(
		Try("try", "t1", func(_ context.Context, _ action.Executor, _ *Bindings) error {
			return nil
		}),
		Do("fail", "t2", func(_ context.Context, _ action.Executor, _ *Bindings) error {
			return errTest
		}),
	)

	_, err := Run(context.Background(), slog.Default(), exec, plan)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if len(exec.executed) != 0 {
		t.Fatalf("expected no undo execution, got %v", exec.executed)
	}
}

// ---------- Rollback ----------

func TestRollbackSuccess(t *testing.T) {
	t.Parallel()

	exec := newFakeExecutor()
	plan := Build(
		Do("first", "t1", func(_ context.Context, _ action.Executor, _ *Bindings) error {
			return nil
		}, staticUndo(testAction("undo-a"))),
		Do("fail", "t2", func(_ context.Context, _ action.Executor, _ *Bindings) error {
			return errTest
		}),
	)

	_, err := Run(context.Background(), slog.Default(), exec, plan)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if len(exec.executed) != 1 || exec.executed[0] != "undo-a" {
		t.Fatalf("expected [undo-a] executed, got %v", exec.executed)
	}
}

func TestRollbackFailure(t *testing.T) {
	t.Parallel()

	exec := newFakeExecutor()
	exec.failOn("undo-a", errors.New("undo failed"))
	plan := Build(
		Do("first", "t1", func(_ context.Context, _ action.Executor, _ *Bindings) error {
			return nil
		}, staticUndo(testAction("undo-a"))),
		Do("fail", "t2", func(_ context.Context, _ action.Executor, _ *Bindings) error {
			return errTest
		}),
	)

	_, err := Run(context.Background(), slog.Default(), exec, plan)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	// Undo was attempted even though it failed.
	if len(exec.executed) != 1 || exec.executed[0] != "undo-a" {
		t.Fatalf("expected [undo-a] attempted, got %v", exec.executed)
	}
}

func TestRollbackReversedOrder(t *testing.T) {
	t.Parallel()

	exec := newFakeExecutor()
	plan := Build(
		Do("first", "t1", func(_ context.Context, _ action.Executor, _ *Bindings) error {
			return nil
		}, staticUndo(testAction("undo-a"))),
		Do("second", "t2", func(_ context.Context, _ action.Executor, _ *Bindings) error {
			return nil
		}, staticUndo(testAction("undo-b"))),
		Do("fail", "t3", func(_ context.Context, _ action.Executor, _ *Bindings) error {
			return errTest
		}),
	)

	_, err := Run(context.Background(), slog.Default(), exec, plan)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if len(exec.executed) != 2 {
		t.Fatalf("expected 2 undo executions, got %d", len(exec.executed))
	}
	if exec.executed[0] != "undo-b" || exec.executed[1] != "undo-a" {
		t.Fatalf("expected reversed order [undo-b, undo-a], got %v", exec.executed)
	}
}

func TestRollbackAllAttempted(t *testing.T) {
	t.Parallel()

	exec := newFakeExecutor()
	exec.failOn("undo-b", errors.New("undo-b failed"))
	plan := Build(
		Do("first", "t1", func(_ context.Context, _ action.Executor, _ *Bindings) error {
			return nil
		}, staticUndo(testAction("undo-a"))),
		Do("second", "t2", func(_ context.Context, _ action.Executor, _ *Bindings) error {
			return nil
		}, staticUndo(testAction("undo-b"))),
		Do("fail", "t3", func(_ context.Context, _ action.Executor, _ *Bindings) error {
			return errTest
		}),
	)

	_, err := Run(context.Background(), slog.Default(), exec, plan)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	// Both entries should have been attempted even though undo-b fails.
	if len(exec.executed) != 2 {
		t.Fatalf("expected 2 undo executions, got %d", len(exec.executed))
	}
	if exec.executed[0] != "undo-b" || exec.executed[1] != "undo-a" {
		t.Fatalf("expected [undo-b, undo-a], got %v", exec.executed)
	}
}

func TestNoRollbackEntriesNoUndoExecuted(t *testing.T) {
	t.Parallel()

	exec := newFakeExecutor()
	plan := Build(
		Do("fail", "t1", func(_ context.Context, _ action.Executor, _ *Bindings) error {
			return errTest
		}),
	)

	_, err := Run(context.Background(), slog.Default(), exec, plan)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if len(exec.executed) != 0 {
		t.Fatalf("expected no undo execution, got %v", exec.executed)
	}
}

// ---------- Run / Run0 ----------

func TestRunReturnsBindingsOnSuccess(t *testing.T) {
	t.Parallel()

	exec := newFakeExecutor()
	key := NewKey[int]("run-bindings-val")
	plan := Build(
		Produce(key, "t1", func(_ context.Context, _ action.Executor, _ *Bindings) (int, error) {
			return 7, nil
		}),
	)

	b, err := Run(context.Background(), slog.Default(), exec, plan)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if b == nil {
		t.Fatal("expected non-nil bindings")
	}
	if v := Get(b, key); v != 7 {
		t.Fatalf("expected 7, got %d", v)
	}
}

func TestRunReturnsErrorOnFailure(t *testing.T) {
	t.Parallel()

	exec := newFakeExecutor()
	plan := Build(
		Do("fail", "t1", func(_ context.Context, _ action.Executor, _ *Bindings) error {
			return errTest
		}),
	)

	_, err := Run(context.Background(), slog.Default(), exec, plan)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestRun0ReturnsNilOnSuccess(t *testing.T) {
	t.Parallel()

	exec := newFakeExecutor()
	plan := Build(
		Do("action", "t1", func(_ context.Context, _ action.Executor, _ *Bindings) error {
			return nil
		}),
	)

	err := Run0(context.Background(), slog.Default(), exec, plan)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRun0ReturnsErrorOnFailure(t *testing.T) {
	t.Parallel()

	exec := newFakeExecutor()
	plan := Build(
		Do("fail", "t1", func(_ context.Context, _ action.Executor, _ *Bindings) error {
			return errTest
		}),
	)

	err := Run0(context.Background(), slog.Default(), exec, plan)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ---------- Edge cases ----------

func TestEmptyPlanSuccess(t *testing.T) {
	t.Parallel()

	exec := newFakeExecutor()
	plan := Build()

	b, err := Run(context.Background(), slog.Default(), exec, plan)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if b == nil {
		t.Fatal("expected non-nil bindings")
	}
}

func TestBuildDuplicateProduceKeyPanics(t *testing.T) {
	t.Parallel()

	key := NewKey[int]("build-dup")
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic")
		}
		msg, ok := r.(string)
		if !ok {
			t.Fatalf("expected string panic, got %T", r)
		}
		if msg != `operation.Build: duplicate Produce key "build-dup"` {
			t.Fatalf("unexpected panic message: %s", msg)
		}
	}()
	Build(
		Produce(key, "t1", func(_ context.Context, _ action.Executor, _ *Bindings) (int, error) {
			return 1, nil
		}),
		Produce(key, "t2", func(_ context.Context, _ action.Executor, _ *Bindings) (int, error) {
			return 2, nil
		}),
	)
}

func TestAllTryNodesFailIsSuccess(t *testing.T) {
	t.Parallel()

	exec := newFakeExecutor()
	plan := Build(
		Try("try1", "t1", func(_ context.Context, _ action.Executor, _ *Bindings) error {
			return errors.New("warn 1")
		}),
		Try("try2", "t2", func(_ context.Context, _ action.Executor, _ *Bindings) error {
			return errors.New("warn 2")
		}),
	)

	_, err := Run(context.Background(), slog.Default(), exec, plan)
	if err != nil {
		t.Fatalf("unexpected error: Try failures should not fail the operation")
	}
}
