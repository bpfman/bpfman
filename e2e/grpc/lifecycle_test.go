//go:build e2e

package grpcparallel

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bpfman/bpfman/e2e/testnet"
	"github.com/bpfman/bpfman/e2e/uprobetarget"
	pb "github.com/bpfman/bpfman/server/pb"
)

// Per-lifecycle RPC accounting, grouped by daemon-side
// serialisation behaviour so the summary can report each path's
// rate separately. Bump these if runOneLifecycle's call list
// changes; the sum must equal rpcsPerLifecycle, otherwise the
// summary numbers stop adding up.
//
//   - loadsPerLifecycle: Load -- no server-level lock; Manager.Load
//     acquires the writer flock only for explicit map-owner joins and
//     PinByName maps.
//   - flockWritesPerLifecycle: Attach + Detach + Unload --
//     serialised on the cross-process writer flock.
//   - readsPerLifecycle: Get + post-Attach Get (link membership) +
//     post-Detach Get (link absence) + post-Unload Get (negative)
//     -- lockless on both fronts.
const (
	loadsPerLifecycle       = 1
	flockWritesPerLifecycle = 3
	readsPerLifecycle       = 4
	rpcsPerLifecycle        = loadsPerLifecycle + flockWritesPerLifecycle + readsPerLifecycle
)

// typeSpec captures the per-program-type knobs the shared
// lifecycle helper needs: where the bytecode lives, the program
// name inside it, the enum value, optional load-time
// ProgSpecificInfo (FentryLoadInfo / FexitLoadInfo / ...), and a
// per-goroutine attach builder.
//
// attachBuilder runs once per goroutine to produce a per-iteration
// AttachInfo closure. Any state the goroutine needs across its
// iterations (e.g. the host-side netif name for the XDP/TC/TCX
// types, which create their own veth at goroutine setup time) is
// captured in the returned closure. That keeps per-goroutine
// state out of typeSpec's signature and away from
// interface{}-typed plumbing.
type typeSpec struct {
	name          string
	object        string // basename under testdataDir
	progName      string
	enumType      pb.BpfmanProgramType
	loadInfo      *pb.ProgSpecificInfo
	attachBuilder func(t *testing.T, gid int) func() *pb.AttachInfo
}

// TestParallel_GRPC runs each program type's lifecycle as a
// separate sub-test. Sub-tests call t.Parallel(), so Go's test
// framework drives them concurrently against the single daemon;
// each sub-test also fans goroutines internally for within-type
// parallelism. The daemon therefore observes
// load/attach/detach/unload of multiple program types
// interleaved, which is the surface that matters for the
// in-process serialisation removal.
func TestParallel_GRPC(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root (bpfman load)")
	}
	if _, err := os.Stat(kmodTargetsRoot); err != nil {
		t.Skipf("bpfman_e2e_targets kmod not available at %s: %v (load the module first)", kmodTargetsRoot, err)
	}

	specs := []typeSpec{
		kprobeSpec(),
		tracepointSpec(),
		fentrySpec(),
		fexitSpec(),
		uprobeSpec(),
		xdpSpec(),
		tcSpec(),
		tcxSpec(),
	}

	// counts is populated entirely here, before any sub-test
	// goroutine starts, then read only inside t.Cleanup after
	// every sub-test has finished. No goroutine ever writes a
	// new key, so a plain map (rather than sync.Map) suffices.
	// The values are *atomic.Int64 because the per-sub-test
	// goroutine pool bumps the same counter concurrently. The
	// whole table lives for one TestParallel_GRPC invocation,
	// so -test.count > 1 sees fresh counts each iteration.
	counts := make(map[string]*atomic.Int64, len(specs))
	for _, spec := range specs {
		counts[spec.name] = new(atomic.Int64)
	}

	// t.Cleanup on the parent runs after every t.Parallel()
	// sub-test has finished, so this is the natural place to
	// summarise the work the daemon has actually serviced. The
	// numbers exclude any lifecycle that failed mid-flight; the
	// per-failure t.Error lines already make that visible.
	n := envInt("BPFMAN_GRPC_GOROUTINES", defaultGoroutines)
	iters := envInt("BPFMAN_GRPC_ITERATIONS", defaultIterations)
	started := time.Now()
	t.Cleanup(func() {
		elapsed := time.Since(started)
		var totalLifecycles int64
		t.Logf("gRPC parallel summary (%d goroutines/type x %d iterations/goroutine; one lifecycle = Load->Attach->Detach->Unload round-trip, ~%d RPCs):", n, iters, rpcsPerLifecycle)
		for _, spec := range specs {
			count := counts[spec.name].Load()
			totalLifecycles += count
			t.Logf("  %-10s %4d lifecycles  ~%6d rpcs", spec.name, count, count*int64(rpcsPerLifecycle))
		}
		totalRPCs := totalLifecycles * int64(rpcsPerLifecycle)
		totalFlockWrites := totalLifecycles * int64(flockWritesPerLifecycle)
		totalLoads := totalLifecycles * int64(loadsPerLifecycle)
		totalReads := totalLifecycles * int64(readsPerLifecycle)
		rate, flockRate, loadRate, readRate := 0.0, 0.0, 0.0, 0.0
		if elapsed > 0 {
			secs := elapsed.Seconds()
			rate = float64(totalRPCs) / secs
			flockRate = float64(totalFlockWrites) / secs
			loadRate = float64(totalLoads) / secs
			readRate = float64(totalReads) / secs
		}
		t.Logf("  %-10s %4d lifecycles  ~%6d rpcs  (%s wall, ~%.1f rpcs/s)", "total", totalLifecycles, totalRPCs, elapsed.Round(time.Millisecond), rate)
		t.Logf("    flock writes (Attach/Detach/Unload):  %6d ops  ~%6.1f/s", totalFlockWrites, flockRate)
		t.Logf("    Loads (manager-gated):                %6d ops  ~%6.1f/s", totalLoads, loadRate)
		t.Logf("    reads (Get):                          %6d ops  ~%6.1f/s", totalReads, readRate)
	})

	for _, spec := range specs {
		counter := counts[spec.name]
		t.Run(spec.name, func(t *testing.T) {
			t.Parallel()
			runParallelLifecycles(t, spec, counter)
		})
	}
}

func TestGRPC_MultiProgramLoadDoesNotFabricateMapOwnership(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root (bpfman load)")
	}

	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()

	loadResp, err := client.Load(ctx, &pb.LoadRequest{
		Bytecode: &pb.BytecodeLocation{
			Location: &pb.BytecodeLocation_File{File: testdataPath("multi_prog_kprobe_counter.bpf.o")},
		},
		Info: []*pb.LoadInfo{
			{Name: "mkp_a", ProgramType: pb.BpfmanProgramType_KPROBE},
			{Name: "mkp_b", ProgramType: pb.BpfmanProgramType_KPROBE},
		},
		Metadata: map[string]string{"test": "grpc_no_fabricated_map_owner"},
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if got := len(loadResp.Programs); got != 2 {
		t.Fatalf("Load: want 2 programs, got %d", got)
	}

	ids := make([]uint32, 0, len(loadResp.Programs))
	for i, prog := range loadResp.Programs {
		if prog.KernelInfo == nil {
			t.Fatalf("Load program %d: missing KernelInfo", i)
		}
		if prog.Info.GetMapOwnerId() != 0 || prog.Info.MapOwnerId != nil {
			t.Fatalf("Load program %d: fabricated map owner %d", i, prog.Info.GetMapOwnerId())
		}
		ids = append(ids, prog.KernelInfo.Id)
	}

	for _, id := range ids {
		getResp, err := client.Get(ctx, &pb.GetRequest{Id: id})
		if err != nil {
			t.Fatalf("Get %d: %v", id, err)
		}

		if getResp.Info.GetMapOwnerId() != 0 || getResp.Info.MapOwnerId != nil {
			t.Fatalf("Get program %d: persisted fabricated map owner %d", id, getResp.Info.GetMapOwnerId())
		}
	}

	for _, id := range ids {
		if _, err := client.Unload(ctx, &pb.UnloadRequest{Id: id}); err != nil {
			t.Fatalf("Unload %d in load order: %v", id, err)
		}
	}
}

// Defaults for the per-sub-test goroutine fan-out and per-goroutine
// iteration count. Picked to give the daemon a meaningful concurrent
// workload while keeping the matrix wall time tolerable on the
// developer loop. Override via BPFMAN_GRPC_GOROUTINES and
// BPFMAN_GRPC_ITERATIONS.
const (
	defaultGoroutines       = 32
	defaultIterations       = 4
	defaultProgressInterval = time.Second
)

const (
	kmodTargetsRoot       = "/sys/kernel/debug/bpfman_e2e"
	grpcKmodTargetFunc    = "bpfman_e2e_target_0"
	grpcKmodTracepoint    = "bpfman_e2e/bpfman_e2e_ping"
	grpcKmodTracepointObj = "tracepoint_kmod_counter.bpf.o"
)

// runParallelLifecycles fans BPFMAN_GRPC_GOROUTINES goroutines,
// each running BPFMAN_GRPC_ITERATIONS independent lifecycles
// of the given type. Failures inside a goroutine stop that
// goroutine and surface as t.Error after wg.Wait, so siblings
// keep running and we see the full failure set rather than just
// the first one. counter accumulates successful lifecycles for
// the cleanup summary; runOneLifecycle itself is counter-agnostic.
func runParallelLifecycles(t *testing.T, spec typeSpec, counter *atomic.Int64) {
	// Crank either knob via env for stress runs; lifecycles
	// serialise on the daemon's writer flock for mutating RPCs,
	// so total wall time scales linearly with
	// N x ITERS x sub-tests.
	n := envInt("BPFMAN_GRPC_GOROUTINES", defaultGoroutines)
	iters := envInt("BPFMAN_GRPC_ITERATIONS", defaultIterations)
	total := int64(n * iters)
	progressEvery := envDuration("BPFMAN_GRPC_PROGRESS_INTERVAL", defaultProgressInterval)

	t.Logf("starting %s gRPC lifecycles: %d goroutines x %d iterations = %d lifecycles (~%d RPCs)", spec.name, n, iters, total, total*int64(rpcsPerLifecycle))

	var wg sync.WaitGroup
	errCh := make(chan error, n*iters)
	progressDone := make(chan struct{})
	progressStopped := make(chan struct{})

	go func() {
		defer close(progressStopped)
		ticker := time.NewTicker(progressEvery)
		defer ticker.Stop()

		started := time.Now()
		for {
			select {
			case <-ticker.C:
				done := counter.Load()
				elapsed := time.Since(started)
				rate := 0.0
				if elapsed > 0 {
					rate = float64(done) / elapsed.Seconds()
				}
				t.Logf("progress %s: %d/%d lifecycles complete (~%d/%d RPCs, %s elapsed, %.1f lifecycles/s)", spec.name, done, total, done*int64(rpcsPerLifecycle), total*int64(rpcsPerLifecycle), elapsed.Round(time.Second), rate)
			case <-progressDone:
				return
			}
		}
	}()

	for g := range n {
		wg.Add(1)
		go func(gid int) {
			defer wg.Done()
			buildAttach := spec.attachBuilder(t, gid)
			for i := range iters {
				if err := runOneLifecycle(t, spec, buildAttach, gid, i); err != nil {
					errCh <- fmt.Errorf("%s g%d iter%d: %w", spec.name, gid, i, err)
					return
				}
				counter.Add(1)
			}
		}(g)
	}

	wg.Wait()
	close(progressDone)
	<-progressStopped
	close(errCh)

	t.Logf("finished %s gRPC lifecycles: %d/%d complete", spec.name, counter.Load(), total)
	for err := range errCh {
		t.Error(err)
	}
}

func envDuration(name string, def time.Duration) time.Duration {
	s := os.Getenv(name)
	if s == "" {
		return def
	}
	d, err := time.ParseDuration(s)
	if err == nil && d > 0 {
		return d
	}

	n, err := strconv.Atoi(s)
	if err == nil && n > 0 {
		return time.Duration(n) * time.Second
	}
	return def
}

// runOneLifecycle drives one Load -> Get -> Attach -> Get ->
// Detach -> Get -> Unload cycle for the given type, with
// round-trip and post-condition assertions at each step. Link
// state is verified through Get's info.links (the wire protocol
// has no per-link inspection RPC): the id Attach returns must
// appear in the program's links while attached and vanish after
// Detach. No counter-delta or shape assertions: those live in the
// .bpfman scripts under e2e/scripts/. We only verify that the
// daemon's gRPC surface behaves correctly under concurrency.
func runOneLifecycle(t *testing.T, spec typeSpec, buildAttach func() *pb.AttachInfo, gid, iter int) error {
	// 180s per-iteration safety net. With 5 sub-tests fanning in
	// parallel, a goroutine can wait up to (N x sub-tests) flock
	// acquisitions x ~50ms ~ tens of seconds in the worst case
	// before its Attach completes. Under RACE=1 STRESS_COUNT=5 the
	// race detector roughly doubles per-transaction wall time, so
	// 180s is generous headroom under RACE=1 and still bounded if
	// something wedges.
	ctx, cancel := context.WithTimeout(t.Context(), 180*time.Second)
	defer cancel()

	loadInfo := &pb.LoadInfo{
		Name:        spec.progName,
		ProgramType: spec.enumType,
		Info:        spec.loadInfo,
	}
	loadResp, err := client.Load(ctx, &pb.LoadRequest{
		Bytecode: &pb.BytecodeLocation{
			Location: &pb.BytecodeLocation_File{File: testdataPath(spec.object)},
		},
		Info: []*pb.LoadInfo{loadInfo},
		Metadata: map[string]string{
			"test":      "grpc_parallel",
			"spec":      spec.name,
			"goroutine": strconv.Itoa(gid),
			"iter":      strconv.Itoa(iter),
		},
	})
	if err != nil {
		return fmt.Errorf("Load: %w", err)
	}

	if len(loadResp.Programs) != 1 {
		return fmt.Errorf("Load: want 1 program, got %d", len(loadResp.Programs))
	}
	if loadResp.Programs[0].KernelInfo == nil {
		return fmt.Errorf("Load: missing KernelInfo")
	}
	progID := loadResp.Programs[0].KernelInfo.Id

	getResp, err := client.Get(ctx, &pb.GetRequest{Id: progID})
	if err != nil {
		return fmt.Errorf("Get %d: %w", progID, err)
	}

	if getResp.KernelInfo == nil || getResp.KernelInfo.Id != progID {
		return fmt.Errorf("Get %d: id mismatch", progID)
	}
	if got := getResp.Info.Metadata["goroutine"]; got != strconv.Itoa(gid) {
		return fmt.Errorf("Get %d: metadata.goroutine %q != %d", progID, got, gid)
	}

	attachResp, err := client.Attach(ctx, &pb.AttachRequest{
		Id:     progID,
		Attach: buildAttach(),
	})
	if err != nil {
		return fmt.Errorf("Attach %d: %w", progID, err)
	}

	linkID := attachResp.LinkId
	if linkID == 0 {
		return fmt.Errorf("Attach %d: returned zero bpfman link id", progID)
	}

	// Get's info.links carries the program's managed link ids --
	// the same id space Attach returns -- so membership proves the
	// attach registered.
	attachedResp, err := client.Get(ctx, &pb.GetRequest{Id: progID})
	if err != nil {
		return fmt.Errorf("post-Attach Get %d: %w", progID, err)
	}
	if attachedResp.Info == nil {
		return fmt.Errorf("post-Attach Get %d: missing Info", progID)
	}
	if !slices.Contains(attachedResp.Info.Links, linkID) {
		return fmt.Errorf("post-Attach Get %d: link %d missing from links %v", progID, linkID, attachedResp.Info.Links)
	}

	if _, err := client.Detach(ctx, &pb.DetachRequest{LinkId: linkID}); err != nil {
		return fmt.Errorf("Detach %d: %w", linkID, err)
	}

	detachedResp, err := client.Get(ctx, &pb.GetRequest{Id: progID})
	if err != nil {
		return fmt.Errorf("post-Detach Get %d: %w", progID, err)
	}
	if detachedResp.Info != nil && slices.Contains(detachedResp.Info.Links, linkID) {
		return fmt.Errorf("post-Detach: link %d still listed for program %d", linkID, progID)
	}

	if _, err := client.Unload(ctx, &pb.UnloadRequest{Id: progID}); err != nil {
		return fmt.Errorf("Unload %d: %w", progID, err)
	}

	if _, err := client.Get(ctx, &pb.GetRequest{Id: progID}); err == nil {
		return fmt.Errorf("post-Unload: Get %d still succeeds", progID)
	}

	return nil
}

// ---------------------------------------------------------------
// Per-type specs.
// ---------------------------------------------------------------

// constantAttachBuilder lifts a per-iteration AttachInfo into
// the typeSpec.attachBuilder signature. Use for types whose
// AttachInfo never varies across goroutines or iterations
// (kprobe / tracepoint / fentry / fexit) -- the per-goroutine
// setup is a no-op and the same value is returned for every
// iteration.
func constantAttachBuilder(info *pb.AttachInfo) func(*testing.T, int) func() *pb.AttachInfo {
	return func(_ *testing.T, _ int) func() *pb.AttachInfo {
		return func() *pb.AttachInfo { return info }
	}
}

func kprobeSpec() typeSpec {
	return typeSpec{
		name:     "kprobe",
		object:   "multi_prog_kprobe_kmod_counter.bpf.o",
		progName: "mkp_a",
		enumType: pb.BpfmanProgramType_KPROBE,
		attachBuilder: constantAttachBuilder(&pb.AttachInfo{
			Info: &pb.AttachInfo_KprobeAttachInfo{
				KprobeAttachInfo: &pb.KprobeAttachInfo{FnName: grpcKmodTargetFunc},
			},
		}),
	}
}

func tracepointSpec() typeSpec {
	return typeSpec{
		name:     "tracepoint",
		object:   grpcKmodTracepointObj,
		progName: "tracepoint_kmod_recorder",
		enumType: pb.BpfmanProgramType_TRACEPOINT,
		attachBuilder: constantAttachBuilder(&pb.AttachInfo{
			Info: &pb.AttachInfo_TracepointAttachInfo{
				TracepointAttachInfo: &pb.TracepointAttachInfo{
					Tracepoint: grpcKmodTracepoint,
				},
			},
		}),
	}
}

func fentrySpec() typeSpec {
	return typeSpec{
		name:     "fentry",
		object:   "fentry_kmod_exact.bpf.o",
		progName: "test_fentry",
		enumType: pb.BpfmanProgramType_FENTRY,
		loadInfo: &pb.ProgSpecificInfo{
			Info: &pb.ProgSpecificInfo_FentryLoadInfo{
				FentryLoadInfo: &pb.FentryLoadInfo{FnName: grpcKmodTargetFunc},
			},
		},
		attachBuilder: constantAttachBuilder(&pb.AttachInfo{
			Info: &pb.AttachInfo_FentryAttachInfo{
				FentryAttachInfo: &pb.FentryAttachInfo{},
			},
		}),
	}
}

func fexitSpec() typeSpec {
	return typeSpec{
		name:     "fexit",
		object:   "fexit_kmod_exact.bpf.o",
		progName: "test_fexit",
		enumType: pb.BpfmanProgramType_FEXIT,
		loadInfo: &pb.ProgSpecificInfo{
			Info: &pb.ProgSpecificInfo_FexitLoadInfo{
				FexitLoadInfo: &pb.FexitLoadInfo{FnName: grpcKmodTargetFunc},
			},
		},
		attachBuilder: constantAttachBuilder(&pb.AttachInfo{
			Info: &pb.AttachInfo_FexitAttachInfo{
				FexitAttachInfo: &pb.FexitAttachInfo{},
			},
		}),
	}
}

// keepUprobeTargetLive pins uprobetarget.Invoke into the
// binary's reachable-symbol set so the Go linker does not
// dead-code-eliminate the cgo wrapper and, with it, the C
// symbol the uprobe sub-test attaches to. The test never calls
// Invoke at runtime -- it only attaches a uprobe to the symbol's
// ELF address.
var keepUprobeTargetLive = uprobetarget.Invoke

func uprobeSpec() typeSpec {
	fnName := uprobetarget.Symbol
	// The uprobe attaches to a cgo'd C symbol in *this* test
	// binary, not in the daemon. /proc/self/exe is daemon-relative
	// once the request reaches bpfman, so resolve the test
	// binary's absolute path here and pass that to the daemon.
	target, err := os.Executable()
	if err != nil {
		// Fall back to /proc/self/exe; if the daemon reads it,
		// it will look in bin/bpfman and fail with a clear
		// "symbol not found" message. Better than panicking
		// during package init.
		target = "/proc/self/exe"
	}
	return typeSpec{
		name:     "uprobe",
		object:   "uprobe_counter.bpf.o",
		progName: "uprobe_counter",
		enumType: pb.BpfmanProgramType_UPROBE,
		attachBuilder: constantAttachBuilder(&pb.AttachInfo{
			Info: &pb.AttachInfo_UprobeAttachInfo{
				UprobeAttachInfo: &pb.UprobeAttachInfo{
					Target: target,
					FnName: &fnName,
				},
			},
		}),
	}
}

// Network types: each spec's attachBuilder allocates its own
// veth pair via testnet.NewTestVethPair and captures the
// host-side interface name in the returned closure. The veth
// thus has goroutine lifetime, which keeps the kernel netif
// state stable across the goroutine's iterations and avoids
// contention on the XDP/TC dispatcher slot limit
// (MaxPrograms = 10) when many goroutines attach concurrently
// to the same interface. TCX doesn't have a dispatcher slot
// limit, but reuses the same shape for consistency.
// WithoutConnectivityWarmup is required at every callsite: the
// gRPC lifecycle test never puts a packet on the wire, and the
// default warmup ping does not scale to the goroutine counts
// the test targets.

func xdpSpec() typeSpec {
	return typeSpec{
		name:     "xdp",
		object:   "xdp_pass.bpf.o",
		progName: "pass",
		enumType: pb.BpfmanProgramType_XDP,
		attachBuilder: func(t *testing.T, _ int) func() *pb.AttachInfo {
			iface := testnet.NewTestVethPair(t, testnet.WithoutConnectivityWarmup()).A.Name
			return func() *pb.AttachInfo {
				return &pb.AttachInfo{Info: &pb.AttachInfo_XdpAttachInfo{
					XdpAttachInfo: &pb.XDPAttachInfo{
						Iface:    iface,
						Priority: 100,
					},
				}}
			}
		},
	}
}

func tcSpec() typeSpec {
	return typeSpec{
		name:     "tc",
		object:   "tc_counter.bpf.o",
		progName: "stats",
		enumType: pb.BpfmanProgramType_TC,
		attachBuilder: func(t *testing.T, _ int) func() *pb.AttachInfo {
			iface := testnet.NewTestVethPair(t, testnet.WithoutConnectivityWarmup()).A.Name
			return func() *pb.AttachInfo {
				return &pb.AttachInfo{Info: &pb.AttachInfo_TcAttachInfo{
					TcAttachInfo: &pb.TCAttachInfo{
						Iface:     iface,
						Direction: "ingress",
						Priority:  100,
					},
				}}
			}
		},
	}
}

func tcxSpec() typeSpec {
	return typeSpec{
		name:     "tcx",
		object:   "tcx_counter.bpf.o",
		progName: "tcx_stats",
		enumType: pb.BpfmanProgramType_TCX,
		attachBuilder: func(t *testing.T, _ int) func() *pb.AttachInfo {
			iface := testnet.NewTestVethPair(t, testnet.WithoutConnectivityWarmup()).A.Name
			return func() *pb.AttachInfo {
				return &pb.AttachInfo{Info: &pb.AttachInfo_TcxAttachInfo{
					TcxAttachInfo: &pb.TCXAttachInfo{
						Iface:     iface,
						Direction: "ingress",
						Priority:  100,
					},
				}}
			}
		},
	}
}
