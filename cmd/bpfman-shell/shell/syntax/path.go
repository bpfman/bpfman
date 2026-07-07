package syntax

import (
	"fmt"
	"strconv"
	"strings"
)

// PathStep is one navigational component of a dotted/indexed value
// path such as "maps[0].name".
type PathStep struct {
	// Field is the field name to descend into; meaningful only when
	// IsIndex is false.
	Field string

	// Index is the element position to select; meaningful only when
	// IsIndex is true.
	Index int

	// IsIndex distinguishes a numeric "[n]" step (true) from a named
	// field step (false).
	IsIndex bool
}

// ParsePath parses a dotted path with optional [n] indexing into a
// sequence of steps.
func ParsePath(path string) ([]PathStep, error) {
	var steps []PathStep
	i := 0
	for i < len(path) {
		if path[i] == '.' {
			i++
			if i >= len(path) {
				break
			}
		}

		if path[i] == '[' {
			j := strings.IndexByte(path[i:], ']')
			if j < 0 {
				return nil, fmt.Errorf("unterminated [ in path %q", path)
			}
			numStr := path[i+1 : i+j]
			n, err := strconv.Atoi(numStr)
			if err != nil {
				return nil, fmt.Errorf("invalid index in path %q: %w", path, err)
			}

			steps = append(steps, PathStep{Index: n, IsIndex: true})
			i += j + 1
			continue
		}

		j := i
		for j < len(path) && path[j] != '.' && path[j] != '[' {
			j++
		}
		steps = append(steps, PathStep{Field: path[i:j]})
		i = j
	}
	return steps, nil
}

// isValidPath reports whether path is syntactically valid and names
// at least one step.
func isValidPath(path string) bool {
	if path == "" {
		return false
	}
	steps, err := ParsePath(path)
	return err == nil && len(steps) > 0
}
