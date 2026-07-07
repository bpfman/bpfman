package bpfmanbuiltin

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/driver"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/runtime"
	"github.com/bpfman/bpfman/internal/registryfixture"
)

func TestLoadImageArgsFromLoadFilePreservesOptions(t *testing.T) {
	t.Parallel()

	gotArgs, err := loadImageArgsFromLoadFile([]runtime.Arg{
		word("program"),
		word("load"),
		word("file"),
		word("testdata/bpf/xdp_pass.bpf.o"),
		word("--programs"), word("xdp:pass"),
		word("--metadata"), word("owner=e2e"),
		word("--global"), word("threshold=0x01020304"),
		word("--application"), word("suite"),
		word("--map-owner-id"), word("42"),
		word("-o"), word("json"),
	}, "127.0.0.1:5000/bpfman-e2e/xdp-pass:tag")
	if err != nil {
		t.Fatalf("loadImageArgsFromLoadFile: %v", err)
	}

	got := driver.ArgTexts(gotArgs)
	want := []string{
		"program", "load", "image",
		"127.0.0.1:5000/bpfman-e2e/xdp-pass:tag",
		"--pull-policy", "Always",
		"--programs", "xdp:pass",
		"--metadata", "owner=e2e",
		"--global", "threshold=0x01020304",
		"--application", "suite",
		"--map-owner-id", "42",
		"-o", "json",
	}
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("args mismatch\ngot:  %#v\nwant: %#v", got, want)
	}
}

func TestPlanBrokeredBuildHandsAbsolutePath(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	absBytecode := filepath.Join(repoRoot, "e2e", "testdata", "bpf", "xdp_pass.bpf.o")

	plan, err := planBrokeredBuild(repoRoot, absBytecode)
	if err != nil {
		t.Fatalf("planBrokeredBuild: %v", err)
	}

	// The image build runs in a child process. If the bytecode path
	// is relative, it resolves only when the child's working
	// directory is set to the repo root -- the cwd-coupling this
	// guards against. Demand an absolute, working-directory-independent
	// path so the build cannot depend on where the child is spawned.
	if !filepath.IsAbs(plan.Bytecode) {
		t.Fatalf("planBrokeredBuild Bytecode = %q, want an absolute path", plan.Bytecode)
	}
	if plan.Bytecode != absBytecode {
		t.Fatalf("planBrokeredBuild Bytecode = %q, want %q", plan.Bytecode, absBytecode)
	}
}

func TestSanitiseImageComponent(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"xdp_pass.bpf":     "xdp-pass-bpf",
		"TC Counter":       "tc-counter",
		"___":              "bytecode",
		"Already-OK_123":   "already-ok-123",
		"multi..prog..xdp": "multi-prog-xdp",
	}
	for input, want := range tests {
		if got := registryfixture.SanitiseComponent(input); got != want {
			t.Fatalf("SanitiseComponent(%q) = %q, want %q", input, got, want)
		}
	}
}
