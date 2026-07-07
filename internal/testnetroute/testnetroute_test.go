package testnetroute

import (
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

//nolint:paralleltest // mutates process environment via t.Setenv.
func TestPref(t *testing.T) {
	t.Run("default when unset", func(t *testing.T) {
		t.Setenv(PrefEnvVar, "")
		p, err := Pref()
		require.NoError(t, err)
		assert.Equal(t, DefaultPref, p)
	})
	t.Run("override", func(t *testing.T) {
		t.Setenv(PrefEnvVar, "2500")
		p, err := Pref()
		require.NoError(t, err)
		assert.Equal(t, 2500, p)
	})
	t.Run("invalid is a loud error", func(t *testing.T) {
		for _, v := range []string{"0", "-1", "banana", "1e3"} {
			t.Setenv(PrefEnvVar, v)
			_, err := Pref()
			require.Error(t, err, "value %q", v)
		}
	})
}

func TestSpec(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "ip rule add pref 99 to 198.51.100.0/24 lookup main", Spec(99))
}

// TestMatches pins the ownership boundary: only the harness's exact
// unconstrained rule shape is recognised, at any preference. A
// constrained foreign rule routing the same subnet is neither
// satisfaction for Ensure nor a target for the cleanup sweep.
func TestMatches(t *testing.T) {
	t.Parallel()

	ours := func() netlink.Rule {
		r := *netlink.NewRule()
		r.Priority = 99
		r.Dst = dst()
		r.Table = unix.RT_TABLE_MAIN
		r.Family = netlink.FAMILY_V4
		return r
	}

	assert.True(t, matches(ours()), "our exact shape must match")
	custom := ours()
	custom.Priority = 6000
	assert.True(t, matches(custom), "preference is not part of the shape")

	_, otherNet, _ := net.ParseCIDR("10.0.0.0/8")

	mutations := map[string]func(*netlink.Rule){
		"from selector":   func(r *netlink.Rule) { r.Src = otherNet },
		"inverted":        func(r *netlink.Rule) { r.Invert = true },
		"fwmark":          func(r *netlink.Rule) { r.Mark = 0x1 },
		"iif":             func(r *netlink.Rule) { r.IifName = "eth0" },
		"oif":             func(r *netlink.Rule) { r.OifName = "eth0" },
		"tos":             func(r *netlink.Rule) { r.Tos = 4 },
		"dport":           func(r *netlink.Rule) { r.Dport = netlink.NewRulePortRange(80, 80) },
		"sport":           func(r *netlink.Rule) { r.Sport = netlink.NewRulePortRange(80, 80) },
		"ipproto":         func(r *netlink.Rule) { r.IPProto = 6 },
		"uid range":       func(r *netlink.Rule) { r.UIDRange = netlink.NewRuleUIDRange(0, 100) },
		"different dst":   func(r *netlink.Rule) { r.Dst = otherNet },
		"different table": func(r *netlink.Rule) { r.Table = 52 },
		"no dst":          func(r *netlink.Rule) { r.Dst = nil },
	}
	for name, mutate := range mutations {
		r := ours()
		mutate(&r)
		assert.False(t, matches(r), "mutation %q must not match", name)
	}
}
