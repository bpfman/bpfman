package runtime

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/source"
)

// Dispatch sites x head kinds. Right now there is only one
// legal def-shaped head kind, but it still pins the main
// cross-site invariant: top-level def dispatch behaves the
// same at bind, command, defer, and bind-collect producer
// sites. If alias
// resolution or another dispatch shape comes back into play,
// widen the headKinds table rather than cloning per-site
// tests.
//
// Rows are dispatch sites:
//
//   - bind: `let p <- HEAD`
//   - command: `HEAD` at statement position
//   - defer: `defer HEAD`
//   - producer: `let xs <- foreach n in [1] { HEAD }` (bind-
//     collect producer)
//
// Columns are head kinds. Today there is one legal def shape:
//
//   - topLevelDef: `def HEAD() { ... }` at script top level
//
// Adding another name-kind (alias, unknown, etc.) extends the
// matrix in one place rather than per-site. Each current cell
// asserts that the def's body runs and the head does not leak
// into the external dispatch path.

type dispatchSite struct {
	name string
	// script wraps the head invocation in the dispatch
	// shape the site uses. The %q slot receives the head
	// name verbatim. The "sentinel" command at the body
	// fires through ExecCommand when the def actually
	// executes.
	wrap func(headName string) string
	// expectExecBind is true when an unknown head at this
	// site reaches the ExecBind pipeline at runtime; false
	// when it reaches ExecCommand. The negative test
	// asserts the head shows up on the right pipeline so
	// preflight-passes-runtime-still-dispatches is held
	// together.
	expectExecBind bool
	dumpVerb       string
}

type headKind struct {
	name string
	// decl is the script-prefix that brings the head into
	// scope. The head name itself is fixed
	// because the wrap functions interpolate the same name.
	decl string
}

func dispatchSites() []dispatchSite {
	return []dispatchSite{
		{
			name:           "bind",
			wrap:           func(h string) string { return "let p <- " + h + "\n" },
			expectExecBind: true,
			dumpVerb:       "DispatchBind",
		},
		{
			name:           "command",
			wrap:           func(h string) string { return h + "\n" },
			expectExecBind: false,
			dumpVerb:       "DispatchCommand",
		},
		{
			name:           "defer",
			wrap:           func(h string) string { return "defer " + h + "\nprint \"main\"\n" },
			expectExecBind: true,
			dumpVerb:       "RegisterDefer",
		},
		{
			name: "producer",
			wrap: func(h string) string {
				return "let xs <- foreach n in [1] { " + h + " }\n"
			},
			expectExecBind: true,
			dumpVerb:       "DispatchBind",
		},
	}
}

func headKinds() []headKind {
	return []headKind{
		{
			name: "topLevelDef",
			decl: "def headfn() {\n  sentinel\n  return \"ok\"\n}\n",
		},
	}
}

func TestDispatch_Matrix_Sites_x_HeadKinds(t *testing.T) {
	t.Parallel()

	for _, site := range dispatchSites() {
		for _, head := range headKinds() {
			t.Run(site.name+"_"+head.name, func(t *testing.T) {
				t.Parallel()

				src := head.decl + site.wrap("headfn")

				// Top-level-def cells: the def's body
				// runs at runtime. The sentinel command
				// fires through ExecCommand; record and
				// assert it shows up.
				r := &recorder{}
				var commandCalls []string
				env := &Env{
					Session:  NewSession(),
					ExecBind: r.execBind,
					ExecCommand: func(args []Arg, _ source.Span) (Value, error) {
						if len(args) > 0 {
							if w, ok := args[0].(WordArg); ok {
								commandCalls = append(commandCalls, w.Text)
							}
						}
						return Value{}, nil
					},
					RenderDeferFailure: func(source.Pos, []Arg, Envelope) {},
				}
				require.NoError(t, runProgramWithEnv(t, src, env))
				assert.Contains(t, commandCalls, "sentinel", "site=%s head=%s: def body must run; recorded=%v", site.name, head.name, commandCalls)
				// The head name must not have reached
				// ExecBind / ExecCommand as a top-level
				// dispatch -- if it did, the def-lookup
				// precedence drifted at this site.
				for _, c := range commandCalls {
					if c == "headfn" {
						t.Fatalf("site=%s head=%s: head name %q reached ExecCommand as a top-level dispatch", site.name, head.name, c)
					}
				}
				for _, c := range r.calls {
					if len(c.args) == 0 {
						continue
					}
					if w, ok := c.args[0].(WordArg); ok && w.Text == "headfn" {
						t.Fatalf("site=%s head=%s: head name %q reached ExecBind as a top-level dispatch", site.name, head.name, w.Text)
					}
				}
			})
		}
	}
}

// A focused negative test: each dispatch site, when given a
// head name that does NOT exist anywhere in the program (no
// def, no near-miss), passes preflight AND reaches the
// external dispatch path at runtime. Preflight-only checking
// would miss the case where the head still falls through to
// the wrong runtime pipeline.
func TestDispatch_Matrix_UnknownNameAllSitesPassPreflight(t *testing.T) {
	t.Parallel()

	for _, site := range dispatchSites() {
		t.Run(site.name, func(t *testing.T) {
			t.Parallel()
			head := "totally_unknown_command"
			src := site.wrap(head)
			// Preflight must not trip the conditional hint --
			// the name is not in source anywhere.
			issues := checkSource(t, src)
			for _, i := range issues {
				if strings.Contains(i.Msg, "conditional") {
					t.Fatalf("site=%s: unknown name must not trip the conditional hint (got %q)", site.name, i.Msg)
				}
			}
			// Runtime: the unknown head must reach the
			// external dispatch pipeline for the site's
			// shape. Bind-style sites land on ExecBind;
			// command-style sites land on ExecCommand.
			// site.expectExecBind says which pipeline the
			// head should appear on.
			r := &recorder{}
			var commandCalls []string
			env := &Env{
				Session:  NewSession(),
				ExecBind: r.execBind,
				ExecCommand: func(args []Arg, _ source.Span) (Value, error) {
					if len(args) > 0 {
						if w, ok := args[0].(WordArg); ok {
							commandCalls = append(commandCalls, w.Text)
						}
					}
					return Value{}, nil
				},
				RenderDeferFailure: func(source.Pos, []Arg, Envelope) {},
			}
			require.NoError(t, runProgramWithEnv(t, src, env))
			if site.expectExecBind {
				found := false
				for _, c := range r.calls {
					if len(c.args) > 0 {
						if w, ok := c.args[0].(WordArg); ok && w.Text == head {
							found = true
							break
						}
					}
				}
				assert.True(t, found, "site=%s: unknown head %q must reach ExecBind", site.name, head)
			} else {
				assert.Contains(t, commandCalls, head, "site=%s: unknown head must reach ExecCommand", site.name)
			}
		})
	}
}

func TestDispatch_Matrix_LoweredLane_Sites_x_HeadKinds(t *testing.T) {
	t.Parallel()

	type laneHeadKind struct {
		name     string
		decl     string
		headName string
		wantLane string
	}

	headKinds := []laneHeadKind{
		{
			name:     "def",
			decl:     "def headfn() { return 1 }\n",
			headName: "headfn",
			wantLane: "def(headfn)",
		},
		{
			name:     "builtin",
			headName: "print",
			wantLane: "builtin(print)",
		},
		{
			name:     "exec",
			headName: "totally_unknown_command",
			wantLane: "exec",
		},
	}

	for _, site := range dispatchSites() {
		for _, head := range headKinds {
			t.Run(site.name+"_"+head.name, func(t *testing.T) {
				t.Parallel()

				prog := parseProgram(t, head.decl+site.wrap(head.headName))
				lp, err := lowerToIR(prog)
				require.NoError(t, err)

				out := dumpLoweredString(t, lp)
				assert.Contains(t, out, site.dumpVerb, "site=%s head=%s: expected %s in lowered dump\n%s", site.name, head.name, site.dumpVerb, out)
				pat := regexp.MustCompile(`(?m)^  ` + regexp.QuoteMeta(site.dumpVerb) + ` .* lane=` + regexp.QuoteMeta(head.wantLane) + `$`)
				assert.Regexp(t, pat, out, "site=%s head=%s: expected %s line with lane=%s in lowered dump\n%s", site.name, head.name, site.dumpVerb, head.wantLane, out)
			})
		}
	}
}
