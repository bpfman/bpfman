package bpfmanbuiltin

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/driver"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/runtime"
	"github.com/bpfman/bpfman/internal/registryfixture"
)

const (
	e2eBytecodeSourceEnv     = "BPFMAN_E2E_BYTECODE_SOURCE"
	e2eBytecodeSourceImage   = "image"
	e2eRepoRootEnv           = "BPFMAN_E2E_REPO_ROOT"
	e2eImageBrokerPullPolicy = "Always"
)

var brokeredImageSeq uint64

var brokeredImageCache = struct {
	sync.Mutex
	refs map[string]string
}{refs: make(map[string]string)}

func maybeBrokerLoadFileArgs(ctx context.Context, args []runtime.Arg) ([]runtime.Arg, error) {
	if os.Getenv(e2eBytecodeSourceEnv) != e2eBytecodeSourceImage {
		return args, nil
	}
	if !isLoadFileArgs(args) {
		return args, nil
	}

	bytecodePath, err := argToCLIText(args[3])
	if err != nil {
		return nil, err
	}

	ref, err := brokerBytecodeImage(ctx, bytecodePath)
	if err != nil {
		return nil, err
	}
	return loadImageArgsFromLoadFile(args, ref)
}

func isLoadFileArgs(args []runtime.Arg) bool {
	return len(args) >= 3 &&
		driver.ArgText(args[0]) == "program" &&
		driver.ArgText(args[1]) == "load" &&
		driver.ArgText(args[2]) == "file"
}

func loadImageArgsFromLoadFile(args []runtime.Arg, imageRef string) ([]runtime.Arg, error) {
	argv := []string{
		"program", "load", "image",
		imageRef,
		"--pull-policy", e2eImageBrokerPullPolicy,
	}
	for i, arg := range args[4:] {
		text, err := argToCLIText(arg)
		if err != nil {
			return nil, fmt.Errorf("program load file arg %d: %w", i+5, err)
		}

		argv = append(argv, text)
	}

	out := make([]runtime.Arg, 0, len(argv))
	for _, a := range argv {
		out = append(out, runtime.WordArg{Text: a})
	}
	return out, nil
}

func renderBrokeredImageBuildArgs(bytecode, imageRef string) []string {
	var argv []string
	argv = append(argv,
		"image", "build",
		imageRef,
		bytecode,
	)
	return argv
}

func brokerBytecodeImage(ctx context.Context, bytecodePath string) (string, error) {
	registryHost, err := registryfixture.Host()
	if err != nil {
		return "", fmt.Errorf("%s=image requires registry host: %w", e2eBytecodeSourceEnv, err)
	}

	repoRoot := os.Getenv(e2eRepoRootEnv)
	if repoRoot == "" {
		return "", fmt.Errorf("%s=image requires %s", e2eBytecodeSourceEnv, e2eRepoRootEnv)
	}
	absBytecode, err := filepath.Abs(bytecodePath)
	if err != nil {
		return "", err
	}

	plan, err := planBrokeredBuild(repoRoot, absBytecode)
	if err != nil {
		return "", err
	}

	key := filepath.Clean(absBytecode)
	brokeredImageCache.Lock()
	if ref := brokeredImageCache.refs[key]; ref != "" {
		brokeredImageCache.Unlock()
		return ref, nil
	}
	ref := imageRefForBytecode(registryHost, plan.RelName)
	if err := buildBrokeredImage(ctx, plan.Bytecode, ref); err != nil {
		brokeredImageCache.Unlock()
		return "", err
	}

	brokeredImageCache.refs[key] = ref
	brokeredImageCache.Unlock()
	return ref, nil
}

// brokeredBuild names a brokered bytecode image and the object it is
// built from.
type brokeredBuild struct {
	RelName  string // repo-relative, slash-separated; names the image
	Bytecode string // path handed to `bpfman image build`
}

// planBrokeredBuild validates that absBytecode lies within repoRoot and
// returns the plan for building its image.
func planBrokeredBuild(repoRoot, absBytecode string) (brokeredBuild, error) {
	rel, err := filepath.Rel(repoRoot, absBytecode)
	if err != nil {
		return brokeredBuild{}, err
	}

	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return brokeredBuild{}, fmt.Errorf("bytecode path %q is outside repository root %s", absBytecode, repoRoot)
	}
	return brokeredBuild{RelName: filepath.ToSlash(rel), Bytecode: absBytecode}, nil
}

func imageRefForBytecode(registryHost, relBytecode string) string {
	sum := sha256.Sum256([]byte(relBytecode))
	base := strings.TrimSuffix(filepath.Base(relBytecode), filepath.Ext(relBytecode))
	name := registryfixture.SanitiseComponent(base)
	tag := fmt.Sprintf("%x-%d-%d", sum[:6], os.Getpid(), atomic.AddUint64(&brokeredImageSeq, 1))
	return registryHost + "/" + registryfixture.RepositoryPrefix + "/" + name + ":" + tag
}

func buildBrokeredImage(ctx context.Context, bytecode, imageRef string) error {
	args := renderBrokeredImageBuildArgs(bytecode, imageRef)
	cmd, cancellationErr := newBPFManCommand(ctx, args...)
	out, err := cmd.CombinedOutput()
	if cancelErr := cancellationErr(); cancelErr != nil {
		return cancelErr
	}

	if err != nil {
		return fmt.Errorf("build bytecode image %s from %s: %w\n%s", imageRef, bytecode, err, strings.TrimSpace(string(out)))
	}
	return nil
}
