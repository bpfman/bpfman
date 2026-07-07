package builtins

import "github.com/bpfman/bpfman/cmd/bpfman-shell/driver"

func init() {
	Register(driver.Builtin{
		Name:     "u64le",
		Handler:  HandleU64LE,
		Category: driver.CategoryIO,
		Usage:    "u64le <integer>",
		Summary:  "Encode an integer as an 8-byte little-endian hex string.",
		Detail: "Returns 16 lowercase hex characters with no 0x prefix. " +
			"Useful for `bpfman -g NAME=HEX` global-data injection " +
			"where the .bpf.c declares `volatile const __u64`. " +
			"Rejects negative inputs (Go uint64 max is the upper bound).",
	})
}
