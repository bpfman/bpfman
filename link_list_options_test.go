package bpfman_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/bpfman/bpfman"
)

func TestLinkListOptions_WithKinds(t *testing.T) {
	t.Parallel()

	opts := bpfman.ApplyLinkListOptions(bpfman.WithKinds(bpfman.LinkKindXDP, bpfman.LinkKindTC))

	xdpLink := &bpfman.LinkRecord{Kind: bpfman.LinkKindXDP}
	tcLink := &bpfman.LinkRecord{Kind: bpfman.LinkKindTC}
	kprobeLink := &bpfman.LinkRecord{Kind: bpfman.LinkKindKprobe}

	assert.True(t, opts.Matches(xdpLink), "should match XDP")
	assert.True(t, opts.Matches(tcLink), "should match TC")
	assert.False(t, opts.Matches(kprobeLink), "should not match kprobe")
}

func TestLinkListOptions_WithProgramID(t *testing.T) {
	t.Parallel()

	opts := bpfman.ApplyLinkListOptions(bpfman.WithProgramID(123))

	matchingLink := &bpfman.LinkRecord{ProgramID: 123}
	nonMatchingLink := &bpfman.LinkRecord{ProgramID: 456}

	assert.True(t, opts.Matches(matchingLink), "should match program 123")
	assert.False(t, opts.Matches(nonMatchingLink), "should not match program 456")
}

func TestLinkListOptions_Combined(t *testing.T) {
	t.Parallel()

	opts := bpfman.ApplyLinkListOptions(
		bpfman.WithKinds(bpfman.LinkKindXDP),
		bpfman.WithProgramID(123),
	)

	matchingLink := &bpfman.LinkRecord{Kind: bpfman.LinkKindXDP, ProgramID: 123}
	wrongKind := &bpfman.LinkRecord{Kind: bpfman.LinkKindTC, ProgramID: 123}
	wrongProgram := &bpfman.LinkRecord{Kind: bpfman.LinkKindXDP, ProgramID: 456}

	assert.True(t, opts.Matches(matchingLink), "should match XDP + program 123")
	assert.False(t, opts.Matches(wrongKind), "should not match TC")
	assert.False(t, opts.Matches(wrongProgram), "should not match wrong program")
}

func TestLinkListOptions_EmptyMatchesAll(t *testing.T) {
	t.Parallel()

	opts := bpfman.ApplyLinkListOptions()

	anyLink := &bpfman.LinkRecord{Kind: bpfman.LinkKindKprobe, ProgramID: 999}
	assert.True(t, opts.Matches(anyLink), "empty options should match all")
}

func TestLinkListOptions_WithKinds_Empty(t *testing.T) {
	t.Parallel()

	// Empty kinds should match all
	opts := bpfman.ApplyLinkListOptions(bpfman.WithKinds())

	link := &bpfman.LinkRecord{Kind: bpfman.LinkKindXDP}
	assert.True(t, opts.Matches(link), "empty kinds should match all links")
}

func TestLinkListOptions_MultipleWithKinds(t *testing.T) {
	t.Parallel()

	// Calling WithKinds multiple times should accumulate
	opts := bpfman.ApplyLinkListOptions(
		bpfman.WithKinds(bpfman.LinkKindXDP),
		bpfman.WithKinds(bpfman.LinkKindKprobe),
	)

	xdpLink := &bpfman.LinkRecord{Kind: bpfman.LinkKindXDP}
	kprobeLink := &bpfman.LinkRecord{Kind: bpfman.LinkKindKprobe}
	tcLink := &bpfman.LinkRecord{Kind: bpfman.LinkKindTC}

	assert.True(t, opts.Matches(xdpLink), "should match XDP")
	assert.True(t, opts.Matches(kprobeLink), "should match Kprobe")
	assert.False(t, opts.Matches(tcLink), "should not match TC")
}

func TestLinkKindNames(t *testing.T) {
	t.Parallel()

	names := bpfman.LinkKindNames()

	assert.Contains(t, names, "xdp")
	assert.Contains(t, names, "tc")
	assert.Contains(t, names, "tcx")
	assert.Contains(t, names, "kprobe")
	assert.Contains(t, names, "kretprobe")
	assert.Contains(t, names, "uprobe")
	assert.Contains(t, names, "uretprobe")
	assert.Contains(t, names, "tracepoint")
	assert.Contains(t, names, "fentry")
	assert.Contains(t, names, "fexit")
	assert.Len(t, names, 10)
}

func TestAllLinkKinds(t *testing.T) {
	t.Parallel()

	kinds := bpfman.AllLinkKinds()

	assert.Contains(t, kinds, bpfman.LinkKindXDP)
	assert.Contains(t, kinds, bpfman.LinkKindTC)
	assert.Contains(t, kinds, bpfman.LinkKindTCX)
	assert.Contains(t, kinds, bpfman.LinkKindKprobe)
	assert.Contains(t, kinds, bpfman.LinkKindKretprobe)
	assert.Contains(t, kinds, bpfman.LinkKindUprobe)
	assert.Contains(t, kinds, bpfman.LinkKindUretprobe)
	assert.Contains(t, kinds, bpfman.LinkKindTracepoint)
	assert.Contains(t, kinds, bpfman.LinkKindFentry)
	assert.Contains(t, kinds, bpfman.LinkKindFexit)
	assert.Len(t, kinds, 10)
}
