package bpfman

import (
	"fmt"
	"strings"
)

// XDPAction represents an XDP return code used for proceed-on
// configuration. It is opaque; construct it with ParseXDPAction.
type XDPAction struct {
	name string
	code int32
}

// The known XDP actions, each pairing a name with its kernel XDP_* return code.
var (
	XDPActionAborted          = XDPAction{"aborted", 0}
	XDPActionDrop             = XDPAction{"drop", 1}
	XDPActionPass             = XDPAction{"pass", 2}
	XDPActionTX               = XDPAction{"tx", 3}
	XDPActionRedirect         = XDPAction{"redirect", 4}
	XDPActionDispatcherReturn = XDPAction{"dispatcher_return", 31}
)

// xdpActionNameToAction maps XDP action names to their domain values.
var xdpActionNameToAction = map[string]XDPAction{
	"aborted":           XDPActionAborted,
	"drop":              XDPActionDrop,
	"pass":              XDPActionPass,
	"tx":                XDPActionTX,
	"redirect":          XDPActionRedirect,
	"dispatcher_return": XDPActionDispatcherReturn,
}

var xdpActionByCode = func() map[int32]XDPAction {
	m := make(map[int32]XDPAction, len(xdpActionNameToAction))
	for _, a := range xdpActionNameToAction {
		m[a.code] = a
	}
	return m
}()

// String returns the action name.
func (a XDPAction) String() string { return a.name }

// Int32 returns the kernel XDP_* return code.
func (a XDPAction) Int32() int32 { return a.code }

// MarshalText implements encoding.TextMarshaler, encoding the action as its name.
func (a XDPAction) MarshalText() ([]byte, error) { return []byte(a.name), nil }

// UnmarshalText implements encoding.TextUnmarshaler, parsing the action
// name case-insensitively.
func (a *XDPAction) UnmarshalText(b []byte) error {
	parsed, err := ParseXDPAction(string(b))
	if err != nil {
		return err
	}

	*a = parsed
	return nil
}

// ParseXDPAction parses a string into an XDP action.
func ParseXDPAction(s string) (XDPAction, error) {
	action, ok := xdpActionNameToAction[strings.TrimSpace(strings.ToLower(s))]
	if !ok {
		return XDPAction{}, fmt.Errorf("unknown XDP action %q", s)
	}
	return action, nil
}

// XDPActionCodes converts XDP actions to kernel int32 codes.
func XDPActionCodes(actions []XDPAction) []int32 {
	result := make([]int32, len(actions))
	for i, action := range actions {
		result[i] = action.Int32()
	}
	return result
}

// XDPActionFromInt32 converts a kernel int32 code to an XDPAction.
// Returns the zero value and an error if the code is not recognised.
func XDPActionFromInt32(code int32) (XDPAction, error) {
	if a, ok := xdpActionByCode[code]; ok {
		return a, nil
	}
	return XDPAction{}, fmt.Errorf("unknown XDP action code %d", code)
}

// XDPActionToString converts an XDP action int32 value to its string name.
func XDPActionToString(action int32) string {
	if a, ok := xdpActionByCode[action]; ok {
		return a.name
	}
	return fmt.Sprintf("unknown(%d)", action)
}

// XDPActionsToString converts a slice of XDP action values to a
// comma-separated string.
func XDPActionsToString(actions []int32) string {
	if len(actions) == 0 {
		return "None"
	}
	names := make([]string, len(actions))
	for i, a := range actions {
		names[i] = XDPActionToString(a)
	}
	return strings.Join(names, ", ")
}
