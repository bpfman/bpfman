package bpfmanbuiltin

import "github.com/bpfman/bpfman/cmd/bpfman-shell/shell/runtime"

func word(s string) runtime.Arg { return runtime.WordArg{Text: s} }
