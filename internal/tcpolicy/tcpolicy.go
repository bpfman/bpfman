// Package tcpolicy holds build-time policy flags for the TC dispatcher
// that are read across package boundaries (the manager that acts on
// them and the tests that gate on them). It has no dependencies so it
// is cheap to import anywhere, including the e2e harness.
package tcpolicy

// ReclaimClsactOnDetach selects how bpfman handles the clsact qdisc it
// creates, once the last member of a TC dispatcher detaches.
//
// false (default): leave the clsact in place. A bare clsact is a
// harmless empty hook, and removing infrastructure bpfman may not
// exclusively own -- another tool can install filters on the same
// clsact -- is riskier than leaving it. This matches upstream Rust,
// which also never reclaims the qdisc. The lingering clsact is handled
// at attach: CreateTCFilter reuses an existing clsact, and tolerates the
// EEXIST a lagging qdisc dump can produce on a reused interface.
//
// true: reclaim the clsact on the last detach (RemoveTCClsactIfUnused
// only removes it when both filter blocks are empty, so a co-resident
// direction or a foreign owner's filters are never torn out). This gives
// bpfman the full create/destroy lifecycle and leaves no qdisc drift, at
// the cost of deleting a qdisc bpfman created.
//
// The two reclaim tests -- the fake-kernel TestTC_ClsactReclaimedOnLast-
// Detach and the .bpfman script of the same name -- gate dynamically on
// this flag: they run when it is true and skip when it is false, so
// flipping the const here enables them automatically, with nothing to
// hunt down.
const ReclaimClsactOnDetach = false
