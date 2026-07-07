// Package testnetroute owns the policy-routing rule that makes the
// e2e harness's host-end veth topology immune to VPN route
// hijacking. Test veth pairs draw their addresses from RFC 5737
// TEST-NET-2 (198.51.100.0/24) and keep one end in the root
// namespace, so reply traffic resolves through host policy routing.
// VPNs such as Tailscale install rules (around preference 5200)
// that capture those destinations into their own tables, breaking
// every packet-path test. One rule at a lower preference sends
// TEST-NET-2 lookups to the main table, where the veth's connected
// or peer route lives; Linux evaluates rules in ascending
// preference order, so the test-net rule wins before any VPN rule
// is consulted.
//
// The preference defaults to 99 and is overridable via
// BPFMAN_E2E_POLICY_RULE_PREF for hosts where 99 is taken or where
// a VPN installs rules below it. The preference is only the
// mechanism: the invariant that matters -- replies to the test
// subnet resolve via the test veth -- is verified per script by
// lib.bpfman's require_host_route_to_peer precheck.
//
// The rule is global harness state, not per-pair state: scripts run
// in parallel (and in separate processes), so per-pair removal
// would race one script's teardown against another's traffic. Both
// pair creators install it idempotently and nothing removes it
// during a run; without test interfaces the rule is inert (the main
// table holds no TEST-NET-2 route, so lookups fall through
// unchanged). bpfman-e2e-cleanup sweeps every matching rule by
// destination and table, regardless of preference, so custom-pref
// installs cannot escape cleanup.
package testnetroute

import (
	"errors"
	"fmt"
	"net"
	"os"
	"slices"
	"strconv"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

// DefaultPref is the rule preference used when
// BPFMAN_E2E_POLICY_RULE_PREF is unset. It must beat VPN rules
// (Tailscale uses ~5200); the rule is scoped to TEST-NET-2 only,
// so a low preference cannot affect real traffic.
const DefaultPref = 99

// PrefEnvVar names the environment override for the preference.
const PrefEnvVar = "BPFMAN_E2E_POLICY_RULE_PREF"

// CIDR is the harness's address pool, RFC 5737 TEST-NET-2.
const CIDR = "198.51.100.0/24"

// Pref resolves the configured preference: the environment
// override when set and valid, otherwise DefaultPref. An invalid
// override is a loud error rather than a silent fallback.
func Pref() (int, error) {
	v := os.Getenv(PrefEnvVar)
	if v == "" {
		return DefaultPref, nil
	}
	n, err := strconv.ParseUint(v, 10, 31)
	if err != nil || n == 0 {
		return 0, fmt.Errorf("%s=%q is not a valid rule preference", PrefEnvVar, v)
	}
	return int(n), nil
}

// Spec renders the rule as the ip(8) invocation that creates it.
func Spec(pref int) string {
	return fmt.Sprintf("ip rule add pref %d to %s lookup main", pref, CIDR)
}

func dst() *net.IPNet {
	_, ipnet, err := net.ParseCIDR(CIDR)
	if err != nil {
		panic("testnetroute: invalid CIDR constant: " + err.Error())
	}
	return ipnet
}

func rule(pref int) *netlink.Rule {
	r := netlink.NewRule()
	r.Priority = pref
	r.Dst = dst()
	r.Table = unix.RT_TABLE_MAIN
	r.Family = netlink.FAMILY_V4
	return r
}

// matches reports whether r is exactly the harness's rule shape:
// TEST-NET-2 destinations looked up in the main table with NO other
// selector (no from, fwmark, iif/oif, ports, uid range, tos, or
// inversion), at any preference. The strictness is ownership: a
// constrained foreign rule such as "from 10.0.0.0/8 to
// 198.51.100.0/24 lookup main" does not establish the invariant for
// test replies and is not ours to delete, so it must match neither
// Ensure's satisfaction check nor the cleanup sweep. The
// non-selector defaults (Goto, Flow, Suppress*) are compared
// against the netlink library's NewRule initial values, which is
// also what RuleList starts each deserialised rule from.
func matches(r netlink.Rule) bool {
	return r.Table == unix.RT_TABLE_MAIN &&
		r.Dst != nil && r.Dst.String() == CIDR &&
		r.Src == nil &&
		!r.Invert &&
		r.IifName == "" && r.OifName == "" &&
		r.Mark == 0 && r.Mask == nil &&
		r.Tos == 0 && r.TunID == 0 &&
		r.Goto == -1 && r.Flow == -1 &&
		r.SuppressIfgroup == -1 && r.SuppressPrefixlen == -1 &&
		r.Dport == nil && r.Sport == nil &&
		r.IPProto == 0 && r.UIDRange == nil
}

// Installed returns every rule establishing the invariant,
// regardless of preference.
func Installed() ([]netlink.Rule, error) {
	rules, err := netlink.RuleList(netlink.FAMILY_V4)
	if err != nil {
		return nil, fmt.Errorf("list ip rules: %w", err)
	}

	var out []netlink.Rule
	for _, r := range rules {
		if matches(r) {
			out = append(out, r)
		}
	}
	return out, nil
}

// Ensure installs the harness rule at the configured preference if
// it is not already there. Only the configured preference counts:
// a stale harness rule at some other preference may sit ABOVE the
// VPN rules and not establish the invariant at all, so Ensure
// repairs by installing at the configured preference regardless
// (cleanup sweeps harness-shaped rules at every preference, and a
// transient duplicate at two preferences is harmless -- both name
// the same lookup). A foreign rule occupying the preference is a
// loud error rather than a replacement: silently displacing it
// could change real routing.
//
// Parallel creators (tests in one process, scripts across
// processes) race this function freely, so every step tolerates a
// concurrent install of OUR rule: a matching rule found at the
// preference is success, and an EEXIST from the final add is
// success once a matching rule is confirmed.
func Ensure() error {
	pref, err := Pref()
	if err != nil {
		return err
	}

	atPref, err := netlink.RuleListFiltered(netlink.FAMILY_V4,
		&netlink.Rule{Priority: pref}, netlink.RT_FILTER_PRIORITY)
	if err != nil {
		return fmt.Errorf("list ip rules at pref %d: %w", pref, err)
	}

	if slices.ContainsFunc(atPref, matches) {
		return nil
	}
	if len(atPref) > 0 {
		return fmt.Errorf("ip rule preference %d is occupied by a foreign rule (dst %v table %d); set %s to a free preference (wanted: %s)", pref, atPref[0].Dst, atPref[0].Table, PrefEnvVar, Spec(pref))
	}
	if err := netlink.RuleAdd(rule(pref)); err != nil {
		if errors.Is(err, os.ErrExist) {
			recheck, lerr := netlink.RuleListFiltered(netlink.FAMILY_V4,
				&netlink.Rule{Priority: pref}, netlink.RT_FILTER_PRIORITY)
			if lerr == nil {
				if slices.ContainsFunc(recheck, matches) {
					return nil
				}
			}
		}
		return fmt.Errorf("install test-net rule (%s): %w", Spec(pref), err)
	}
	return nil
}
