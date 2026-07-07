// Package fixturemode owns the BPFMAN_SHELL_MODE helper entry points.
//
// These modes bypass the normal script runner and turn
// bpfman-shell into small helper processes used by tests and
// fixtures. They are command-private support code for the binary,
// not part of the shell language itself.
package fixturemode
