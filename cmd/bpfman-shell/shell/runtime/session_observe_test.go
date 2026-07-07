package runtime

import (
	"maps"
	"slices"
)

// Names returns the visible variable set as a sorted slice, with each
// name appearing once -- inner bindings shadow outer ones, so a name
// bound in several frames is reported once. It is test-only
// observability: the capture and scoping tests use it to inspect frame
// state, while production code reads variables through Get.
func (s *Session) Names() []string {
	seen := make(map[string]struct{})
	for _, f := range s.frames {
		for name := range f {
			seen[name] = struct{}{}
		}
	}
	return slices.Sorted(maps.Keys(seen))
}
