// Package args provides the parsers the bpfman CLI binds its Kong
// operands and flags to, each validating its input at bind time.
//
// Rather than accept raw strings and validate them deep inside a
// command, the CLI registers a parser for each operand or flag. Kong
// calls the parser as it binds, so an ill-formed program ID, object
// path, or "KEY=value" pair is rejected before the command runs and the
// command body receives a value it can trust. This is the
// parse-don't-validate discipline: validation happens once, at the
// boundary, and downstream code cannot mistake the result for
// unvalidated input.
//
// IDs parse straight to their domain types (kernel.ProgramID,
// bpfman.LinkID) and an object path to a checked filesystem path. The
// structured operands -- KeyValue, GlobalData, ProgramSpec -- parse to
// small records whose fields carry the parsed components. The map
// helpers (MetadataMap, GlobalDataMap) fold those into the shapes the
// manager API expects.
package args
