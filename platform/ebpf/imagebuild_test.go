package ebpf

import (
	"os"
	"strings"
	"testing"

	ciliumebpf "github.com/cilium/ebpf"
)

func TestNormaliseBPFMapTypeMatchesBytecodeImageSpec(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"PerCPUArray":        "per_cpu_array",
		"ProgramArray":       "prog_array",
		"LPMTrie":            "lpm_trie",
		"LRUPerCPUHash":      "lru_per_cpu_hash",
		"ReusePortSockArray": "reuseport_sockarray",
	}

	for input, want := range tests {
		if got := NormaliseBPFMapType(input); got != want {
			t.Fatalf("NormaliseBPFMapType(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestCiliumBytecodeInputFromFile(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"bpf_x86_bpfel.o":      "BC_AMD64_EL",
		"bpf_386_bpfel.o":      "BC_386_EL",
		"bpf_mips_bpfeb.o":     "BC_MIPS_EB",
		"bpf_mipsle_bpfel.o":   "BC_MIPSLE_EL",
		"bpf_mips64_bpfeb.o":   "BC_MIPS64_EB",
		"bpf_mips64le_bpfel.o": "BC_MIPS64LE_EL",
		"bpf_s390x_bpfeb.o":    "BC_S390X_EB",
	}

	for name, want := range tests {
		got, ok := bytecodeInputFromCiliumFile(name)
		if !ok {
			t.Fatalf("bytecodeInputFromCiliumFile(%q) did not match", name)
		}
		if got.BuildArg != want {
			t.Fatalf("bytecodeInputFromCiliumFile(%q).BuildArg = %q, want %q", name, got.BuildArg, want)
		}
	}
}

func TestCiliumBytecodeInputFromFileRejectsGenericEndianOnlyName(t *testing.T) {
	t.Parallel()

	if got, ok := bytecodeInputFromCiliumFile("bpf_bpfel.o"); ok {
		t.Fatalf("bytecodeInputFromCiliumFile matched generic file as %+v", got)
	}
}

func TestCiliumProjectBytecodeSourceRejectsUnrecognisedObjectInMixedDirectory(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFile(t, dir+"/bpf_x86_bpfel.o", "")
	writeFile(t, dir+"/bpf_future_bpfel.o", "")

	_, err := CiliumProjectBytecodeSource(dir)
	if err == nil {
		t.Fatal("CiliumProjectBytecodeSource returned nil error for unrecognised object")
	}
	if !strings.Contains(err.Error(), "unrecognised cilium/ebpf bytecode file") {
		t.Fatalf("CiliumProjectBytecodeSource error = %q, want unrecognised-file error", err)
	}
}

func TestBuildInfoFromCollectionSpecExcludesDottedMapsAndUnsupportedPrograms(t *testing.T) {
	t.Parallel()

	info, err := BuildInfoFromCollectionSpec("fixture.o", &ciliumebpf.CollectionSpec{
		Programs: map[string]*ciliumebpf.ProgramSpec{
			"pass":    {SectionName: "xdp"},
			"unknown": {SectionName: "not/a/supported/program/type"},
		},
		Maps: map[string]*ciliumebpf.MapSpec{
			".rodata":                {Type: ciliumebpf.Array},
			".kconfig":               {Type: ciliumebpf.Array},
			"xdp_pass_stats_map":     {Type: ciliumebpf.PerCPUArray},
			"xdp_pass_prog_array":    {Type: ciliumebpf.ProgramArray},
			"xdp_pass_reuseport_map": {Type: ciliumebpf.ReusePortSockArray},
		},
	})
	if err != nil {
		t.Fatalf("BuildInfoFromCollectionSpec returned error: %v", err)
	}

	assertStringMapEqual(t, info.Programs, map[string]string{"pass": "xdp"})
	assertStringMapEqual(t, info.Maps, map[string]string{
		"xdp_pass_stats_map":     "per_cpu_array",
		"xdp_pass_prog_array":    "prog_array",
		"xdp_pass_reuseport_map": "reuseport_sockarray",
	})
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func assertStringMapEqual(t *testing.T, got, want map[string]string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("len(got) = %d, want %d\ngot:  %#v\nwant: %#v", len(got), len(want), got, want)
	}
	for key, wantValue := range want {
		if got[key] != wantValue {
			t.Fatalf("got[%q] = %q, want %q\ngot:  %#v\nwant: %#v", key, got[key], wantValue, got, want)
		}
	}
}
