package fs_test

import (
	"os"
	"path/filepath"
	"testing"

	bpfman "github.com/bpfman/bpfman"
	"github.com/bpfman/bpfman/fs"
)

func TestBPFFS_RemoveDispatcherProgPin_ValidatesNameAndParent(t *testing.T) {
	t.Parallel()

	root, err := fs.New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	b := root.BPFFS()

	// Create fake bpffs mount tree for the test.
	if err := os.MkdirAll(b.XDP(), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	revDir := filepath.Join(b.XDP(), "dispatcher_1_2_3")
	if err := os.MkdirAll(revDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// Valid dispatcher prog pin.
	okPin := filepath.Join(revDir, "dispatcher")
	if err := os.WriteFile(okPin, []byte("x"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if err := b.RemoveDispatcherProgPin(bpfman.ProgPinPath(okPin)); err != nil {
		t.Errorf("RemoveDispatcherProgPin(%s) = %v; want nil", okPin, err)
	}

	// Wrong filename - should fail.
	badPin := filepath.Join(revDir, "not-dispatcher")
	if err := os.WriteFile(badPin, []byte("x"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if err := b.RemoveDispatcherProgPin(bpfman.ProgPinPath(badPin)); err == nil {
		t.Errorf("RemoveDispatcherProgPin(%s) = nil; want error", badPin)
	}

	// Wrong parent dir pattern - should fail.
	badDir := filepath.Join(b.XDP(), "dispatcher_NOPE")
	if err := os.MkdirAll(badDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	badPin2 := filepath.Join(badDir, "dispatcher")
	if err := os.WriteFile(badPin2, []byte("x"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if err := b.RemoveDispatcherProgPin(bpfman.ProgPinPath(badPin2)); err == nil {
		t.Errorf("RemoveDispatcherProgPin(%s) = nil; want error", badPin2)
	}
}

func TestBPFFS_RemoveDispatcherRevDir_RefusesMountRoot(t *testing.T) {
	t.Parallel()

	root, err := fs.New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	b := root.BPFFS()
	if err := os.MkdirAll(b.MountPoint(), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	if err := b.RemoveDispatcherRevDir(bpfman.DispatcherRevDir(b.MountPoint())); err == nil {
		t.Errorf("RemoveDispatcherRevDir(mount root) = nil; want error")
	}
}

func TestBPFFS_RemoveProgPin_ValidatesNumericSuffix(t *testing.T) {
	t.Parallel()

	root, err := fs.New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	b := root.BPFFS()
	if err := os.MkdirAll(b.MountPoint(), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// Valid numeric suffix.
	ok := filepath.Join(b.MountPoint(), "prog_123")
	if err := os.WriteFile(ok, []byte("x"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if err := b.RemoveProgPin(bpfman.ProgPinPath(ok)); err != nil {
		t.Errorf("RemoveProgPin(%s) = %v; want nil", ok, err)
	}

	// Non-numeric suffix - should fail.
	bad := filepath.Join(b.MountPoint(), "prog_abc")
	if err := os.WriteFile(bad, []byte("x"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if err := b.RemoveProgPin(bpfman.ProgPinPath(bad)); err == nil {
		t.Errorf("RemoveProgPin(%s) = nil; want error", bad)
	}
}

func TestBPFFS_RemoveDispatcherLinkPin_ValidatesPattern(t *testing.T) {
	t.Parallel()

	root, err := fs.New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	b := root.BPFFS()
	if err := os.MkdirAll(b.XDP(), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// Valid dispatcher link pin.
	ok := filepath.Join(b.XDP(), "dispatcher_1_2_link")
	if err := os.WriteFile(ok, []byte("x"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if err := b.RemoveDispatcherLinkPin(bpfman.LinkPath(ok)); err != nil {
		t.Errorf("RemoveDispatcherLinkPin(%s) = %v; want nil", ok, err)
	}

	// Wrong pattern - should fail.
	bad := filepath.Join(b.XDP(), "dispatcher_abc_def_link")
	if err := os.WriteFile(bad, []byte("x"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if err := b.RemoveDispatcherLinkPin(bpfman.LinkPath(bad)); err == nil {
		t.Errorf("RemoveDispatcherLinkPin(%s) = nil; want error", bad)
	}
}

func TestBPFFS_SafeRemoveAll_RefusesEscape(t *testing.T) {
	t.Parallel()

	root, err := fs.New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	b := root.BPFFS()
	if err := os.MkdirAll(b.MountPoint(), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// Path outside mount should fail.
	err = b.SafeRemoveAll("/tmp/outside")
	if err == nil {
		t.Error("SafeRemoveAll(/tmp/outside) = nil; want error")
	}
}
