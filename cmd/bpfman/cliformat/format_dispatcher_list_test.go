package cliformat

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/bpfman/bpfman/dispatcher"
	"github.com/bpfman/bpfman/kernel"
	"github.com/bpfman/bpfman/platform"
)

func sampleDispatcherSummaries() []platform.DispatcherSummary {
	linkID := kernel.LinkID(9)
	return []platform.DispatcherSummary{
		{
			Key:      dispatcher.NewKey(dispatcher.DispatcherTypeXDP, 4026531840, 7),
			Revision: 3,
			Runtime: platform.DispatcherRuntime{
				ProgramID:    101,
				KernelLinkID: &linkID,
			},
			MemberCount: 2,
		},
		{
			Key:      dispatcher.NewKey(dispatcher.DispatcherTypeTCIngress, 4026531999, 12),
			Revision: 1,
			Runtime: platform.DispatcherRuntime{
				ProgramID: 202,
				NetnsPath: "/var/run/netns/blue",
			},
			MemberCount: 1,
		},
	}
}

func TestRenderDispatcherListJSON_WrapsInResult(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	if err := RenderDispatcherList(&buf, sampleDispatcherSummaries(), OutputFormatJSON); err != nil {
		t.Fatalf("RenderDispatcherList() error = %v", err)
	}
	output := buf.String()
	var result platform.DispatcherListResult
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("output is not a DispatcherListResult: %v\n%s", err, output)
	}
	if len(result.Dispatchers) != 2 {
		t.Errorf("expected 2 dispatchers, got %d", len(result.Dispatchers))
	}
}

func TestRenderDispatcherListJSON_EmptyListYieldsEmptyResult(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	if err := RenderDispatcherList(&buf, nil, OutputFormatJSON); err != nil {
		t.Fatalf("RenderDispatcherList() error = %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, `"dispatchers": []`) {
		t.Errorf("empty list should marshal as an empty dispatchers array: %s", output)
	}
}

func TestRenderDispatcherListTable_SingleListingCarriesNetns(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	if err := RenderDispatcherList(&buf, sampleDispatcherSummaries(), OutputFormatText); err != nil {
		t.Fatalf("RenderDispatcherList(table) error = %v", err)
	}
	table := buf.String()
	for _, want := range []string{"NETNS", "/var/run/netns/blue"} {
		if !strings.Contains(table, want) {
			t.Errorf("table missing %q:\n%s", want, table)
		}
	}
}
