package operation

import (
	"fmt"
	"reflect"
	"sync"
)

// registry holds the type registered for each key name, used by
// NewKey to detect type-confused re-registrations. sync.Map is used
// so concurrent NewKey callers (parallel tests, `go test -count=N`)
// hit an atomic check-and-insert without explicit locking.
var registry sync.Map // map[string]reflect.Type

// Key is a typed reference to a plan binding. The type parameter
// ensures that Get returns the correct type without requiring callers
// to assert.
type Key[T any] struct{ name string }

// NewKey creates a binding key with the given name.
//
// Within a single process, every (name, T) pair must agree:
//
//   - Same name, same T -- returns a Key with that name (idempotent).
//     This is what lets `go test -count=N` and parallel tests
//     re-enter NewKey safely; production callers register at package
//     init.
//
//   - Same name, different T -- panics. Without this check, one call
//     site's Produce(Key[int]("foo"), ...) would store an int at
//     "foo" in a Bindings map, and another site's Get(b,
//     Key[string]("foo")) would type-assert to string at runtime.
//     NewKey moves that failure to registration time (effectively
//     startup) instead of an arbitrary later Get call.
//
// Bindings are per-Run, so name reuse never causes cross-run state
// sharing -- only the type guarantee matters here.
func NewKey[T any](name string) Key[T] {
	t := reflect.TypeFor[T]()
	if existing, loaded := registry.LoadOrStore(name, t); loaded {
		if existing.(reflect.Type) != t {
			panic(fmt.Sprintf("operation.NewKey: %q already registered with type %s (got %s)", name, existing.(reflect.Type), t))
		}
	}
	return Key[T]{name: name}
}

// Bindings stores values produced by Produce nodes during execution.
type Bindings struct{ m map[string]any }

func newBindings() *Bindings { return &Bindings{m: make(map[string]any)} }

// Get retrieves a typed value from bindings. Panics if the key is
// absent; this is a programming error indicating the Produce node was
// skipped or has not run yet.
func Get[T any](b *Bindings, key Key[T]) T {
	v, ok := b.m[key.name]
	if !ok {
		panic(fmt.Sprintf("operation.Get: key %q not bound", key.name))
	}

	val, ok2 := v.(T)
	if !ok2 {
		panic(fmt.Sprintf("operation.Get: key %q has type %T, not %T", key.name, v, val))
	}
	return val
}
