// Package lower converts parsed syntax trees into the canonical IR.
//
// It is the explicit bridge between the surface language and the
// executable block-structured program model in shell/ir. The package
// owns AST-to-IR translation only; it does not parse, check, or run
// programs itself.
package lower
