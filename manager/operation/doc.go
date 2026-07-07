// Package operation provides the plan model for composing BPF
// lifecycle operations as sequences of typed nodes.
//
// # Plans and nodes
//
// A Plan is an ordered list of nodes. Each node is one of three
// flavours:
//
//   - Produce:  executes an action, stores the result in typed
//     bindings for later nodes.
//   - Do:       executes an action for its side effect, with optional
//     undo.
//   - Try:      best-effort; errors are logged at debug level but do
//     not fail the operation.
//
// The plan interpreter (Run/Run0) walks nodes sequentially, threading
// an executor through each closure. On failure it executes
// accumulated undo actions in reverse order.
//
// # Design trade-offs
//
// Three aspects of the plan model are conscious compromises driven by
// Go's type system. They are documented here so they remain visible
// trade-offs rather than accidental ones.
//
// Forward nodes are closures, not values. The undo side is
// inspectable data (UndoFrom returns []action.Action), but the
// forward side cannot be: nodes have data dependencies (the output of
// node 1 feeds node 2) which in Go require closures. You cannot ask
// "what actions will this plan produce?" without running it. A fully
// value-oriented plan would be a free monad over the Action type, but
// Go cannot express typed continuations without massive ceremony.
// What is lost: structural comparison of plans and pre-execution
// logging. What is preserved: plan-level testing is possible by
// passing a recording executor that collects actions without
// performing I/O.
//
// Bindings use typed keys with runtime panics. Key[T] and Get[T]
// provide compile-time type safety for values, and NewKey panics at
// process startup on duplicate names. Build panics if two Produce
// nodes bind the same key. The remaining gap is ordering: nothing
// verifies at build time that a Get for a key appears only after the
// Produce that binds it. Static checking would require nodes to
// declare key dependencies as data, which pulls the forward path from
// closures into values (see above). The sequential interpreter and
// the convention of declaring keys alongside their producer prevent
// misordering in practice.
//
// The forward/undo asymmetry follows from the above. Forward nodes
// are closures because they have data dependencies; undo actions are
// pure data because they do not. The two halves of a node's
// semantics have different testability characteristics: undo actions
// can be inspected and compared without an executor, forward closures
// cannot. This is a direct consequence of the closure compromise,
// not an independent design choice.
package operation
