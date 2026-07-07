package manager_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bpfman/bpfman"
	"github.com/bpfman/bpfman/kernel"
	"github.com/bpfman/bpfman/manager"
)

// TestAttachRejectsProgramKindMismatch verifies that attaching a loaded
// program via a verb its type cannot drive is rejected cleanly, before
// any kernel or store side effect (Rust parity). The verb to
// program-type mapping is one-to-one except for the probe verbs, which
// each serve both the entry and return variant; those legitimate cases
// are covered as controls that must still succeed.
func TestAttachRejectsProgramKindMismatch(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	loadProg := func(f *testFixture, file, name string, pt bpfman.ProgramType, attachFunc string) bpfman.Program {
		var spec bpfman.LoadSpec
		var err error
		if attachFunc != "" {
			spec, err = bpfman.NewAttachLoadSpec(f.BytecodeFile(file), name, pt, attachFunc)
		} else {
			spec, err = bpfman.NewLoadSpec(f.BytecodeFile(file), name, pt)
		}
		require.NoError(t, err)
		prog, err := f.Load(ctx, spec, manager.LoadOpts{})
		require.NoError(t, err)
		return prog
	}

	// kernelOpsAfterLoad reports the kernel operations recorded after the
	// initial load. A rejected attach must add none.
	opsAfterLoad := func(f *testFixture) []string {
		var ops []string
		for _, op := range f.Kernel.Operations() {
			if op.Op != "load" {
				ops = append(ops, op.Op)
			}
		}
		return ops
	}

	mismatches := []struct {
		name       string
		loadType   bpfman.ProgramType
		loadFile   string
		attachFunc string
		spec       func(id kernel.ProgramID) bpfman.AttachSpec
		wantKind   string
	}{
		{
			name:     "uprobe program via kprobe verb",
			loadType: bpfman.ProgramTypeUprobe, loadFile: "uprobe.o",
			spec: func(id kernel.ProgramID) bpfman.AttachSpec {
				s, err := bpfman.NewKprobeAttachSpec(id, "do_unlinkat")
				require.NoError(t, err)
				return s
			},
			wantKind: "kprobe",
		},
		{
			name:     "kprobe program via uprobe verb",
			loadType: bpfman.ProgramTypeKprobe, loadFile: "kprobe.o",
			spec: func(id kernel.ProgramID) bpfman.AttachSpec {
				s, err := bpfman.NewUprobeAttachSpec(id, "/bin/bash", 0, 0)
				require.NoError(t, err)
				return s.WithFnName("main")
			},
			wantKind: "uprobe",
		},
		{
			name:     "fexit program via fentry verb",
			loadType: bpfman.ProgramTypeFexit, loadFile: "fexit.o", attachFunc: "tcp_close",
			spec: func(id kernel.ProgramID) bpfman.AttachSpec {
				s, err := bpfman.NewFentryAttachSpec(id)
				require.NoError(t, err)
				return s
			},
			wantKind: "fentry",
		},
		{
			name:     "fentry program via fexit verb",
			loadType: bpfman.ProgramTypeFentry, loadFile: "fentry.o", attachFunc: "tcp_connect",
			spec: func(id kernel.ProgramID) bpfman.AttachSpec {
				s, err := bpfman.NewFexitAttachSpec(id)
				require.NoError(t, err)
				return s
			},
			wantKind: "fexit",
		},
		{
			name:     "kprobe program via tracepoint verb",
			loadType: bpfman.ProgramTypeKprobe, loadFile: "kprobe.o",
			spec: func(id kernel.ProgramID) bpfman.AttachSpec {
				s, err := bpfman.NewTracepointAttachSpecFromString(id, "syscalls/sys_enter_kill")
				require.NoError(t, err)
				return s
			},
			wantKind: "tracepoint",
		},
		{
			name:     "xdp program via tc verb",
			loadType: bpfman.ProgramTypeXDP, loadFile: "xdp.o",
			spec: func(id kernel.ProgramID) bpfman.AttachSpec {
				s, err := bpfman.NewTCAttachSpec(id, "lo", bpfman.TCDirectionIngress, 50)
				require.NoError(t, err)
				return s
			},
			wantKind: "tc",
		},
		{
			name:     "tc program via xdp verb",
			loadType: bpfman.ProgramTypeTC, loadFile: "tc.o",
			spec: func(id kernel.ProgramID) bpfman.AttachSpec {
				s, err := bpfman.NewXDPAttachSpec(id, "lo", 50)
				require.NoError(t, err)
				return s
			},
			wantKind: "xdp",
		},
		{
			name:     "xdp program via tcx verb",
			loadType: bpfman.ProgramTypeXDP, loadFile: "xdp.o",
			spec: func(id kernel.ProgramID) bpfman.AttachSpec {
				s, err := bpfman.NewTCXAttachSpec(id, "lo", bpfman.TCDirectionIngress, 50)
				require.NoError(t, err)
				return s
			},
			wantKind: "tcx",
		},
	}

	for _, tc := range mismatches {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newTestFixture(t)
			prog := loadProg(f, tc.loadFile, "prog", tc.loadType, tc.attachFunc)

			_, err := f.Attach(ctx, tc.spec(prog.Record.ProgramID))

			require.Error(t, err, "cross-attach must be rejected")
			var mismatch bpfman.ErrAttachKindMismatch
			require.ErrorAs(t, err, &mismatch, "expected ErrAttachKindMismatch")
			assert.Equal(t, tc.loadType, mismatch.ActualType)
			assert.Equal(t, tc.wantKind, mismatch.AttachKind)
			assert.Empty(t, opsAfterLoad(f), "rejected attach must perform no kernel work")
			links, err := f.Manager.ListLinksByProgram(ctx, prog.Record.ProgramID)
			require.NoError(t, err)
			assert.Empty(t, links, "rejected attach must leave no link record")
		})
	}

	// Controls: the probe verbs legitimately serve both the entry and
	// the return variant. These must NOT be rejected by the kind guard.
	controls := []struct {
		name     string
		loadType bpfman.ProgramType
		loadFile string
		spec     func(id kernel.ProgramID) bpfman.AttachSpec
	}{
		{
			name:     "kretprobe program via kprobe verb",
			loadType: bpfman.ProgramTypeKretprobe, loadFile: "kprobe.o",
			spec: func(id kernel.ProgramID) bpfman.AttachSpec {
				s, err := bpfman.NewKprobeAttachSpec(id, "do_unlinkat")
				require.NoError(t, err)
				return s
			},
		},
		{
			name:     "uretprobe program via uprobe verb",
			loadType: bpfman.ProgramTypeUretprobe, loadFile: "uprobe.o",
			spec: func(id kernel.ProgramID) bpfman.AttachSpec {
				s, err := bpfman.NewUprobeAttachSpec(id, "/bin/bash", 0, 0)
				require.NoError(t, err)
				return s.WithFnName("main")
			},
		},
	}

	for _, tc := range controls {
		t.Run(tc.name+" (control, must succeed)", func(t *testing.T) {
			t.Parallel()
			f := newTestFixture(t)
			prog := loadProg(f, tc.loadFile, "prog", tc.loadType, "")
			_, err := f.Attach(ctx, tc.spec(prog.Record.ProgramID))
			require.NoError(t, err, "legitimate probe-variant attach must not be rejected")
		})
	}
}
