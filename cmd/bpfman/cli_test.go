package main

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/alecthomas/kong"
	"github.com/cilium/ebpf"
)

// A failed program load carries the kernel verifier log inside a
// *ebpf.VerifierError. Its Error() summarises to the last line or two,
// but the full log is the primary diagnostic when the verifier rejects
// a program, so formatError renders every line rather than the summary.
func TestFormatError_VerifierErrorShowsFullLog(t *testing.T) {
	t.Parallel()

	verr := &ebpf.VerifierError{
		Cause: errors.New("permission denied"),
		Log:   []string{"0: (bf) r6 = r1", "1: (85) call bpf_probe_read#4", "R1 type=inv expected=fp", "processed 2 insns"},
	}
	wrapped := fmt.Errorf("failed to load programs: %w", fmt.Errorf("failed to load collection: %w", verr))

	out := (&CLI{}).formatError(wrapped).Error()

	for _, line := range []string{"0: (bf) r6 = r1", "1: (85) call bpf_probe_read#4", "R1 type=inv expected=fp"} {
		if !strings.Contains(out, line) {
			t.Errorf("verifier log line %q missing from rendered error:\n%s", line, out)
		}
	}
	if strings.Contains(out, "omitted") {
		t.Errorf("rendered error still truncates the log:\n%s", out)
	}
}

func TestMaybeInjectServe(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		args []string
		want []string
	}{
		{
			"no args",
			[]string{"bpfman"},
			[]string{"bpfman"},
		},
		{
			"csi-support only",
			[]string{"bpfman", "--csi-support"},
			[]string{"bpfman", "serve", "--csi-support"},
		},
		{
			"csi-support followed by another flag",
			[]string{"bpfman", "--csi-support", "--socket-path=/run/x.sock"},
			[]string{"bpfman", "serve", "--csi-support", "--socket-path=/run/x.sock"},
		},
		{
			"csi-support followed by separated flag value",
			[]string{"bpfman", "--csi-support", "--socket-path", "/run/x.sock"},
			[]string{"bpfman", "serve", "--csi-support", "--socket-path", "/run/x.sock"},
		},
		{
			"explicit subcommand",
			[]string{"bpfman", "get", "link", "5"},
			[]string{"bpfman", "get", "link", "5"},
		},
		{
			"version subcommand",
			[]string{"bpfman", "version"},
			[]string{"bpfman", "version"},
		},
		{
			"explicit serve",
			[]string{"bpfman", "serve", "--csi-support"},
			[]string{"bpfman", "serve", "--csi-support"},
		},
		{
			"non-marker flag alone",
			[]string{"bpfman", "--socket-path=/run/x.sock"},
			[]string{"bpfman", "--socket-path=/run/x.sock"},
		},
		{
			"marker not at argv[1]",
			[]string{"bpfman", "--socket-path=/run/x.sock", "--csi-support"},
			[]string{"bpfman", "--socket-path=/run/x.sock", "--csi-support"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := maybeInjectServe(tc.args)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("maybeInjectServe(%v) = %v, want %v", tc.args, got, tc.want)
			}
		})
	}
}

func TestRootHelpShowsOnlyPublicLifecycleCommands(t *testing.T) {
	t.Parallel()

	type helpExit struct{}

	var cli CLI
	var out bytes.Buffer
	parser, err := kong.New(&cli, append(KongOptions(),
		kong.Writers(&out, &out),
		kong.Exit(func(int) { panic(helpExit{}) }),
	)...)
	if err != nil {
		t.Fatalf("kong.New: %v", err)
	}

	exited := false
	func() {
		defer func() {
			r := recover()
			if r == nil {
				return
			}
			if _, ok := r.(helpExit); !ok {
				panic(r)
			}
			exited = true
		}()
		_, err = parser.Parse([]string{"--help"})
	}()
	if err != nil {
		t.Fatalf("Parse(--help): %v", err)
	}
	if !exited {
		t.Fatal("Parse(--help) did not exit after printing help")
	}

	help := out.String()
	for _, want := range []string{
		"program load file",
		"program load image",
		"program unload",
		"program get",
		"program list",
		"link attach xdp",
		"link attach tc",
		"link attach tcx",
		"link attach tracepoint",
		"link attach kprobe",
		"link attach uprobe",
		"link attach fentry",
		"link attach fexit",
		"link detach",
		"link get",
		"link list",
		"version",
	} {
		if !strings.Contains(help, want) {
			t.Fatalf("root help missing public command %q:\n%s", want, help)
		}
	}

	for _, hidden := range []string{
		"program delete",
		"link delete",
		"dispatcher list",
		"dispatcher get",
		"image build",
		"image generate-build-args",
		"image inspect",
		"image verify",
		"serve",
	} {
		if strings.Contains(help, hidden) {
			t.Fatalf("root help exposes hidden command %q:\n%s", hidden, help)
		}
	}
}
