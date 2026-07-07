// Package bpfmanbuiltin implements the `bpfman ...` builtin family.
//
// It is the shell's frontend for the product domain commands:
// program, show, image, link, dispatcher, and audit. The package adapts shell
// runtime arguments to CLI argv, invokes the external bpfman binary,
// and decodes the JSON result for bind-position commands.
package bpfmanbuiltin
