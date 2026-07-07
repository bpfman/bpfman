package imagebuild

import (
	"debug/elf"
	"strings"
	"testing"
)

func TestMultiArchSourceBuildsStablePlan(t *testing.T) {
	t.Parallel()

	source, err := MultiArchSource([]BytecodeInput{
		{Platform: "linux/amd64", BuildArg: "BC_AMD64_EL", Endian: elf.ELFDATA2LSB, Path: "bpf_x86_bpfel.o"},
		{Platform: "linux/s390x", BuildArg: "BC_S390X_EB", Endian: elf.ELFDATA2MSB, Path: "bpf_s390_bpfeb.o"},
	})
	if err != nil {
		t.Fatalf("MultiArchSource returned error: %v", err)
	}

	var inspected []string
	plan, err := Build(source, func(path string, expectedEndian elf.Data) (Info, error) {
		inspected = append(inspected, path+":"+expectedEndian.String())
		return Info{
			Programs: map[string]string{"pass": "xdp"},
			Maps:     map[string]string{"xdp_pass_stats_map": "per_cpu_array"},
		}, nil
	})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	assertStringSliceEqual(t, plan.Platforms, []string{"linux/amd64", "linux/s390x"})
	assertStringSliceEqual(t, plan.BuildArgs, []string{
		"BC_AMD64_EL=bpf_x86_bpfel.o",
		"BC_S390X_EB=bpf_s390_bpfeb.o",
	})
	assertStringSliceEqual(t, inspected, []string{
		"bpf_x86_bpfel.o:ELFDATA2LSB",
		"bpf_s390_bpfeb.o:ELFDATA2MSB",
	})
}

func TestBuildValidatesEveryInput(t *testing.T) {
	t.Parallel()

	source, err := MultiArchSource([]BytecodeInput{
		{Platform: "linux/amd64", BuildArg: "BC_AMD64_EL", Endian: elf.ELFDATA2LSB, Path: "good.o"},
		{Platform: "linux/s390x", BuildArg: "BC_S390X_EB", Endian: elf.ELFDATA2MSB, Path: "bad.o"},
	})
	if err != nil {
		t.Fatalf("MultiArchSource returned error: %v", err)
	}

	_, err = Build(source, func(path string, expectedEndian elf.Data) (Info, error) {
		if path == "bad.o" {
			return Info{}, errWrongEndian
		}
		return Info{Programs: map[string]string{"pass": "xdp"}, Maps: map[string]string{}}, nil
	})
	if err == nil {
		t.Fatal("Build returned nil error for failing secondary input")
	}
	if err != errWrongEndian {
		t.Fatalf("Build error = %v, want %v", err, errWrongEndian)
	}
}

func TestFormatText(t *testing.T) {
	t.Parallel()

	got, err := Format(Plan{
		BuildArgs: []string{"BYTECODE_FILE=e2e/testdata/bpf/xdp_pass.bpf.o"},
		Labels: Info{
			Programs: map[string]string{"pass": "xdp"},
			Maps:     map[string]string{"xdp_pass_stats_map": "per_cpu_array"},
		},
	}, "text")
	if err != nil {
		t.Fatalf("Format returned error: %v", err)
	}

	want := "BYTECODE_FILE=e2e/testdata/bpf/xdp_pass.bpf.o\n" +
		"PROGRAMS={\"pass\":\"xdp\"}\n" +
		"MAPS={\"xdp_pass_stats_map\":\"per_cpu_array\"}\n"
	if got != want {
		t.Fatalf("Format text = %q, want %q", got, want)
	}
}

func TestFormatJSON(t *testing.T) {
	t.Parallel()

	got, err := Format(Plan{
		Platforms: []string{"linux/amd64", "linux/arm64"},
		BuildArgs: []string{
			"BC_AMD64_EL=e2e/testdata/bpf/xdp_pass.bpf.o",
			"BC_ARM64_EL=e2e/testdata/bpf/xdp_pass.bpf.o",
		},
		Labels: Info{
			Programs: map[string]string{"pass": "xdp"},
			Maps:     map[string]string{"xdp_pass_stats_map": "per_cpu_array"},
		},
	}, "json")
	if err != nil {
		t.Fatalf("Format returned error: %v", err)
	}

	want := "{\n" +
		"  \"platforms\": [\n" +
		"    \"linux/amd64\",\n" +
		"    \"linux/arm64\"\n" +
		"  ],\n" +
		"  \"build_args\": {\n" +
		"    \"BC_AMD64_EL\": \"e2e/testdata/bpf/xdp_pass.bpf.o\",\n" +
		"    \"BC_ARM64_EL\": \"e2e/testdata/bpf/xdp_pass.bpf.o\"\n" +
		"  },\n" +
		"  \"programs\": {\n" +
		"    \"pass\": \"xdp\"\n" +
		"  },\n" +
		"  \"maps\": {\n" +
		"    \"xdp_pass_stats_map\": \"per_cpu_array\"\n" +
		"  }\n" +
		"}\n"
	if got != want {
		t.Fatalf("Format json = %q, want %q", got, want)
	}
}

func TestFormatJSONSingleArchUsesEmptyPlatforms(t *testing.T) {
	t.Parallel()

	got, err := Format(Plan{
		BuildArgs: []string{"BYTECODE_FILE=e2e/testdata/bpf/xdp_pass.bpf.o"},
		Labels: Info{
			Programs: map[string]string{"pass": "xdp"},
			Maps:     map[string]string{"xdp_pass_stats_map": "per_cpu_array"},
		},
	}, "json")
	if err != nil {
		t.Fatalf("Format returned error: %v", err)
	}

	if !strings.Contains(got, "\"platforms\": []") {
		t.Fatalf("Format json = %q, want empty platforms array", got)
	}
}

type testError string

func (e testError) Error() string { return string(e) }

const errWrongEndian = testError("wrong endian")

func assertStringSliceEqual(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("len(got) = %d, want %d\ngot:  %#v\nwant: %#v", len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("arg[%d] = %q, want %q\ngot:  %#v\nwant: %#v", i, got[i], want[i], got, want)
		}
	}
}
