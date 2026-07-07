package builtins

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/driver"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/runtime"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/semantics"
)

// callNet invokes handleNet with the given word-arg sequence and
// returns the (value, error) result. Tests use this for failures
// that surface before any ip(8) invocation; the happy path
// requires root and lives in the e2e corpus.
func callNet(t *testing.T, args ...string) (runtime.Value, error) {
	wargs := make([]runtime.Arg, len(args))
	for i, a := range args {
		wargs[i] = runtime.WordArg{Text: a}
	}
	return handleNet(driver.Ctx{
		Ctx:  t.Context(),
		Cmd:  "net",
		Args: wargs,
	})
}

// pairArg wraps a *runtime.NetPair as a StructuredValueArg the way
// the argument expander would produce when a script writes
// $pair. Tests that need to dispatch handleNet on a $pair pass
// the result here as args[0].
func pairArg(p *runtime.NetPair) runtime.Arg {
	return runtime.StructuredValueArg{
		Name:  "pair",
		Value: runtime.ValueFromNetPair(p),
	}
}

// scalarPairArg constructs a non-NetPair structured arg so tests
// can confirm the kind check on $pair-receiving subcommands fires
// for the wrong shape.
func scalarPairArg() runtime.Arg {
	v := runtime.StringValue("not a pair")
	return runtime.StructuredValueArg{Name: "x", Value: v}
}

func TestHandleNet_NoSubcommand(t *testing.T) {
	t.Parallel()
	_, err := callNet(t)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "subcommand required")
}

func TestHandleNet_UnknownSubcommand(t *testing.T) {
	t.Parallel()
	_, err := callNet(t, "bridge")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unknown subcommand "bridge"`)
	assert.Contains(t, err.Error(), "exec, netns-veth-pair, release, start, veth-pair")
}

func TestParseVethPairFlags_ExplicitMode_BothSpellings(t *testing.T) {
	t.Parallel()
	cases := [][]string{
		{
			"--ns=ns0", "--host-link=h0", "--host-addr=198.51.100.1/32",
			"--peer-link=p0", "--peer-addr=198.51.100.2/32",
		},
		{
			"--ns", "ns0", "--host-link", "h0", "--host-addr", "198.51.100.1/32",
			"--peer-link", "p0", "--peer-addr", "198.51.100.2/32",
		},
	}
	for _, args := range cases {
		wargs := make([]runtime.Arg, len(args))
		for i, a := range args {
			wargs[i] = runtime.WordArg{Text: a}
		}
		f, err := parseVethPairFlags(wargs)
		require.NoErrorf(t, err, "args=%v", args)
		assert.Equal(t, "ns0", f.Ns)
		assert.Equal(t, "h0", f.HostLink)
		assert.Equal(t, "p0", f.PeerLink)
		assert.Equal(t, "198.51.100.1/32", f.HostAddrCIDR)
		assert.Equal(t, "198.51.100.2/32", f.PeerAddrCIDR)
		assert.Equal(t, "198.51.100.1", f.HostAddr)
		assert.Equal(t, "198.51.100.2", f.PeerAddr)
		assert.Falsef(t, f.AutoAddrs, "explicit-address args should not flip AutoAddrs")
		assert.Falsef(t, f.AutoNames, "explicit-name args should not flip AutoNames")
	}
}

func TestParseVethPairFlags_AutoAddrs_NoAddrFlags(t *testing.T) {
	t.Parallel()
	args := []string{"--ns=ns0", "--host-link=h0", "--peer-link=p0"}
	wargs := make([]runtime.Arg, len(args))
	for i, a := range args {
		wargs[i] = runtime.WordArg{Text: a}
	}
	f, err := parseVethPairFlags(wargs)
	require.NoError(t, err)
	assert.True(t, f.AutoAddrs, "no addr flags should select auto-addresses")
	assert.False(t, f.AutoNames, "explicit names should not flip AutoNames")
	assert.Empty(t, f.HostAddrCIDR)
	assert.Empty(t, f.PeerAddrCIDR)
	assert.Empty(t, f.HostAddr)
	assert.Empty(t, f.PeerAddr)
}

func TestParseVethPairFlags_AutoNames_NoIdentityFlags(t *testing.T) {
	t.Parallel()
	args := []string{"--host-addr=198.51.100.1/30", "--peer-addr=198.51.100.2/30"}
	wargs := make([]runtime.Arg, len(args))
	for i, a := range args {
		wargs[i] = runtime.WordArg{Text: a}
	}
	f, err := parseVethPairFlags(wargs)
	require.NoError(t, err)
	assert.True(t, f.AutoNames, "no identity flags should select auto-naming")
	assert.False(t, f.AutoAddrs, "explicit addresses should not flip AutoAddrs")
	assert.Empty(t, f.Ns)
	assert.Empty(t, f.HostLink)
	assert.Empty(t, f.PeerLink)
}

func TestParseVethPairFlags_AutoEverything_NoFlags(t *testing.T) {
	t.Parallel()
	f, err := parseVethPairFlags(nil)
	require.NoError(t, err, "an empty arg list should select auto-naming and auto-addresses")
	assert.True(t, f.AutoNames)
	assert.True(t, f.AutoAddrs)
	assert.Empty(t, f.Ns)
	assert.Empty(t, f.HostLink)
	assert.Empty(t, f.PeerLink)
	assert.Empty(t, f.HostAddrCIDR)
	assert.Empty(t, f.PeerAddrCIDR)
}

func TestParseVethPairFlags_PartialIdentityGroupIsError(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"only_ns", []string{"--ns=ns0"}, "--ns"},
		{"only_host_link", []string{"--host-link=h0"}, "--host-link"},
		{"only_peer_link", []string{"--peer-link=p0"}, "--peer-link"},
		{"ns_and_host_link", []string{"--ns=ns0", "--host-link=h0"}, "--peer-link"},
		{"ns_and_peer_link", []string{"--ns=ns0", "--peer-link=p0"}, "--host-link"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			wargs := make([]runtime.Arg, len(c.args))
			for i, a := range c.args {
				wargs[i] = runtime.WordArg{Text: a}
			}
			_, err := parseVethPairFlags(wargs)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "must be passed together or all omitted")
			assert.Contains(t, err.Error(), c.want)
		})
	}
}

func TestParseVethPairFlags_HostAddrAloneIsError(t *testing.T) {
	t.Parallel()
	args := []string{"--ns=ns0", "--host-link=h0", "--peer-link=p0", "--host-addr=198.51.100.1/30"}
	wargs := make([]runtime.Arg, len(args))
	for i, a := range args {
		wargs[i] = runtime.WordArg{Text: a}
	}
	_, err := parseVethPairFlags(wargs)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--host-addr was given without --peer-addr")
}

func TestParseVethPairFlags_PeerAddrAloneIsError(t *testing.T) {
	t.Parallel()
	args := []string{"--ns=ns0", "--host-link=h0", "--peer-link=p0", "--peer-addr=198.51.100.2/30"}
	wargs := make([]runtime.Arg, len(args))
	for i, a := range args {
		wargs[i] = runtime.WordArg{Text: a}
	}
	_, err := parseVethPairFlags(wargs)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--peer-addr was given without --host-addr")
}

func TestParseVethPairFlags_NoRoutesFlagIsUnknown(t *testing.T) {
	t.Parallel()
	args := []string{
		"--ns=ns0", "--host-link=h0", "--peer-link=p0", "--no-routes",
	}
	wargs := make([]runtime.Arg, len(args))
	for i, a := range args {
		wargs[i] = runtime.WordArg{Text: a}
	}
	_, err := parseVethPairFlags(wargs)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unknown flag "--no-routes"`)
}

func TestParseVethPairFlags_BareAddressRejected(t *testing.T) {
	t.Parallel()
	args := []string{
		"--ns=ns0", "--host-link=h0", "--host-addr=198.51.100.1",
		"--peer-link=p0", "--peer-addr=198.51.100.2/32",
	}
	wargs := make([]runtime.Arg, len(args))
	for i, a := range args {
		wargs[i] = runtime.WordArg{Text: a}
	}
	_, err := parseVethPairFlags(wargs)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--host-addr")
	assert.Contains(t, err.Error(), "CIDR form")
}

func TestParseVethPairFlags_IPv6Rejected(t *testing.T) {
	t.Parallel()
	args := []string{
		"--ns=ns0", "--host-link=h0", "--host-addr=2001:db8::1/128",
		"--peer-link=p0", "--peer-addr=198.51.100.2/32",
	}
	wargs := make([]runtime.Arg, len(args))
	for i, a := range args {
		wargs[i] = runtime.WordArg{Text: a}
	}
	_, err := parseVethPairFlags(wargs)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "IPv4")
}

func TestParseVethPairFlags_UnknownFlag(t *testing.T) {
	t.Parallel()
	args := []string{
		"--ns=ns0", "--host-link=h0", "--host-addr=198.51.100.1/32",
		"--peer-link=p0", "--peer-addr=198.51.100.2/32",
		"--bogus=x",
	}
	wargs := make([]runtime.Arg, len(args))
	for i, a := range args {
		wargs[i] = runtime.WordArg{Text: a}
	}
	_, err := parseVethPairFlags(wargs)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unknown flag "--bogus=x"`)
}

func TestParseVethPairFlags_UnexpectedPositional(t *testing.T) {
	t.Parallel()
	args := []string{
		"--ns=ns0", "stray", "--host-link=h0", "--host-addr=198.51.100.1/32",
		"--peer-link=p0", "--peer-addr=198.51.100.2/32",
	}
	wargs := make([]runtime.Arg, len(args))
	for i, a := range args {
		wargs[i] = runtime.WordArg{Text: a}
	}
	_, err := parseVethPairFlags(wargs)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unexpected positional argument "stray"`)
}

func TestParseVethPairFlags_TrailingFlagWithoutValue(t *testing.T) {
	t.Parallel()
	wargs := []runtime.Arg{runtime.WordArg{Text: "--ns"}}
	_, err := parseVethPairFlags(wargs)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--ns requires a value")
}

func TestHandleNetRelease_NoArgs(t *testing.T) {
	t.Parallel()
	_, err := callNet(t, "release")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exactly one $pair argument")
}

func TestHandleNetRelease_NonPairArg(t *testing.T) {
	t.Parallel()
	_, err := handleNet(driver.Ctx{
		Ctx:  t.Context(),
		Cmd:  "net",
		Args: []runtime.Arg{runtime.WordArg{Text: "release"}, scalarPairArg()},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "$pair argument")
}

func TestHandleNetRelease_IdempotentOnAlreadyReleased(t *testing.T) {
	t.Parallel()
	pair := &runtime.NetPair{
		Ns:       "ns0",
		HostLink: "h0",
		PeerLink: "p0",
		HostAddr: "198.51.100.1",
		PeerAddr: "198.51.100.2",
	}
	pair.MarkReleased()
	v, err := handleNet(driver.Ctx{
		Ctx:  t.Context(),
		Cmd:  "net",
		Args: []runtime.Arg{runtime.WordArg{Text: "release"}, pairArg(pair)},
	})
	require.NoError(t, err)
	require.Equal(t, semantics.OriginEnvelope, v.Kind())
	env, ok := v.Origin().(runtime.Envelope)
	require.True(t, ok, "release should publish a runtime.Envelope as origin")
	assert.True(t, env.OK())
	assert.Equal(t, 0, env.ExitCode)
}

func TestHandleNetExec_TooFewArgs(t *testing.T) {
	t.Parallel()
	_, err := callNet(t, "exec")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "$pair and a command")
}

func TestHandleNetExec_NonPairArg(t *testing.T) {
	t.Parallel()
	_, err := handleNet(driver.Ctx{
		Ctx:  t.Context(),
		Cmd:  "net",
		Args: []runtime.Arg{runtime.WordArg{Text: "exec"}, scalarPairArg(), runtime.WordArg{Text: "true"}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected a $pair or endpoint argument")
}

func TestHandleNetExec_RejectsReleasedHandle(t *testing.T) {
	t.Parallel()
	pair := &runtime.NetPair{Ns: "ns0", HostLink: "h0", PeerLink: "p0", HostAddr: "1.2.3.4", PeerAddr: "1.2.3.5"}
	pair.MarkReleased()
	_, err := handleNet(driver.Ctx{
		Ctx:  t.Context(),
		Cmd:  "net",
		Args: []runtime.Arg{runtime.WordArg{Text: "exec"}, pairArg(pair), runtime.WordArg{Text: "true"}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "released")
}

func TestHandleNetStart_TooFewArgs(t *testing.T) {
	t.Parallel()
	_, err := callNet(t, "start")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "$pair and a command")
}

func TestHandleNetStart_NonPairArg(t *testing.T) {
	t.Parallel()
	_, err := handleNet(driver.Ctx{
		Ctx:  t.Context(),
		Cmd:  "net",
		Args: []runtime.Arg{runtime.WordArg{Text: "start"}, scalarPairArg(), runtime.WordArg{Text: "sleep"}, runtime.WordArg{Text: "0"}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected a $pair or endpoint argument")
}

func TestHandleNetStart_RejectsReleasedHandle(t *testing.T) {
	t.Parallel()
	pair := &runtime.NetPair{Ns: "ns0", HostLink: "h0", PeerLink: "p0", HostAddr: "1.2.3.4", PeerAddr: "1.2.3.5"}
	pair.MarkReleased()
	_, err := handleNet(driver.Ctx{
		Ctx:  t.Context(),
		Cmd:  "net",
		Args: []runtime.Arg{runtime.WordArg{Text: "start"}, pairArg(pair), runtime.WordArg{Text: "sleep"}, runtime.WordArg{Text: "0"}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "released")
}

// TestNetPair_FieldsRemainReadableAfterRelease confirms the
// invariant the doc surface promises: net release marks the
// handle consumed, but $pair.host_link / $pair.peer_addr / ...
// still resolve through the standard path-walker so a
// downstream print or interpolation does not error.
func TestNetPair_FieldsRemainReadableAfterRelease(t *testing.T) {
	t.Parallel()
	pair := &runtime.NetPair{
		Ns:       "ns0",
		HostLink: "h0",
		PeerLink: "p0",
		HostAddr: "198.51.100.1",
		PeerAddr: "198.51.100.2",
	}
	pair.MarkReleased()
	v := runtime.ValueFromNetPair(pair)
	got, err := v.Lookup("$pair", "host_link")
	require.NoError(t, err)
	s, err := got.Scalar()
	require.NoError(t, err)
	assert.Equal(t, "h0", s)
}

// TestNetBuiltin_RegisteredInRegistry confirms the registry
// entry is reachable so 'help net' and dispatcher lookup both
// see it. Missing entry would cause a regression where 'net' falls
// through to the external-command runner and silently spawns
// a nonexistent /usr/bin/net binary.
func TestNetBuiltin_RegisteredInRegistry(t *testing.T) {
	t.Parallel()
	entry, ok := driver.Builtins()["net"]
	require.Truef(t, ok, "net is not in builtinRegistry")
	assert.NotNil(t, entry.Handler)
	assert.NotEmpty(t, entry.Usage)
	assert.NotEmpty(t, entry.Summary)
}

// TestHandleNetRelease_AutoModeReleasesLease guards the
// handler-to-pool wiring on the release path. An auto-address
// NetPair must trigger releasePoolSlot during teardown
// so the lockfile body picks up a released_at and the flock is
// dropped. The ip(8) commands inside handleNetRelease fail
// silently against the synthetic ns / link names; that is the
// existing behaviour and not what this test exercises.
func TestHandleNetRelease_AutoModeReleasesLease(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	lease, err := acquirePoolSlot(poolAcquireRequest{
		root:        root,
		origin:      "test_release.bpfman:1",
		nsName:      "ns-rel",
		linkAName:   "vea-rel",
		linkExists:  func(string) bool { return false },
		netnsExists: func(string) bool { return false },
	})
	require.NoError(t, err)
	pair := &runtime.NetPair{
		Ns:       "ns-rel",
		HostLink: "vea-rel",
		PeerLink: "veb-rel",
		HostAddr: lease.hostAddr,
		PeerAddr: lease.peerAddr,
	}
	rememberNetPairLease(pair, lease)
	v, err := handleNet(driver.Ctx{
		Ctx:  t.Context(),
		Cmd:  "net",
		Args: []runtime.Arg{runtime.WordArg{Text: "release"}, pairArg(pair)},
	})
	require.NoError(t, err)
	env, ok := v.Origin().(runtime.Envelope)
	require.True(t, ok)
	assert.True(t, env.OK())

	body, err := os.ReadFile(slotLockPath(root, lease.slot))
	require.NoError(t, err)
	var prov struct {
		Origin     string `json:"origin"`
		NsName     string `json:"ns_name"`
		LinkAName  string `json:"link_a_name"`
		ReleasedAt string `json:"released_at"`
	}
	require.NoError(t, json.Unmarshal(body, &prov))
	assert.Equal(t, "test_release.bpfman:1", prov.Origin)
	assert.Equal(t, "ns-rel", prov.NsName)
	assert.Equal(t, "vea-rel", prov.LinkAName)
	assert.NotEmpty(t, prov.ReleasedAt, "release path must write released_at into the slot body")
	assert.True(t, pair.IsReleased())
}

// isolatedPair constructs a NetnsVethPair fixture with the
// residue-convention names the real builder would generate.
func isolatedPair() *runtime.NetnsVethPair {
	return runtime.NewNetnsVethPair(
		runtime.NetnsVethEndpoint{Ns: "B0000000000a1Na", Link: "B0000000000a1Na", Addr: "198.51.100.1"},
		runtime.NetnsVethEndpoint{Ns: "B0000000000a1Nb", Link: "B0000000000a1Nb", Addr: "198.51.100.2"},
	)
}

// isolatedPairArg wraps a *runtime.NetnsVethPair the way the
// argument expander would when a script writes $pair.
func isolatedPairArg(p *runtime.NetnsVethPair) runtime.Arg {
	return runtime.StructuredValueArg{Name: "pair", Value: runtime.ValueFromNetnsVethPair(p)}
}

// endpointArg extracts $pair.a / $pair.b the way the path walker
// would and wraps it as a structured argument.
func endpointArg(t *testing.T, p *runtime.NetnsVethPair, side string) runtime.Arg {
	t.Helper()
	ep, err := runtime.ValueFromNetnsVethPair(p).LookupValue("$pair", side)
	require.NoError(t, err)
	return runtime.StructuredValueArg{Name: "pair." + side, Value: ep}
}

func TestHandleNetNetnsVethPair_RejectsFlags(t *testing.T) {
	t.Parallel()
	_, err := callNet(t, "netns-veth-pair", "--ns=x")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "takes no arguments")
}

func TestHandleNetNetnsVethPair_RejectsPositionals(t *testing.T) {
	t.Parallel()
	_, err := callNet(t, "netns-veth-pair", "stray")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "takes no arguments")
}

func TestHandleNetExec_BareIsolatedPairRejected(t *testing.T) {
	t.Parallel()
	_, err := handleNet(driver.Ctx{
		Ctx:  t.Context(),
		Cmd:  "net",
		Args: []runtime.Arg{runtime.WordArg{Text: "exec"}, isolatedPairArg(isolatedPair()), runtime.WordArg{Text: "true"}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "net exec: netns-veth-pair has two endpoints; use $pair.a or $pair.b")
}

func TestHandleNetStart_BareIsolatedPairRejected(t *testing.T) {
	t.Parallel()
	_, err := handleNet(driver.Ctx{
		Ctx:  t.Context(),
		Cmd:  "net",
		Args: []runtime.Arg{runtime.WordArg{Text: "start"}, isolatedPairArg(isolatedPair()), runtime.WordArg{Text: "sleep"}, runtime.WordArg{Text: "0"}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "net start: netns-veth-pair has two endpoints; use $pair.a or $pair.b")
}

func TestHandleNetExec_EndpointOfReleasedPairRejected(t *testing.T) {
	t.Parallel()
	pair := isolatedPair()
	pair.MarkReleased()
	_, err := handleNet(driver.Ctx{
		Ctx:  t.Context(),
		Cmd:  "net",
		Args: []runtime.Arg{runtime.WordArg{Text: "exec"}, endpointArg(t, pair, "a"), runtime.WordArg{Text: "true"}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "released")
}

func TestHandleNetStart_EndpointOfReleasedPairRejected(t *testing.T) {
	t.Parallel()
	pair := isolatedPair()
	pair.MarkReleased()
	_, err := handleNet(driver.Ctx{
		Ctx:  t.Context(),
		Cmd:  "net",
		Args: []runtime.Arg{runtime.WordArg{Text: "start"}, endpointArg(t, pair, "b"), runtime.WordArg{Text: "sleep"}, runtime.WordArg{Text: "0"}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "released")
}

func TestHandleNetRelease_EndpointRejected(t *testing.T) {
	t.Parallel()
	_, err := handleNet(driver.Ctx{
		Ctx:  t.Context(),
		Cmd:  "net",
		Args: []runtime.Arg{runtime.WordArg{Text: "release"}, endpointArg(t, isolatedPair(), "a")},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "net release: endpoint belongs to a netns-veth-pair; release the pair")
}

func TestHandleNetRelease_IsolatedIdempotentOnAlreadyReleased(t *testing.T) {
	t.Parallel()
	pair := isolatedPair()
	pair.MarkReleased()
	v, err := handleNet(driver.Ctx{
		Ctx:  t.Context(),
		Cmd:  "net",
		Args: []runtime.Arg{runtime.WordArg{Text: "release"}, isolatedPairArg(pair)},
	})
	require.NoError(t, err)
	env, ok := v.Origin().(runtime.Envelope)
	require.True(t, ok)
	assert.True(t, env.OK())
}

// TestHandleNetRelease_IsolatedAutoModeReleasesLease mirrors the
// host-end lease-release test for the isolated builder: releasing
// the pair must write released_at carrying both netns names so
// the next acquirer's leak check validates both tenants.
func TestHandleNetRelease_IsolatedAutoModeReleasesLease(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	lease, err := acquirePoolSlot(poolAcquireRequest{
		root:        root,
		origin:      "test_release.bpfman:1",
		nsName:      "B0000000000a1Na",
		nsBName:     "B0000000000a1Nb",
		linkAName:   "B0000000000a1Na",
		linkExists:  func(string) bool { return false },
		netnsExists: func(string) bool { return false },
	})
	require.NoError(t, err)
	pair := isolatedPair()
	rememberNetnsVethPairLease(pair, lease)
	v, err := handleNet(driver.Ctx{
		Ctx:  t.Context(),
		Cmd:  "net",
		Args: []runtime.Arg{runtime.WordArg{Text: "release"}, isolatedPairArg(pair)},
	})
	require.NoError(t, err)
	env, ok := v.Origin().(runtime.Envelope)
	require.True(t, ok)
	assert.True(t, env.OK())

	body, err := os.ReadFile(slotLockPath(root, lease.slot))
	require.NoError(t, err)
	var prov struct {
		NsName     string `json:"ns_name"`
		NsBName    string `json:"ns_b_name"`
		LinkAName  string `json:"link_a_name"`
		ReleasedAt string `json:"released_at"`
	}
	require.NoError(t, json.Unmarshal(body, &prov))
	assert.Equal(t, "B0000000000a1Na", prov.NsName)
	assert.Equal(t, "B0000000000a1Nb", prov.NsBName)
	assert.Equal(t, "B0000000000a1Na", prov.LinkAName)
	assert.NotEmpty(t, prov.ReleasedAt)
	assert.True(t, pair.IsReleased())
}

// TestNetExecNamespace pins the target-resolution rule shared by
// net exec and net start: a host-end $pair runs in its peer
// namespace, an endpoint runs in its own namespace.
func TestNetExecNamespace(t *testing.T) {
	t.Parallel()

	hostEnd := &runtime.NetPair{Ns: "ns0", HostLink: "h0", PeerLink: "p0", HostAddr: "1.2.3.4", PeerAddr: "1.2.3.5"}
	ns, err := netExecNamespace(pairArg(hostEnd))
	require.NoError(t, err)
	assert.Equal(t, "ns0", ns)

	pair := isolatedPair()
	ns, err = netExecNamespace(endpointArg(t, pair, "a"))
	require.NoError(t, err)
	assert.Equal(t, "B0000000000a1Na", ns)

	ns, err = netExecNamespace(endpointArg(t, pair, "b"))
	require.NoError(t, err)
	assert.Equal(t, "B0000000000a1Nb", ns)
}

// TestNetnsVethPair_FieldsRemainReadableAfterRelease mirrors the
// host-end invariant: release consumes the capability, not the
// historical description.
func TestNetnsVethPair_FieldsRemainReadableAfterRelease(t *testing.T) {
	t.Parallel()
	pair := isolatedPair()
	pair.MarkReleased()
	v := runtime.ValueFromNetnsVethPair(pair)
	got, err := v.Lookup("$pair", "a.addr")
	require.NoError(t, err)
	s, err := got.Scalar()
	require.NoError(t, err)
	assert.Equal(t, "198.51.100.1", s)
}
