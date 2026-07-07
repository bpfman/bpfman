package imagebuild

import (
	"debug/elf"
	"encoding/json"
	"fmt"
	"runtime"
	"slices"
	"strings"
)

// BytecodeInput is one object file that can be copied into a bytecode
// image build context.
type BytecodeInput struct {
	// Platform is the OCI platform string (for example "linux/amd64")
	// this object targets. It is empty for a single host-architecture
	// input.
	Platform string

	// BuildArg is the Docker build-arg name that carries this object's
	// path into the image build context (for example "BC_AMD64_EL", or
	// "BYTECODE_FILE" for a single host input).
	BuildArg string

	// Endian is the ELF byte order the object must have. Inspection
	// rejects a mismatch; the zero value disables the check.
	Endian elf.Data

	// Path is the filesystem path of the object file to copy in.
	Path string
}

// Arch identifies one architecture/endian bytecode image slot.
type Arch string

const (
	// Arch386EL is the linux/386 little-endian slot.
	Arch386EL Arch = "386-el"

	// ArchAmd64EL is the linux/amd64 little-endian slot.
	ArchAmd64EL Arch = "amd64-el"

	// ArchArmEL is the linux/arm little-endian slot.
	ArchArmEL Arch = "arm-el"

	// ArchArm64EL is the linux/arm64 little-endian slot.
	ArchArm64EL Arch = "arm64-el"

	// ArchLoong64EL is the linux/loong64 little-endian slot.
	ArchLoong64EL Arch = "loong64-el"

	// ArchMipsEB is the linux/mips big-endian slot.
	ArchMipsEB Arch = "mips-eb"

	// ArchMipsleEL is the linux/mipsle little-endian slot.
	ArchMipsleEL Arch = "mipsle-el"

	// ArchMips64EB is the linux/mips64 big-endian slot.
	ArchMips64EB Arch = "mips64-eb"

	// ArchMips64leEL is the linux/mips64le little-endian slot.
	ArchMips64leEL Arch = "mips64le-el"

	// ArchPpc64EB is the linux/ppc64 big-endian slot.
	ArchPpc64EB Arch = "ppc64-eb"

	// ArchPpc64leEL is the linux/ppc64le little-endian slot.
	ArchPpc64leEL Arch = "ppc64le-el"

	// ArchRiscv64EL is the linux/riscv64 little-endian slot.
	ArchRiscv64EL Arch = "riscv64-el"

	// ArchS390xEB is the linux/s390x big-endian slot.
	ArchS390xEB Arch = "s390x-eb"
)

type archSpec struct {
	arch         Arch
	platform     string
	buildArg     string
	endian       elf.Data
	ciliumArchs  []string
	ciliumEndian string
}

var archRegistry = []archSpec{
	{arch: Arch386EL, platform: "linux/386", buildArg: "BC_386_EL", endian: elf.ELFDATA2LSB, ciliumArchs: []string{"386"}, ciliumEndian: "bpfel"},
	{arch: ArchAmd64EL, platform: "linux/amd64", buildArg: "BC_AMD64_EL", endian: elf.ELFDATA2LSB, ciliumArchs: []string{"x86"}, ciliumEndian: "bpfel"},
	{arch: ArchArmEL, platform: "linux/arm", buildArg: "BC_ARM_EL", endian: elf.ELFDATA2LSB, ciliumArchs: []string{"arm"}, ciliumEndian: "bpfel"},
	{arch: ArchArm64EL, platform: "linux/arm64", buildArg: "BC_ARM64_EL", endian: elf.ELFDATA2LSB, ciliumArchs: []string{"arm64"}, ciliumEndian: "bpfel"},
	{arch: ArchLoong64EL, platform: "linux/loong64", buildArg: "BC_LOONG64_EL", endian: elf.ELFDATA2LSB, ciliumArchs: []string{"loongarch", "loong64"}, ciliumEndian: "bpfel"},
	{arch: ArchMipsEB, platform: "linux/mips", buildArg: "BC_MIPS_EB", endian: elf.ELFDATA2MSB, ciliumArchs: []string{"mips"}, ciliumEndian: "bpfeb"},
	{arch: ArchMipsleEL, platform: "linux/mipsle", buildArg: "BC_MIPSLE_EL", endian: elf.ELFDATA2LSB, ciliumArchs: []string{"mipsle"}, ciliumEndian: "bpfel"},
	{arch: ArchMips64EB, platform: "linux/mips64", buildArg: "BC_MIPS64_EB", endian: elf.ELFDATA2MSB, ciliumArchs: []string{"mips64"}, ciliumEndian: "bpfeb"},
	{arch: ArchMips64leEL, platform: "linux/mips64le", buildArg: "BC_MIPS64LE_EL", endian: elf.ELFDATA2LSB, ciliumArchs: []string{"mips64le"}, ciliumEndian: "bpfel"},
	{arch: ArchPpc64EB, platform: "linux/ppc64", buildArg: "BC_PPC64_EB", endian: elf.ELFDATA2MSB, ciliumArchs: []string{"powerpc", "ppc64"}, ciliumEndian: "bpfeb"},
	{arch: ArchPpc64leEL, platform: "linux/ppc64le", buildArg: "BC_PPC64LE_EL", endian: elf.ELFDATA2LSB, ciliumArchs: []string{"powerpc", "ppc64le"}, ciliumEndian: "bpfel"},
	{arch: ArchRiscv64EL, platform: "linux/riscv64", buildArg: "BC_RISCV64_EL", endian: elf.ELFDATA2LSB, ciliumArchs: []string{"riscv", "riscv64"}, ciliumEndian: "bpfel"},
	{arch: ArchS390xEB, platform: "linux/s390x", buildArg: "BC_S390X_EB", endian: elf.ELFDATA2MSB, ciliumArchs: []string{"s390", "s390x"}, ciliumEndian: "bpfeb"},
}

// BytecodeInputForArch returns a bytecode input for one registry
// architecture slot.
func BytecodeInputForArch(arch Arch, path string) (BytecodeInput, bool) {
	for _, spec := range archRegistry {
		if spec.arch == arch {
			return BytecodeInput{
				Platform: spec.platform,
				BuildArg: spec.buildArg,
				Endian:   spec.endian,
				Path:     path,
			}, true
		}
	}
	return BytecodeInput{}, false
}

// BytecodeInputForPlatform returns a bytecode input for one OCI platform.
func BytecodeInputForPlatform(platform, path string) (BytecodeInput, bool) {
	for _, spec := range archRegistry {
		if spec.platform == platform {
			return BytecodeInput{
				Platform: spec.platform,
				BuildArg: spec.buildArg,
				Endian:   spec.endian,
				Path:     path,
			}, true
		}
	}
	return BytecodeInput{}, false
}

// SupportedPlatforms returns the OCI platforms accepted as explicit
// bytecode input mappings.
func SupportedPlatforms() []string {
	platforms := make([]string, 0, len(archRegistry))
	for _, spec := range archRegistry {
		platforms = append(platforms, spec.platform)
	}
	return platforms
}

// BytecodeInputForCiliumObject returns the image build slot for a
// cilium/ebpf object filename architecture and endian token.
func BytecodeInputForCiliumObject(arch, endian string) (BytecodeInput, bool) {
	for _, spec := range archRegistry {
		if spec.ciliumEndian != endian {
			continue
		}
		if slices.Contains(spec.ciliumArchs, arch) {
			input, _ := BytecodeInputForArch(spec.arch, "")
			return input, true
		}
	}
	return BytecodeInput{}, false
}

// BytecodeSource is the parsed bytecode source selected by the CLI.
// Once constructed, mutually-exclusive source modes are no longer
// representable.
type BytecodeSource struct {
	inputs    []BytecodeInput
	multiArch bool
}

// SingleFileSource constructs a source for the host architecture.
func SingleFileSource(path string) (BytecodeSource, error) {
	if path == "" {
		return BytecodeSource{}, fmt.Errorf("bytecode path is required")
	}
	return BytecodeSource{
		inputs: []BytecodeInput{{
			BuildArg: "BYTECODE_FILE",
			Endian:   HostELFData(),
			Path:     path,
		}},
	}, nil
}

// MultiArchSource constructs a source for explicitly supplied
// architecture-specific bytecode inputs.
func MultiArchSource(inputs []BytecodeInput) (BytecodeSource, error) {
	if len(inputs) == 0 {
		return BytecodeSource{}, fmt.Errorf("one bytecode input is required")
	}
	copied := append([]BytecodeInput(nil), inputs...)
	for _, input := range copied {
		if input.Platform == "" || input.BuildArg == "" || input.Path == "" {
			return BytecodeSource{}, fmt.Errorf("incomplete bytecode input")
		}
	}
	return BytecodeSource{inputs: copied, multiArch: true}, nil
}

// HostELFData returns the native ELF byte order for this process.
func HostELFData() elf.Data {
	switch runtime.GOARCH {
	case "mips", "mips64", "ppc64", "s390x":
		return elf.ELFDATA2MSB
	default:
		return elf.ELFDATA2LSB
	}
}

// Info is the bytecode metadata needed to label a bytecode image.
type Info struct {
	// Programs maps each BPF program name to its bpfman program-type
	// spelling (for example "xdp", "tc"). It is rendered into the
	// image's io.ebpf.programs label.
	Programs map[string]string

	// Maps maps each BPF map name to its normalised map-type spelling
	// (for example "hash", "array"). It is rendered into the image's
	// io.ebpf.maps label.
	Maps map[string]string
}

// Inspector validates one bytecode input and returns its metadata.
type Inspector func(path string, expectedEndian elf.Data) (Info, error)

// Plan is the complete build contract shared by image build and
// generate-build-args.
type Plan struct {
	// Platforms lists the OCI platforms to build, in input order. It is
	// empty for a single host-architecture build.
	Platforms []string

	// BuildArgs holds the Docker build args, each of the form NAME=path,
	// that point the build at every bytecode object.
	BuildArgs []string

	// Labels is the bytecode metadata, derived from the first input,
	// used to label the resulting image.
	Labels Info
}

// Build computes the image build plan and validates every bytecode
// input. Image labels are derived from the first input.
func Build(source BytecodeSource, inspect Inspector) (Plan, error) {
	if inspect == nil {
		return Plan{}, fmt.Errorf("bytecode inspector is required")
	}
	if len(source.inputs) == 0 {
		return Plan{}, fmt.Errorf("no bytecode files found for building BPF image")
	}

	plan := Plan{}
	for i, input := range source.inputs {
		info, err := inspect(input.Path, input.Endian)
		if err != nil {
			return Plan{}, err
		}

		if i == 0 {
			plan.Labels = info
		}
		if source.multiArch {
			plan.Platforms = append(plan.Platforms, input.Platform)
		}
		plan.BuildArgs = append(plan.BuildArgs, input.BuildArg+"="+input.Path)
	}
	return plan, nil
}

// Format renders a plan as either text build args or JSON.
func Format(plan Plan, output string) (string, error) {
	switch output {
	case "", "text":
		return FormatText(plan)
	case "json":
		return FormatJSON(plan)
	default:
		return "", fmt.Errorf("unsupported output format %q", output)
	}
}

// FormatText renders the shell-oriented KEY=VALUE format.
func FormatText(plan Plan) (string, error) {
	programs, maps, err := LabelBuildArgValues(plan.Labels)
	if err != nil {
		return "", err
	}

	lines := append([]string(nil), plan.BuildArgs...)
	lines = append(lines, "PROGRAMS="+programs, "MAPS="+maps)
	return strings.Join(lines, "\n") + "\n", nil
}

// FormatJSON renders a structured version of the build contract.
func FormatJSON(plan Plan) (string, error) {
	buildArgs := make(map[string]string, len(plan.BuildArgs))
	for _, arg := range plan.BuildArgs {
		key, value, ok := strings.Cut(arg, "=")
		if !ok {
			return "", fmt.Errorf("invalid build arg %q", arg)
		}
		buildArgs[key] = value
	}

	platforms := append([]string(nil), plan.Platforms...)
	if platforms == nil {
		platforms = []string{}
	}

	out := struct {
		Platforms []string          `json:"platforms"`
		BuildArgs map[string]string `json:"build_args"`
		Programs  map[string]string `json:"programs"`
		Maps      map[string]string `json:"maps"`
	}{
		Platforms: platforms,
		BuildArgs: buildArgs,
		Programs:  plan.Labels.Programs,
		Maps:      plan.Labels.Maps,
	}
	bytes, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return "", err
	}
	return string(bytes) + "\n", nil
}

// LabelBuildArgValues renders OCI label build args from typed metadata.
func LabelBuildArgValues(info Info) (programs string, maps string, err error) {
	programBytes, err := json.Marshal(info.Programs)
	if err != nil {
		return "", "", err
	}

	mapBytes, err := json.Marshal(info.Maps)
	if err != nil {
		return "", "", err
	}
	return string(programBytes), string(mapBytes), nil
}
