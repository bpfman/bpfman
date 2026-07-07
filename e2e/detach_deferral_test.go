//go:build e2e

package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/bpfman/bpfman"
	"github.com/bpfman/bpfman/manager"
)

// TestDebug_DetachDeferral_Kretprobe is a diagnostic, not a regression
// test. It measures how long after env.Detach(b) returns the kernel
// keeps firing the just-detached BPF program. Run with:
//
//	sudo ./bin/e2e.test \
//	    -test.run '^TestDebug_DetachDeferral_Kretprobe$' \
//	    -test.v -test.count=20
//
// For each iteration it logs:
//
//   - tDetach: wall time spent in env.Detach (start to return)
//   - quietPoll: counter reads at +0us, +250us, +500us, ... while
//     firing nothing -- the counter must not move (sanity for the
//     polling itself)
//   - probe-N: fire one kmod slot event at offset T from Detach
//     return; record whether the just-detached program counted it
//
// "did the just-detached program still count" is the only signal that
// matters. If a probe at +T sees a counter delta, the program was
// still hooked at offset T.
func TestDebug_DetachDeferral_Kretprobe(t *testing.T) {
	RequireRoot(t)
	RequireKmodTargets(t)

	slot := acquireKmodSlot(t)
	t.Logf("kmod slot: index=%d func=%s", slot.Index, slot.Func)

	env := NewTestEnv(t)
	ctx := context.Background()
	env.AssertCleanState()

	weights := uniqueWeights(t, 2)
	type plan struct {
		suffix string
		weight uint64
	}
	plans := []plan{
		{suffix: "a", weight: weights[0]},
		{suffix: "b", weight: weights[1]},
	}

	specs := make([]manager.ProgramSpec, len(plans))
	globals := map[string][]byte{}
	for i, p := range plans {
		specs[i] = manager.ProgramSpec{Type: bpfman.ProgramTypeKretprobe, Name: "mkp_" + p.suffix}
		globals["weight_"+p.suffix] = uint64LE(p.weight)
	}

	programs, err := env.LoadFile(ctx, "testdata/bpf/multi_prog_kprobe_kmod_counter.bpf.o", specs, manager.LoadOpts{
		GlobalData: globals,
	})
	require.NoError(t, err)
	require.Len(t, programs, len(plans))
	for _, prog := range programs {
		t.Cleanup(func() { env.Unload(context.Background(), prog.Status.Kernel.ID) })
	}

	mapIDB := mapIDByName(t, programs[1], "mkp_b_count")
	mapIDA := mapIDByName(t, programs[0], "mkp_a_count")

	links := make([]bpfman.LinkRecord, len(plans))
	for i, prog := range programs {
		spec, err := bpfman.NewKprobeAttachSpec(prog.Status.Kernel.ID, slot.Func)
		require.NoError(t, err)
		link, err := env.Attach(ctx, spec)
		require.NoError(t, err, "attach mkp_%s", plans[i].suffix)
		links[i] = link
		t.Cleanup(func() { env.Detach(context.Background(), link.ID) })
	}

	// Sanity: fire one event; both should count.
	bBefore := readArrayCounterByID(t, mapIDB)
	aBefore := readArrayCounterByID(t, mapIDA)
	slot.Fire(t, 1)
	require.Equal(t, bBefore+plans[1].weight, readArrayCounterByID(t, mapIDB), "sanity: b should count pre-detach")
	require.Equal(t, aBefore+plans[0].weight, readArrayCounterByID(t, mapIDA), "sanity: a should count pre-detach")

	// Detach b. Time the call.
	bSnapshot := readArrayCounterByID(t, mapIDB)
	tDetachStart := time.Now()
	require.NoError(t, env.Detach(ctx, links[1].ID))
	tDetachReturn := time.Now()
	t.Logf("env.Detach returned in %s", tDetachReturn.Sub(tDetachStart))

	// Phase 1: idle poll. No workload activity. The counter must not
	// move; if it does, polling itself has a side effect we missed.
	for i := range 40 {
		offset := time.Since(tDetachReturn)
		v := readArrayCounterByID(t, mapIDB)
		if v != bSnapshot {
			t.Fatalf("idle poll #%d at +%s: counter moved without workload (snapshot=%d, now=%d)", i, offset, bSnapshot, v)
		}
		time.Sleep(250 * time.Microsecond)
	}
	t.Logf("idle phase OK: counter stable at %d for ~10ms with no workload", bSnapshot)

	// Phase 2: fire 10 individual kmod slot events, one at a time, recording
	// for each: offset from tDetachReturn, b's delta, a's delta.
	// b's delta should be 0 if cleanly detached. a is the control --
	// it should always count (it's still attached).
	const probes = 10
	type sample struct {
		offset time.Duration
		bDelta uint64
		aDelta uint64
	}
	samples := make([]sample, 0, probes)
	for range probes {
		bPre := readArrayCounterByID(t, mapIDB)
		aPre := readArrayCounterByID(t, mapIDA)
		fireT := time.Since(tDetachReturn)
		slot.Fire(t, 1)
		bPost := readArrayCounterByID(t, mapIDB)
		aPost := readArrayCounterByID(t, mapIDA)
		samples = append(samples, sample{
			offset: fireT,
			bDelta: bPost - bPre,
			aDelta: aPost - aPre,
		})
	}

	stillFiring := 0
	for i, s := range samples {
		marker := "QUIET"
		if s.bDelta != 0 {
			marker = "STILL FIRING"
			stillFiring++
		}
		t.Logf("probe %d at +%s: a +%d (control), b +%d  %s", i, s.offset, s.aDelta, s.bDelta, marker)
	}

	if stillFiring > 0 {
		t.Logf("RESULT phase2: detached program counted %d/%d post-Detach probes; deferral observed", stillFiring, probes)
	} else {
		t.Logf("RESULT phase2: all %d post-Detach probes were quiet; no deferral observed (window < ~%s)", probes, samples[0].offset)
	}

	// Phase 3: replicate the actual failing pattern. Re-attach b,
	// fire one wave so it counts, then immediately Detach and fire a
	// five-event batch. The events happen back-to-back inside the
	// slot trigger loop, so any sub-roundtrip deferral window is exposed.
	spec, err := bpfman.NewKprobeAttachSpec(programs[1].Status.Kernel.ID, slot.Func)
	require.NoError(t, err)
	bLink, err := env.Attach(ctx, spec)
	require.NoError(t, err)

	slot.Fire(t, 5) // warm-up while b is attached
	bSnapshot = readArrayCounterByID(t, mapIDB)
	t.Logf("phase3 pre-detach: b counter = %d", bSnapshot)

	tDetachStart = time.Now()
	require.NoError(t, env.Detach(ctx, bLink.ID))
	tDetachReturn = time.Now()
	t.Logf("phase3 env.Detach returned in %s", tDetachReturn.Sub(tDetachStart))

	tBatchStart := time.Now()
	slot.Fire(t, 5) // the batch -- 5 events close together
	tBatchReturn := time.Now()

	bAfter := readArrayCounterByID(t, mapIDB)
	bDelta := bAfter - bSnapshot
	t.Logf("phase3 batch Fire(5): start +%s, ack +%s after Detach, b delta = %d (expected 0; one event = %d)", tBatchStart.Sub(tDetachReturn), tBatchReturn.Sub(tDetachReturn), bDelta, plans[1].weight)

	if bDelta != 0 {
		t.Logf("RESULT phase3: STILL FIRING at +%s -- deferral observed (delta = %d events of weight %d)", tBatchStart.Sub(tDetachReturn), bDelta/plans[1].weight, plans[1].weight)
	} else {
		t.Logf("RESULT phase3: quiet")
	}
}
