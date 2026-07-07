// Package semantics is the closed-world semantic substrate of the
// shipped bpfman-shell DSL.
//
// The DSL is not a reusable language. It is a lifecycle
// orchestrator for bpfman programs and links, and its semantic
// universe is deliberately closed: Programs and Links are first-
// class citizens the runtime tags with OriginProgram / OriginLink
// and the checker validates against. Pretending otherwise --
// modelling semantics as generic and injecting bpfman knowledge
// from the cmd side -- only adds setup-order coupling and hides
// facts that are actually true at the language level.
//
// semantics therefore owns:
//
//   - the origin lattice (OriginKind) the runtime tags values with
//   - the type-kind walker shared by checker and runtime
//   - the bind-shape vocabulary
//   - pure-builtin metadata (parser-visible names, arities, kinds)
//   - bpfman Program / Link shapes, derived declaratively from the
//     bpfman package's own types via reflect rather than declared
//     by hand on the builtin side
//
// The bpfman dependency is intentional. semantics imports the
// bpfman API package because Programs and Links are part of the
// language the shell ships, not because semantics aspires to be
// reusable. Treat the role as: this is what the DSL knows about
// the world it lives in.
package semantics
