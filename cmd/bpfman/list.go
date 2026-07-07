package main

import (
	"context"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/labels"

	"github.com/bpfman/bpfman"
	"github.com/bpfman/bpfman/cmd/bpfman/cliformat"
	"github.com/bpfman/bpfman/cmd/internal/args"
	"github.com/bpfman/bpfman/cmd/internal/runtime"
	"github.com/bpfman/bpfman/kernel"
	"github.com/bpfman/bpfman/manager"
)

// ListProgramsCmd lists BPF programs; --all also includes unmanaged kernel programs.
type ListProgramsCmd struct {
	cliformat.OutputFlags

	// Quiet suppresses the table and prints only program IDs, one per line.
	Quiet bool `short:"q" help:"Output only program IDs, one per line."`

	// Attached restricts the listing to programs that have at least one
	// active kernel link. Mutually exclusive with Unattached.
	Attached bool `name:"attached" help:"Show only programs with active links."`

	// Unattached restricts the listing to programs that have no active
	// kernel link. Mutually exclusive with Attached.
	Unattached bool `name:"unattached" help:"Show only programs without active links."`

	// Type filters by one or more program types (case-insensitive,
	// comma-separated or repeated, e.g. --type=xdp,kprobe).
	Type []bpfman.ProgramType `name:"type" sep:"," help:"Filter by program type (case-insensitive, e.g., --type=xdp,kprobe)."`

	// ProgramType is an alias for --type; values from both flags are
	// unioned into the type filter.
	ProgramType []bpfman.ProgramType `name:"program-type" short:"p" sep:"," help:"Alias for --type."`

	// Application filters to programs whose bpfman.io/application metadata
	// equals this value.
	Application string `name:"application" help:"Filter by application metadata."`

	// MetadataSelector filters by KEY=VALUE program metadata; repeat the
	// flag to require several pairs.
	MetadataSelector []args.KeyValue `name:"metadata-selector" short:"m" help:"Filter by KEY=VALUE metadata (can be repeated)."`

	// All includes unmanaged kernel programs (those loaded outside bpfman)
	// in the listing; without it only bpfman-managed programs are shown.
	All bool `name:"all" short:"a" help:"Include unmanaged kernel programs (those loaded outside bpfman)."`

	// Selector filters by a Kubernetes-style label selector over program
	// metadata (e.g. app=myapp,version!=v1).
	Selector string `name:"selector" short:"l" help:"Label selector (e.g., app=myapp,version!=v1)."`
}

// Validate rejects mutually exclusive flag combinations before the
// command runs; currently it fails when both --attached and --unattached
// are given.
func (c *ListProgramsCmd) Validate() error {
	if c.Attached && c.Unattached {
		return fmt.Errorf("--attached and --unattached are mutually exclusive")
	}
	return nil
}

// applicationMetadata builds the metadata-selector map from the
// --metadata-selector pairs and folds --application in under the
// application metadata key when it is set. It backs the program-scope
// filtering on both the program and link list commands.
func applicationMetadata(selector []args.KeyValue, application string) map[string]string {
	metadata := args.MetadataMap(selector)
	if application != "" {
		if metadata == nil {
			metadata = map[string]string{}
		}
		metadata[manager.ApplicationMetadataKey] = application
	}
	return metadata
}

func (c *ListProgramsCmd) buildListOptions() ([]bpfman.ListOption, error) {
	var opts []bpfman.ListOption

	// Attachment state
	if c.Attached {
		opts = append(opts, bpfman.WithAttached())
	} else if c.Unattached {
		opts = append(opts, bpfman.WithUnattached())
	}

	if len(c.Type) > 0 {
		opts = append(opts, bpfman.WithTypes(c.Type...))
	}
	if len(c.ProgramType) > 0 {
		opts = append(opts, bpfman.WithTypes(c.ProgramType...))
	}

	var selectors []labels.Selector
	metadata := applicationMetadata(c.MetadataSelector, c.Application)
	if len(metadata) > 0 {
		selectors = append(selectors, labels.SelectorFromSet(labels.Set(metadata)))
	}
	if s := strings.TrimSpace(c.Selector); s != "" {
		sel, err := labels.Parse(s)
		if err != nil {
			return nil, fmt.Errorf("invalid label selector: %w", err)
		}

		selectors = append(selectors, sel)
	}
	if len(selectors) > 0 {
		opts = append(opts, bpfman.MatchingSelector(combineSelectors(selectors...)))
	}

	if c.All {
		opts = append(opts, bpfman.WithIncludeUnmanaged())
	}

	return opts, nil
}

func combineSelectors(selectors ...labels.Selector) labels.Selector {
	combined := labels.NewSelector()
	for _, sel := range selectors {
		requirements, selectable := sel.Requirements()
		if !selectable {
			return labels.Nothing()
		}

		combined = combined.Add(requirements...)
	}
	return combined
}

// Run lists programs matching the configured filters and renders them to
// the output. With --quiet it prints only the program IDs, one per
// line; otherwise it renders the program table or structured output in
// the selected format.
func (c *ListProgramsCmd) Run(cli *runtime.CLI, ctx context.Context) error {
	format, err := c.OutputFlags.Format()
	if err != nil {
		return err
	}

	mgr, cleanup, err := newManager(ctx, cli)
	if err != nil {
		return fmt.Errorf("create manager: %w", err)
	}
	defer cleanup()

	opts, err := c.buildListOptions()
	if err != nil {
		return err
	}

	result, err := mgr.ListProgramEntries(ctx, opts...)
	if err != nil {
		return err
	}

	if len(result.Programs) == 0 && !format.IsStructured() {
		return nil
	}

	if c.Quiet {
		var b strings.Builder
		for _, p := range result.Programs {
			fmt.Fprintf(&b, "%d\n", p.ProgramID)
		}
		return cli.PrintOut(b.String())
	}

	return cliformat.RenderProgramList(cli.Out, result, format)
}

// ListLinksCmd lists managed links.
type ListLinksCmd struct {
	cliformat.OutputFlags

	// Quiet suppresses the table and prints only link IDs, one per line.
	Quiet bool `short:"q" help:"Output only link IDs, one per line."`

	// ProgramID restricts the listing to links owned by this program ID;
	// nil lists links for all programs.
	ProgramID *kernel.ProgramID `name:"program-id" help:"Filter by program ID."`

	// Kind filters by one or more link kinds (comma-separated or repeated,
	// e.g. --kind=xdp,kprobe).
	Kind []bpfman.LinkKind `name:"kind" sep:"," help:"Filter by link kind (e.g., --kind=xdp,kprobe)."`

	// ProgramType is a program-scoped filter: it lists links whose owning
	// program is of one of the given types.
	ProgramType []bpfman.ProgramType `name:"program-type" short:"p" sep:"," help:"Filter by the owning program's type (e.g. xdp,kprobe)."`

	// Application is a program-scoped filter: it lists links whose owning
	// program's bpfman.io/application metadata equals this value.
	Application string `name:"application" help:"Filter by the owning program's application metadata."`

	// MetadataSelector is a program-scoped filter on KEY=VALUE metadata of
	// the owning program; repeat the flag to require several pairs.
	MetadataSelector []args.KeyValue `name:"metadata-selector" short:"m" help:"Filter by the owning program's KEY=VALUE metadata (can be repeated)."`
}

func (c *ListLinksCmd) buildLinkListOptions() ([]bpfman.LinkListOption, error) {
	var opts []bpfman.LinkListOption

	if c.ProgramID != nil {
		opts = append(opts, bpfman.WithProgramID(*c.ProgramID))
	}

	if len(c.Kind) > 0 {
		opts = append(opts, bpfman.WithKinds(c.Kind...))
	}

	return opts, nil
}

// programScopeOptions builds the program-list options for
// program-scoped link filtering: --program-type, --application and
// --metadata-selector select the owning program. The bool reports whether
// any program-scope filter was supplied; when false, all links are listed
// (subject only to the link-level filters).
func (c *ListLinksCmd) programScopeOptions() ([]bpfman.ListOption, bool) {
	var opts []bpfman.ListOption
	scoped := false

	if len(c.ProgramType) > 0 {
		opts = append(opts, bpfman.WithTypes(c.ProgramType...))
		scoped = true
	}

	metadata := applicationMetadata(c.MetadataSelector, c.Application)
	if len(metadata) > 0 {
		opts = append(opts, bpfman.MatchingSelector(labels.SelectorFromSet(labels.Set(metadata))))
		scoped = true
	}

	return opts, scoped
}

// Run lists links matching the configured filters and renders them to
// the output. When any program-scope filter is set the links are scoped
// to the matching programs. With --quiet it prints only the link IDs,
// one per line; otherwise it renders the link table or structured output
// in the selected format.
func (c *ListLinksCmd) Run(cli *runtime.CLI, ctx context.Context) error {
	format, err := c.OutputFlags.Format()
	if err != nil {
		return err
	}

	mgr, cleanup, err := newManager(ctx, cli)
	if err != nil {
		return fmt.Errorf("create manager: %w", err)
	}
	defer cleanup()

	opts, err := c.buildLinkListOptions()
	if err != nil {
		return err
	}

	var links []bpfman.LinkRecord
	if progOpts, scoped := c.programScopeOptions(); scoped {
		links, err = mgr.ListLinksScopedToPrograms(ctx, progOpts, opts)
	} else {
		links, err = mgr.ListLinks(ctx, opts...)
	}
	if err != nil {
		return err
	}

	if len(links) == 0 && !format.IsStructured() {
		return nil
	}

	if c.Quiet {
		var b strings.Builder
		for _, l := range links {
			fmt.Fprintf(&b, "%d\n", l.ID)
		}
		return cli.PrintOut(b.String())
	}

	view := cliformat.LinkListView{Links: links}

	// The text table names each link's owning program (application and
	// function name); resolve those in one pass over the program entries.
	// Structured output carries the link records alone and needs no join.
	if !format.IsStructured() {
		entries, err := mgr.ListProgramEntries(ctx)
		if err != nil {
			return err
		}

		refs := make(map[kernel.ProgramID]cliformat.LinkProgramRef, len(entries.Programs))
		for _, e := range entries.Programs {
			refs[e.ProgramID] = cliformat.LinkProgramRef{Application: e.Application, FunctionName: e.FunctionName}
		}
		view.Programs = refs
	}

	return cliformat.RenderLinkList(cli.Out, view, format)
}
