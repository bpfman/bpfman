// registry exposes the e2e image registry fixture to scripts. It only
// fabricates references and reports the backing endpoint; real image
// operations stay on the production bpfman image command path.
package builtins

import (
	"fmt"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/driver"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/runtime"
	"github.com/bpfman/bpfman/internal/registryfixture"
)

func init() {
	Register(driver.Builtin{
		Name:     "registry",
		Handler:  handleRegistry,
		Category: driver.CategoryIO,
		Usage:    "registry ref NAME  |  registry host  |  registry url",
		Summary:  "E2E image registry fixture helpers.",
		Detail: "registry is the e2e image-registry fixture surface. ref mints " +
			"a unique alias reference under bpfman-e2e-registry.example.com " +
			"without publishing anything; pass that reference to real " +
			"'bpfman image build' and 'bpfman program load image' commands. " +
			"host and url expose the anonymous backing registry, using " +
			"BPFMAN_E2E_IMAGE_REGISTRY when set and otherwise starting a " +
			"process-local loopback registry lazily.",
	})
}

func handleRegistry(c driver.Ctx) (runtime.Value, error) {
	if len(c.Args) == 0 {
		return runtime.Value{}, fmt.Errorf("registry: subcommand required (valid: ref, host, url)")
	}
	sub := driver.ArgText(c.Args[0])
	rest := c.Args[1:]
	switch sub {
	case "ref":
		return handleRegistryRef(rest)
	case "host":
		return handleRegistryHost(rest)
	case "url":
		return handleRegistryURL(rest)
	default:
		return runtime.Value{}, fmt.Errorf("registry: unknown subcommand %q (valid: ref, host, url)", sub)
	}
}

func handleRegistryRef(args []runtime.Arg) (runtime.Value, error) {
	if len(args) != 1 {
		return runtime.Value{}, fmt.Errorf("registry ref: requires exactly one NAME argument")
	}
	ref, err := registryfixture.Ref(driver.ArgText(args[0]))
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.StringValue(ref), nil
}

func handleRegistryHost(args []runtime.Arg) (runtime.Value, error) {
	if len(args) != 0 {
		return runtime.Value{}, fmt.Errorf("registry host: takes no arguments")
	}
	host, err := registryfixture.Host()
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.StringValue(host), nil
}

func handleRegistryURL(args []runtime.Arg) (runtime.Value, error) {
	if len(args) != 0 {
		return runtime.Value{}, fmt.Errorf("registry url: takes no arguments")
	}
	url, err := registryfixture.URL()
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.StringValue(url), nil
}
