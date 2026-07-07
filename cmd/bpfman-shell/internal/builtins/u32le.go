package builtins

import "github.com/bpfman/bpfman/cmd/bpfman-shell/driver"

func init() {
	Register(driver.Builtin{
		Name:     "u32le",
		Handler:  HandleU32LE,
		Category: driver.CategoryIO,
		Usage:    "u32le <integer>",
		Summary:  "Encode an integer as a 4-byte little-endian hex string.",
		Detail: "Returns 8 lowercase hex characters with no 0x prefix. " +
			"Useful for `bpfman -g NAME=HEX` global-data injection " +
			"where the .bpf.c declares `volatile const __u32`. " +
			"Rejects negative inputs and values that exceed UINT32_MAX.",
	})
}
