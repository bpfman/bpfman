//go:build e2e

// Detach-contract tests: daemon-process properties that only a
// long-lived `bpfman serve` process can exhibit. The script corpus
// runs every bpfman command as a short-lived subprocess, so process
// exit releases any leaked FD before an assertion can observe it;
// here the daemon outlives every RPC, so reference-lifetime bugs are
// visible in its /proc/<pid>/fd table and in how long kernel link
// objects survive.
//
// Three properties, one test each:
//
//   - The daemon holds no bpf link FDs while a link is attached: the
//     bpffs pin is the link's only reference and the daemon keeps no
//     link state.
//   - When the Detach RPC returns, the kernel link object is already
//     gone: the daemon's close performed the last reference drop
//     synchronously inside close(2).
//   - An out-of-band pin removal releases the link: with no
//     daemon-held FD, removing the pin drops the last reference and
//     the kernel frees the link, rather than leaving a zombie
//     attached and firing until the daemon exits.
package grpcparallel

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cilium/ebpf/link"

	pb "github.com/bpfman/bpfman/server/pb"
)

// loadAndAttachKprobe drives Load + Attach for the kmod-target
// kprobe spec and returns the program and bpfman link IDs. Cleanup
// (Detach where still needed, then Unload) is registered on t.
func loadAndAttachKprobe(ctx context.Context, t *testing.T) (progID, linkID uint32) {
	t.Helper()
	spec := kprobeSpec()

	loadResp, err := client.Load(ctx, &pb.LoadRequest{
		Bytecode: &pb.BytecodeLocation{
			Location: &pb.BytecodeLocation_File{File: testdataPath(spec.object)},
		},
		Info: []*pb.LoadInfo{{
			Name:        spec.progName,
			ProgramType: spec.enumType,
			Info:        spec.loadInfo,
		}},
		Metadata: map[string]string{"test": "grpc_detach_contract"},
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(loadResp.Programs) != 1 || loadResp.Programs[0].KernelInfo == nil {
		t.Fatalf("Load: want 1 program with KernelInfo, got %+v", loadResp.Programs)
	}
	progID = loadResp.Programs[0].KernelInfo.Id
	t.Cleanup(func() {
		if _, err := client.Unload(context.Background(), &pb.UnloadRequest{Id: progID}); err != nil {
			t.Errorf("cleanup Unload %d: %v", progID, err)
		}
	})

	attachResp, err := client.Attach(ctx, &pb.AttachRequest{
		Id:     progID,
		Attach: spec.attachBuilder(t, 0)(),
	})
	if err != nil {
		t.Fatalf("Attach %d: %v", progID, err)
	}
	if attachResp.LinkId == 0 {
		t.Fatalf("Attach %d: returned zero bpfman link id", progID)
	}
	return progID, attachResp.LinkId
}

// linkPinPath returns the daemon's bpffs pin for a bpfman link ID:
// {runtime}/fs/links/{id}.
func linkPinPath(linkID uint32) string {
	return filepath.Join(daemonRuntimeDir, "fs", "links", strconv.FormatUint(uint64(linkID), 10))
}

// kernelLinkIDFromPin opens the pin, reads the kernel link ID, and
// closes the FD again so the test process holds no reference when
// the caller goes on to detach.
func kernelLinkIDFromPin(t *testing.T, pin string) link.ID {
	t.Helper()
	lnk, err := link.LoadPinnedLink(pin, nil)
	if err != nil {
		t.Fatalf("load pinned link %s: %v", pin, err)
	}
	info, err := lnk.Info()
	if err != nil {
		_ = lnk.Close()
		t.Fatalf("link info %s: %v", pin, err)
	}
	if err := lnk.Close(); err != nil {
		t.Fatalf("close link %s: %v", pin, err)
	}
	return info.ID
}

// procFDTargets returns the entries of a /proc/<pid>/fd directory
// whose symlink target contains any of the given tokens.
func procFDTargets(t *testing.T, fdDir string, tokens ...string) []string {
	t.Helper()
	entries, err := os.ReadDir(fdDir)
	if err != nil {
		t.Fatalf("read %s: %v", fdDir, err)
	}
	var out []string
	for _, e := range entries {
		target, err := os.Readlink(filepath.Join(fdDir, e.Name()))
		if err != nil {
			continue // fd closed between ReadDir and Readlink
		}
		for _, tok := range tokens {
			if strings.Contains(target, tok) {
				out = append(out, target+bpfFDInfo(fdDir, e.Name()))
				break
			}
		}
	}
	return out
}

// bpfFDInfo renders the identifying fdinfo lines (prog_id, map_id,
// link_id, prog_tag) for one fd so a failing scan names the kernel
// objects being held, not just their kinds.
func bpfFDInfo(fdDir, fd string) string {
	data, err := os.ReadFile(filepath.Join(filepath.Dir(fdDir), "fdinfo", fd))
	if err != nil {
		return ""
	}
	var ids []string
	for line := range strings.SplitSeq(string(data), "\n") {
		if strings.HasPrefix(line, "prog_id") || strings.HasPrefix(line, "map_id") || strings.HasPrefix(line, "link_id") || strings.HasPrefix(line, "prog_tag") || strings.HasPrefix(line, "map_name") {
			ids = append(ids, strings.Join(strings.Fields(line), "="))
		}
	}
	if len(ids) == 0 {
		return ""
	}
	return "(" + strings.Join(ids, ",") + ")"
}

// bpfLinkFDTargets returns the fd targets naming a bpf link anon
// inode.
func bpfLinkFDTargets(t *testing.T, fdDir string) []string {
	t.Helper()
	return procFDTargets(t, fdDir, "bpf_link", "bpf-link")
}

// bpfFDTargets returns the fd targets naming any bpf object anon
// inode: programs, maps, or links. A quiescent daemon must hold
// none of any kind -- the bpffs pins are the only references.
func bpfFDTargets(t *testing.T, fdDir string) []string {
	t.Helper()
	return procFDTargets(t, fdDir, "bpf_link", "bpf-link", "bpf-prog", "bpf-map")
}

// TestGRPC_DaemonHoldsNoLinkFDsWhileAttached asserts the daemon's fd
// table contains no bpf link FDs while a link is attached: the pin
// is the link's only reference. A daemon that tracks link FDs in
// memory fails this immediately.
func TestGRPC_DaemonHoldsNoLinkFDsWhileAttached(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()

	_, linkID := loadAndAttachKprobe(ctx, t)
	pin := linkPinPath(linkID)
	t.Cleanup(func() {
		if _, err := client.Detach(context.Background(), &pb.DetachRequest{LinkId: linkID}); err != nil {
			t.Errorf("cleanup Detach %d: %v", linkID, err)
		}
	})

	// Validate the fd-table matcher before trusting a zero count:
	// open the pin in this process and require exactly one new
	// matching entry to appear. If the kernel's anon-inode name
	// for links ever changes, this fails loudly instead of letting
	// the daemon assertion pass vacuously.
	before := len(bpfLinkFDTargets(t, "/proc/self/fd"))
	lnk, err := link.LoadPinnedLink(pin, nil)
	if err != nil {
		t.Fatalf("load pinned link %s: %v", pin, err)
	}
	after := len(bpfLinkFDTargets(t, "/proc/self/fd"))
	if err := lnk.Close(); err != nil {
		t.Fatalf("close link: %v", err)
	}
	if after != before+1 {
		t.Fatalf("fd matcher self-check: opening a link changed matching fds %d -> %d, want +1; the anon-inode name assumption is broken", before, after)
	}

	if fds := bpfLinkFDTargets(t, fmt.Sprintf("/proc/%d/fd", daemonPID)); len(fds) != 0 {
		t.Fatalf("daemon holds %d bpf link FD(s) while link %d is attached: %v; the bpffs pin must be the link's only reference", len(fds), linkID, fds)
	}
}

// TestGRPC_KernelLinkReleasedAtDetachReturn asserts that when the
// Detach RPC returns, the kernel link object is already unopenable:
// the daemon's close performed the last reference drop synchronously.
// A daemon that leaks the link FD keeps the ID openable indefinitely
// and fails this deterministically.
func TestGRPC_KernelLinkReleasedAtDetachReturn(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()

	_, linkID := loadAndAttachKprobe(ctx, t)
	kid := kernelLinkIDFromPin(t, linkPinPath(linkID))

	if _, err := client.Detach(ctx, &pb.DetachRequest{LinkId: linkID}); err != nil {
		t.Fatalf("Detach %d: %v", linkID, err)
	}

	l, err := link.NewFromID(kid)
	if err == nil {
		_ = l.Close()
		t.Fatalf("kernel link %d still openable after Detach returned", kid)
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("kernel link %d: want ENOENT after Detach, got: %v", kid, err)
	}
}

// TestGRPC_OutOfBandUnpinReleasesLink asserts that removing the pin
// behind the daemon's back releases the kernel link: with no
// daemon-held FD, the pin's put drops the last reference and the
// kernel frees the link via its deferred path. A daemon that tracks
// link FDs keeps the link alive -- attached and firing, invisible to
// bpfman's own state -- until the daemon exits, and fails this at
// the poll deadline.
func TestGRPC_OutOfBandUnpinReleasesLink(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()

	_, linkID := loadAndAttachKprobe(ctx, t)
	pin := linkPinPath(linkID)
	kid := kernelLinkIDFromPin(t, pin)
	t.Cleanup(func() {
		// The pin is gone; Detach must still succeed and clear
		// the record.
		if _, err := client.Detach(context.Background(), &pb.DetachRequest{LinkId: linkID}); err != nil {
			t.Errorf("cleanup Detach %d: %v", linkID, err)
		}
	})

	if err := os.Remove(pin); err != nil {
		t.Fatalf("out-of-band unpin %s: %v", pin, err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for {
		l, err := link.NewFromID(kid)
		if err == nil {
			_ = l.Close()
		} else if errors.Is(err, os.ErrNotExist) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("kernel link %d still alive 3s after out-of-band unpin: a held reference is keeping a zombie attached", kid)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
