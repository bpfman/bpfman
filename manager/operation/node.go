package operation

import (
	"context"
	"fmt"

	"github.com/bpfman/bpfman/manager/action"
)

// nodeFlavour distinguishes the three node types in a plan.
type nodeFlavour uint8

const (
	flavourProduce nodeFlavour = iota
	flavourDo
	flavourTry
)

// node is the internal representation of a plan step. Callers
// construct nodes via the Produce, Do, and Try functions; the struct
// fields are unexported.
type node struct {
	label   string
	flavour nodeFlavour
	target  string

	// Exactly one of these is set, depending on flavour.
	// do/try use execFn; produce uses produceFn.
	execFn    func(context.Context, action.Executor, *Bindings) error
	produceFn func(context.Context, action.Executor, *Bindings) (any, error)

	// For produce: the key name used to store the binding.
	bindKey string

	// Options (only meaningful for produce/do).
	undoFn func(*Bindings) []action.Action
}

// Node is the public type alias for plan nodes. Callers receive
// values from the constructor functions but cannot construct the
// underlying struct directly because its fields are unexported.
type Node = node

// Produce creates a value-producing node. The returned value is stored
// under the given key and can be retrieved by later nodes via Get.
func Produce[T any](key Key[T], target string,
	fn func(context.Context, action.Executor, *Bindings) (T, error),
	opts ...NodeOpt,
) Node {
	n := node{
		label:   key.name,
		flavour: flavourProduce,
		target:  target,
		bindKey: key.name,
		produceFn: func(ctx context.Context, exec action.Executor, b *Bindings) (any, error) {
			return fn(ctx, exec, b)
		},
	}
	for _, o := range opts {
		o.applyNodeOpt(&n)
	}
	return n
}

// Do creates a side-effecting node. Do nodes support undo via
// UndoFrom.
func Do(label string, target string,
	fn func(context.Context, action.Executor, *Bindings) error,
	opts ...NodeOpt,
) Node {
	n := node{
		label:   label,
		flavour: flavourDo,
		target:  target,
		execFn:  fn,
	}
	for _, o := range opts {
		o.applyNodeOpt(&n)
	}
	return n
}

// Try creates a best-effort node. If the function returns an error,
// the operation continues without setting the error state. Try nodes
// have no undo.
func Try(label string, target string,
	fn func(context.Context, action.Executor, *Bindings) error,
) Node {
	return node{
		label:   label,
		flavour: flavourTry,
		target:  target,
		execFn:  fn,
	}
}

// DoAction creates a Do node that executes a single action. This is
// a convenience wrapper around Do for the common case where the
// closure simply calls exec.Execute with a fixed action value.
func DoAction(label, target string, a action.Action, opts ...NodeOpt) Node {
	return Do(label, target,
		func(ctx context.Context, exec action.Executor, _ *Bindings) error {
			return exec.Execute(ctx, a)
		},
		opts...,
	)
}

// NodeOpt configures optional behaviour on Produce and Do nodes.
type NodeOpt interface {
	applyNodeOpt(*node)
}

type nodeOptFunc func(*node)

func (f nodeOptFunc) applyNodeOpt(n *node) { f(n) }

// UndoFrom declares late-bind undo: the closure is called after
// successful execution to compute undo actions from current bindings.
func UndoFrom(fn func(*Bindings) []action.Action) NodeOpt {
	return nodeOptFunc(func(n *node) {
		n.undoFn = fn
	})
}

// Plan is an ordered list of nodes describing a complete operation.
type Plan struct{ nodes []node }

// Build constructs a Plan from the given nodes. It panics if two
// Produce nodes bind the same key, catching the error at plan
// construction time rather than during execution.
func Build(nodes ...Node) Plan {
	seen := map[string]bool{}
	for _, n := range nodes {
		if n.flavour == flavourProduce {
			if seen[n.bindKey] {
				panic(fmt.Sprintf("operation.Build: duplicate Produce key %q", n.bindKey))
			}
			seen[n.bindKey] = true
		}
	}
	return Plan{nodes: nodes}
}
