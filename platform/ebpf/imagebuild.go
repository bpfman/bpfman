package ebpf

import (
	"debug/elf"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	ciliumebpf "github.com/cilium/ebpf"

	"github.com/bpfman/bpfman/internal/imagebuild"
	"github.com/bpfman/bpfman/kernel"
)

// CiliumProjectBytecodeSource discovers cilium/ebpf bpf2go object files
// in dir and returns a multi-architecture image build source.
func CiliumProjectBytecodeSource(dir string) (imagebuild.BytecodeSource, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return imagebuild.BytecodeSource{}, err
	}

	var inputs []imagebuild.BytecodeInput
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".o" {
			continue
		}
		input, ok := bytecodeInputFromCiliumFile(entry.Name())
		if !ok {
			return imagebuild.BytecodeSource{}, fmt.Errorf("unrecognised cilium/ebpf bytecode file %s", filepath.Join(dir, entry.Name()))
		}
		input.Path = filepath.Join(dir, entry.Name())
		inputs = append(inputs, input)
	}

	slices.SortFunc(inputs, func(a, b imagebuild.BytecodeInput) int {
		return strings.Compare(a.Platform, b.Platform)
	})

	if len(inputs) == 0 {
		return imagebuild.BytecodeSource{}, fmt.Errorf("no cilium/ebpf bytecode files found in %s", dir)
	}
	return imagebuild.MultiArchSource(inputs)
}

func bytecodeInputFromCiliumFile(name string) (imagebuild.BytecodeInput, bool) {
	base := strings.TrimSuffix(strings.ToLower(name), filepath.Ext(name))
	parts := strings.FieldsFunc(base, func(r rune) bool {
		return r == '_' || r == '-' || r == '.'
	})
	var endian string
	for _, part := range parts {
		if part == "bpfel" || part == "bpfeb" {
			endian = part
		}
	}
	if endian == "" {
		return imagebuild.BytecodeInput{}, false
	}
	for _, part := range parts {
		if input, ok := imagebuild.BytecodeInputForCiliumObject(part, endian); ok {
			return input, true
		}
	}
	return imagebuild.BytecodeInput{}, false
}

// InspectBytecode validates one BPF object and extracts image label
// metadata from it.
func InspectBytecode(path string, expectedEndian elf.Data) (imagebuild.Info, error) {
	spec, err := validateBPFBytecode(path, expectedEndian)
	if err != nil {
		return imagebuild.Info{}, err
	}
	return BuildInfoFromCollectionSpec(path, spec)
}

func validateBPFBytecode(path string, expectedEndian elf.Data) (*ciliumebpf.CollectionSpec, error) {
	f, err := elf.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open bytecode ELF: %w", err)
	}

	if expectedEndian != 0 && f.Data != expectedEndian {
		_ = f.Close()
		return nil, fmt.Errorf("bytecode %s has %s endianness, expected %s", path, f.Data, expectedEndian)
	}
	_ = f.Close()

	spec, err := ciliumebpf.LoadCollectionSpec(path)
	if err != nil {
		return nil, fmt.Errorf("load bytecode collection spec: %w", err)
	}
	return spec, nil
}

// BuildInfoFromCollectionSpec extracts bytecode image metadata from a
// loaded cilium/ebpf collection spec.
func BuildInfoFromCollectionSpec(path string, spec *ciliumebpf.CollectionSpec) (imagebuild.Info, error) {
	if len(spec.Programs) == 0 {
		return imagebuild.Info{}, fmt.Errorf("no programs found in bytecode %s", path)
	}

	programs := make(map[string]string, len(spec.Programs))
	for name, prog := range spec.Programs {
		progType := InferProgramType(prog.SectionName)
		if progType.String() == "" {
			continue
		}
		programs[name] = progType.String()
	}
	if len(programs) == 0 {
		return imagebuild.Info{}, fmt.Errorf("no supported programs found in bytecode %s", path)
	}

	maps := make(map[string]string, len(spec.Maps))
	for name, m := range spec.Maps {
		if kernel.IsInternalMapName(name) {
			continue
		}
		maps[name] = NormaliseBPFMapType(m.Type.String())
	}

	return imagebuild.Info{Programs: programs, Maps: maps}, nil
}

// NormaliseBPFMapType converts cilium/ebpf map type names to the OCI
// label spellings used by bpfman bytecode images.
func NormaliseBPFMapType(mapType string) string {
	switch mapType {
	case "Unspecified":
		return "unspec"
	case "Hash":
		return "hash"
	case "Array":
		return "array"
	case "ProgramArray":
		return "prog_array"
	case "PerfEventArray":
		return "perf_event_array"
	case "PerCPUHash":
		return "per_cpu_hash"
	case "PerCPUArray":
		return "per_cpu_array"
	case "StackTrace":
		return "stack_trace"
	case "CgroupArray":
		return "cgroup_array"
	case "LRUHash":
		return "lru_hash"
	case "LRUCPUHash", "LRUPerCPUHash":
		return "lru_per_cpu_hash"
	case "LPMTrie":
		return "lpm_trie"
	case "ArrayOfMaps":
		return "array_of_maps"
	case "HashOfMaps":
		return "hash_of_maps"
	case "DevMap":
		return "devmap"
	case "SockMap":
		return "sockmap"
	case "CPUMap":
		return "cpumap"
	case "XSKMap":
		return "xskmap"
	case "SockHash":
		return "sockhash"
	case "CgroupStorage":
		return "cgroup_storage"
	case "ReusePortSockArray":
		return "reuseport_sockarray"
	case "PerCPUCgroupStorage":
		return "per_cpu_cgroup_storage"
	case "Queue":
		return "queue"
	case "Stack":
		return "stack"
	case "SkStorage":
		return "sk_storage"
	case "DevMapHash":
		return "devmap_hash"
	case "StructOps":
		return "struct_ops"
	case "RingBuf":
		return "ringbuf"
	case "InodeStorage":
		return "inode_storage"
	case "TaskStorage":
		return "task_storage"
	case "BloomFilter":
		return "bloom_filter"
	case "UserRingBuf":
		return "user_ringbuf"
	case "CgrpStorage":
		return "cgrp_storage"
	case "Arena":
		return "arena"
	default:
		return strings.ToLower(mapType)
	}
}
