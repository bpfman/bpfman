package logging

import (
	"fmt"
	"strings"
)

// Spec represents a logging specification with a base level and optional
// per-component overrides.
//
// Format: "<base-level>[,<component>=<level>]..."
//
// Examples:
//   - "info" - base level info
//   - "debug" - base level debug
//   - "warn,manager=debug" - base warn, manager at debug
//   - "info,manager=debug,store=trace" - multiple overrides
type Spec struct {
	// BaseLevel is the default level for all components.
	BaseLevel Level `json:"base_level"`

	// Components maps component names to their specific levels.
	Components map[string]Level `json:"components,omitempty"`
}

// ParseSpec parses a log specification string.
// The format is: <base-level>[,<component>=<level>]...
// An empty string defaults to warn level with no component overrides.
func ParseSpec(s string) (Spec, error) {
	spec := Spec{
		BaseLevel:  LevelWarn,
		Components: make(map[string]Level),
	}

	s = strings.TrimSpace(s)
	if s == "" {
		return spec, nil
	}

	parts := strings.Split(s, ",")
	for i, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		// Check if this is a component=level pair
		if before, after, ok := strings.Cut(part, "="); ok {
			component := strings.TrimSpace(before)
			levelStr := strings.TrimSpace(after)

			if component == "" {
				return spec, fmt.Errorf("empty component name in %q", part)
			}

			level, err := ParseLevel(levelStr)
			if err != nil {
				return spec, fmt.Errorf("invalid level for component %q: %w", component, err)
			}

			spec.Components[component] = level
		} else {
			// This is a base level - only valid as the first element
			if i != 0 {
				return spec, fmt.Errorf("base level %q must be first in spec", part)
			}

			level, err := ParseLevel(part)
			if err != nil {
				return spec, err
			}

			spec.BaseLevel = level
		}
	}

	return spec, nil
}

// LevelFor returns the effective level for a component.
// If the component has a specific level configured, that is returned.
// Otherwise, the base level is returned.
func (s *Spec) LevelFor(component string) Level {
	if level, ok := s.Components[component]; ok {
		return level
	}
	return s.BaseLevel
}

// String returns the spec as a parseable string.
func (s *Spec) String() string {
	var parts []string
	parts = append(parts, s.BaseLevel.String())

	for component, level := range s.Components {
		parts = append(parts, fmt.Sprintf("%s=%s", component, level.String()))
	}

	return strings.Join(parts, ",")
}
