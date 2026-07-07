// Package residue finds and removes kernel-side residue left
// behind by bpfman and its e2e suite: pinned XDP dispatcher links
// whose owning process is gone, clsact qdiscs hosting a
// tc_dispatcher filter, and the veth pairs / dummy interfaces /
// network namespaces that the e2e harness names with the
// `B<hex>N[ab]?` convention.
//
// Two consumers share the package. The cmd/bpfman-e2e-cleanup CLI
// uses it to drain a host before a re-run; the e2e suite uses it
// to clean up stale state at TestMain entry. Both go through the
// same Residue value and the same Apply path so the two stay in
// step.
package residue
