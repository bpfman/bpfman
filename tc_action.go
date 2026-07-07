package bpfman

import (
	"fmt"
	"strings"
)

// TCAction represents a TC action return code: a named kernel TC_ACT_*
// value. The name and its kernel code are a single domain value, not a
// label with an incidental lookup, so TCAction is a value object with
// unexported fields. Construct it via the package-level constants or
// ParseTCAction; a TCAction so obtained always carries a valid code, so
// Int32 is total and there is no forged-value-maps-to-zero case.
type TCAction struct {
	name string
	code int32
}

// The known TC actions, each pairing a name with its kernel TC_ACT_*
// return code.
var (
	TCActionUnspec           = TCAction{"unspec", -1}
	TCActionOK               = TCAction{"ok", 0}
	TCActionReclassify       = TCAction{"reclassify", 1}
	TCActionShot             = TCAction{"shot", 2}
	TCActionPipe             = TCAction{"pipe", 3}
	TCActionStolen           = TCAction{"stolen", 4}
	TCActionQueued           = TCAction{"queued", 5}
	TCActionRepeat           = TCAction{"repeat", 6}
	TCActionRedirect         = TCAction{"redirect", 7}
	TCActionTrap             = TCAction{"trap", 8}
	TCActionDispatcherReturn = TCAction{"dispatcher_return", 30}
)

// allTCActions is the canonical list of valid TC actions; the name and
// code lookups derive from it.
var allTCActions = []TCAction{
	TCActionUnspec,
	TCActionOK,
	TCActionReclassify,
	TCActionShot,
	TCActionPipe,
	TCActionStolen,
	TCActionQueued,
	TCActionRepeat,
	TCActionRedirect,
	TCActionTrap,
	TCActionDispatcherReturn,
}

// tcActionByName maps action names to their domain values (for parsing).
var tcActionByName = func() map[string]TCAction {
	m := make(map[string]TCAction, len(allTCActions))
	for _, a := range allTCActions {
		m[a.name] = a
	}
	return m
}()

// tcActionByCode maps kernel int32 codes to their domain values.
var tcActionByCode = func() map[int32]TCAction {
	m := make(map[int32]TCAction, len(allTCActions))
	for _, a := range allTCActions {
		m[a.code] = a
	}
	return m
}()

// String returns the action name.
func (a TCAction) String() string { return a.name }

// Int32 returns the kernel TC_ACT_* return code.
func (a TCAction) Int32() int32 { return a.code }

// MarshalText implements encoding.TextMarshaler, encoding the action as its name.
func (a TCAction) MarshalText() ([]byte, error) { return []byte(a.name), nil }

// UnmarshalText implements encoding.TextUnmarshaler, parsing the action
// name case-insensitively.
func (a *TCAction) UnmarshalText(b []byte) error {
	parsed, err := ParseTCAction(string(b))
	if err != nil {
		return err
	}

	*a = parsed
	return nil
}

// ParseTCAction parses a string into a TCAction (case-insensitive).
func ParseTCAction(s string) (TCAction, error) {
	action, ok := tcActionByName[strings.TrimSpace(strings.ToLower(s))]
	if !ok {
		return TCAction{}, fmt.Errorf("unknown TC action %q", s)
	}
	return action, nil
}

// TCActionNames returns all valid TC action names as strings.
func TCActionNames() []string {
	names := make([]string, len(allTCActions))
	for i, a := range allTCActions {
		names[i] = a.name
	}
	return names
}

// TCActionCodes converts TC actions to kernel int32 codes.
func TCActionCodes(actions []TCAction) []int32 {
	result := make([]int32, len(actions))
	for i, action := range actions {
		result[i] = action.Int32()
	}
	return result
}

// TCActionFromInt32 converts a kernel int32 code to a TCAction.
// Returns the zero value and an error if the code is not recognised.
func TCActionFromInt32(code int32) (TCAction, error) {
	if a, ok := tcActionByCode[code]; ok {
		return a, nil
	}
	return TCAction{}, fmt.Errorf("unknown TC action code %d", code)
}

// TCActionToString converts a TC action int32 value to its string name.
func TCActionToString(action int32) string {
	if a, ok := tcActionByCode[action]; ok {
		return a.name
	}
	return fmt.Sprintf("unknown(%d)", action)
}

// TCActionsToString converts a slice of TC action values to a
// comma-separated string.
func TCActionsToString(actions []int32) string {
	if len(actions) == 0 {
		return "None"
	}
	names := make([]string, len(actions))
	for i, a := range actions {
		names[i] = TCActionToString(a)
	}
	return strings.Join(names, ", ")
}
