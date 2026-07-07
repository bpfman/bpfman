package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/bpfman/bpfman/cmd/internal/runtime"
	"github.com/bpfman/bpfman/config"
	"github.com/bpfman/bpfman/internal/imagebuild"
	"github.com/bpfman/bpfman/platform"
	platformebpf "github.com/bpfman/bpfman/platform/ebpf"
	"github.com/bpfman/bpfman/platform/image/oci"
	"github.com/bpfman/bpfman/platform/image/verify"
)

// ImageCmd groups image-related subcommands.
type ImageCmd struct {
	// Build builds an OCI image holding BPF bytecode and pushes it to the
	// given registry reference.
	Build ImageBuildCmd `cmd:"" help:"Build and push an OCI image containing BPF bytecode."`

	// GenerateBuildArgs computes and prints the image build contract (the
	// labels and build arguments describing the bytecode) without building
	// or pushing anything.
	GenerateBuildArgs ImageGenerateBuildArgsCmd `cmd:"" help:"Generate OCI image build arguments for BPF bytecode."`

	// Inspect fetches an OCI bytecode image and prints its bpfman metadata
	// as JSON.
	Inspect ImageInspectCmd `cmd:"" help:"Inspect OCI image metadata for BPF bytecode."`

	// Verify checks an OCI image's signature against the configured trust
	// policy.
	Verify ImageVerifyCmd `cmd:"" help:"Verify an OCI image signature."`
}

// bytecodeInputs are the bytecode-source flags shared by the image
// build and generate-build-args commands: explicit positional inputs,
// or a cilium/ebpf project directory.
type bytecodeInputs struct {
	// Bytecode lists the bytecode inputs. A single bare BYTECODE path is a
	// host-architecture image; one or more linux/arch=BYTECODE entries are
	// a multi-architecture image. Bare and platform-mapped inputs cannot be
	// mixed, and this is mutually exclusive with --cilium-ebpf-project.
	Bytecode []string `arg:"" optional:"" name:"bytecode" placeholder:"BYTECODE" help:"Bytecode input: BYTECODE for a single host-architecture image, or linux/arch=BYTECODE for a multi-architecture image."`

	// CiliumEBPFProject points at a directory of cilium/ebpf bpf2go object
	// files to use as the bytecode source instead of explicit positional
	// inputs; supplying it together with bytecode arguments is an error.
	CiliumEBPFProject string `short:"c" name:"cilium-ebpf-project" placeholder:"DIR" help:"Directory containing cilium/ebpf bpf2go object files."`
}

// plan builds the image build plan from the bytecode inputs.
func (b bytecodeInputs) plan() (imagebuild.Plan, error) {
	return planImageBuild(b.Bytecode, b.CiliumEBPFProject)
}

// ImageBuildCmd builds and pushes an OCI bytecode image.
type ImageBuildCmd struct {
	// ImageURL is the registry reference to publish the built image to.
	ImageURL string `arg:"" name:"image" placeholder:"IMAGE" help:"Image reference to publish."`

	bytecodeInputs
}

// AllowRootless reports that the image build command may run without
// root: it only builds and pushes an image and touches no kernel or
// bpffs state, so the CLI's root requirement is waived for it.
func (c *ImageBuildCmd) AllowRootless() bool { return true }

// ImageGenerateBuildArgsCmd prints the bytecode image build contract.
type ImageGenerateBuildArgsCmd struct {
	// Output selects the rendering of the build contract: "text" (default)
	// or "json".
	Output string `short:"o" name:"output" placeholder:"FORMAT" enum:"text,json" default:"text" help:"Output format: text or json."`

	bytecodeInputs
}

// AllowRootless reports that the generate-build-args command may run
// without root: it is pure computation over the bytecode inputs and
// touches no kernel or bpffs state, so the CLI's root requirement is
// waived for it.
func (c *ImageGenerateBuildArgsCmd) AllowRootless() bool { return true }

// ImageInspectCmd inspects an OCI bytecode image.
type ImageInspectCmd struct {
	// ImageURL is the OCI image reference to fetch and inspect.
	ImageURL string `arg:"" name:"image" placeholder:"IMAGE" help:"OCI image reference to inspect."`
}

// AllowRootless reports that the image inspect command may run without
// root: it only reads remote image metadata and touches no kernel or
// bpffs state, so the CLI's root requirement is waived for it.
func (c *ImageInspectCmd) AllowRootless() bool { return true }

// Run builds the bytecode image plan from the configured inputs,
// publishes the resulting OCI image to ImageURL, and prints the pinned
// (digest-qualified) reference of the published image.
func (c *ImageBuildCmd) Run(cli *runtime.CLI, ctx context.Context) error {
	plan, err := c.plan()
	if err != nil {
		return err
	}

	logger := cli.Logger().With("tag", c.ImageURL)
	logger.Info("publishing bytecode image")
	published, err := oci.PublishBytecodeImage(ctx, c.ImageURL, plan, oci.WithPublishLogger(logger))
	if err != nil {
		return err
	}

	return cli.PrintOutf("published: %s\n", published.PinnedReference)
}

// Run computes the bytecode image build plan from the configured inputs
// and prints the build contract (labels and build arguments) in the
// format selected by --output, without building or pushing an image.
func (c *ImageGenerateBuildArgsCmd) Run(cli *runtime.CLI, ctx context.Context) error {
	plan, err := c.plan()
	if err != nil {
		return err
	}

	output, err := imagebuild.Format(plan, c.Output)
	if err != nil {
		return err
	}

	return cli.PrintOut(output)
}

// Run fetches the OCI image named by ImageURL and prints its bpfman
// bytecode metadata as indented JSON.
func (c *ImageInspectCmd) Run(cli *runtime.CLI, ctx context.Context) error {
	inspection, err := oci.InspectBytecodeImage(ctx, c.ImageURL)
	if err != nil {
		return err
	}

	output, err := json.MarshalIndent(inspection, "", "  ")
	if err != nil {
		return err
	}

	return cli.PrintOut(string(output) + "\n")
}

func planImageBuild(bytecode []string, ciliumProject string) (imagebuild.Plan, error) {
	source, err := bytecodeSource(bytecode, ciliumProject)
	if err != nil {
		return imagebuild.Plan{}, err
	}

	return imagebuild.Build(source, platformebpf.InspectBytecode)
}

func bytecodeSource(bytecode []string, ciliumProject string) (imagebuild.BytecodeSource, error) {
	if ciliumProject != "" {
		if len(bytecode) > 0 {
			return imagebuild.BytecodeSource{}, fmt.Errorf("--cilium-ebpf-project conflicts with bytecode inputs")
		}
		return platformebpf.CiliumProjectBytecodeSource(ciliumProject)
	}
	return positionalBytecodeSource(bytecode)
}

func positionalBytecodeSource(bytecode []string) (imagebuild.BytecodeSource, error) {
	var bare []string
	var mapped []imagebuild.BytecodeInput
	seenPlatforms := map[string]struct{}{}
	for _, arg := range bytecode {
		input, ok, err := parsePlatformBytecodeInput(arg)
		if err != nil {
			return imagebuild.BytecodeSource{}, err
		}

		if ok {
			if _, exists := seenPlatforms[input.Platform]; exists {
				return imagebuild.BytecodeSource{}, fmt.Errorf("platform %s specified more than once", input.Platform)
			}
			seenPlatforms[input.Platform] = struct{}{}
			mapped = append(mapped, input)
			continue
		}
		bare = append(bare, arg)
	}
	if len(mapped) > 0 {
		if len(bare) > 0 {
			return imagebuild.BytecodeSource{}, fmt.Errorf("cannot mix bare bytecode inputs with platform-mapped inputs")
		}
		return imagebuild.MultiArchSource(mapped)
	}
	if len(bare) > 1 {
		return imagebuild.BytecodeSource{}, fmt.Errorf("cannot infer OCI platforms from multiple BPF objects: BPF ELF records EM_BPF and endianness, not CPU architecture; pass one BYTECODE file, use linux/arch=BYTECODE inputs, or use --cilium-ebpf-project")
	}
	if len(bare) == 1 {
		return imagebuild.SingleFileSource(bare[0])
	}
	return imagebuild.MultiArchSource(nil)
}

func parsePlatformBytecodeInput(arg string) (imagebuild.BytecodeInput, bool, error) {
	platform, path, ok := strings.Cut(arg, "=")
	if !ok || !strings.Contains(platform, "/") {
		return imagebuild.BytecodeInput{}, false, nil
	}
	if path == "" {
		return imagebuild.BytecodeInput{}, false, fmt.Errorf("bytecode path is required for %s", platform)
	}
	input, ok := imagebuild.BytecodeInputForPlatform(platform, path)
	if !ok {
		return imagebuild.BytecodeInput{}, false, fmt.Errorf("unsupported OCI platform %q; supported platforms: %s", platform, strings.Join(imagebuild.SupportedPlatforms(), ", "))
	}
	return input, true, nil
}

// ImageVerifyCmd verifies the signature of an OCI image.
type ImageVerifyCmd struct {
	// ImageURL is the OCI image reference whose signature is verified.
	ImageURL string `arg:"" name:"image" placeholder:"IMAGE" help:"OCI image reference (e.g., quay.io/bpfman-bytecode/xdp_pass:latest)."`

	// AllowUnsigned, when set, overrides the config file's allow-unsigned
	// setting so an unsigned image is accepted by policy rather than
	// rejected. Nil leaves the configured value untouched.
	AllowUnsigned *bool `name:"allow-unsigned" help:"Allow unsigned images (overrides config file)."`

	// CertificateIdentity overrides the expected signing certificate
	// identity from the config file's trusted identities. Mutually
	// exclusive with CertificateIdentityRegexp.
	CertificateIdentity *string `name:"certificate-identity" help:"Expected signing certificate identity (overrides config file)."`

	// CertificateOIDCIssuer overrides the expected signing certificate
	// OIDC issuer from the config file's trusted identities. Mutually
	// exclusive with CertificateOIDCIssuerRegexp.
	CertificateOIDCIssuer *string `name:"certificate-oidc-issuer" help:"Expected signing certificate OIDC issuer (overrides config file)."`

	// CertificateIdentityRegexp overrides the expected signing certificate
	// identity with a regular expression. Mutually exclusive with
	// CertificateIdentity.
	CertificateIdentityRegexp *string `name:"certificate-identity-regexp" help:"Expected signing certificate identity regexp (overrides config file)."`

	// CertificateOIDCIssuerRegexp overrides the expected signing
	// certificate OIDC issuer with a regular expression. Mutually
	// exclusive with CertificateOIDCIssuer.
	CertificateOIDCIssuerRegexp *string `name:"certificate-oidc-issuer-regexp" help:"Expected signing certificate OIDC issuer regexp (overrides config file)."`

	// RegistryAuth carries base64-encoded "username:password" registry
	// credentials for pulling the image manifest. Prefer the
	// BPFMAN_REGISTRY_AUTH environment variable so the credentials do not
	// appear in process listings.
	RegistryAuth string `name:"registry-auth" env:"BPFMAN_REGISTRY_AUTH" help:"Base64-encoded registry auth (username:password). Prefer BPFMAN_REGISTRY_AUTH env var to avoid exposing credentials in process listings."`
}

// AllowRootless reports that the image verify command may run without
// root: it only fetches and checks a signature and touches no kernel or
// bpffs state, so the CLI's root requirement is waived for it.
func (c *ImageVerifyCmd) AllowRootless() bool { return true }

// Run loads the signing configuration, applies the command's CLI
// overrides (allow-unsigned and the certificate-identity/issuer trusted
// identity), forces verification on, verifies the signature of the image
// named by ImageURL, and prints the resulting verification status.
func (c *ImageVerifyCmd) Run(cli *runtime.CLI, ctx context.Context) error {
	logger := cli.Logger()
	logger.Info("verifying image signature", "image", c.ImageURL)

	// Load configuration from the shared --config flag (or its default).
	cfg, err := cli.LoadConfig()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// Apply CLI overrides
	if c.AllowUnsigned != nil {
		cfg.Signing.AllowUnsigned = *c.AllowUnsigned
	}
	if c.CertificateIdentity != nil && c.CertificateIdentityRegexp != nil {
		return fmt.Errorf("--certificate-identity and --certificate-identity-regexp are mutually exclusive")
	}
	if c.CertificateOIDCIssuer != nil && c.CertificateOIDCIssuerRegexp != nil {
		return fmt.Errorf("--certificate-oidc-issuer and --certificate-oidc-issuer-regexp are mutually exclusive")
	}
	registryAuth, err := registryAuthFromFlag(c.RegistryAuth)
	if err != nil {
		return err
	}

	override := config.TrustedIdentityConfig{}
	overrideGiven := false
	if c.CertificateIdentity != nil {
		override.CertificateIdentity = *c.CertificateIdentity
		overrideGiven = true
	}
	if c.CertificateOIDCIssuer != nil {
		override.CertificateOIDCIssuer = *c.CertificateOIDCIssuer
		overrideGiven = true
	}
	if c.CertificateIdentityRegexp != nil {
		override.CertificateIdentityRegexp = *c.CertificateIdentityRegexp
		overrideGiven = true
	}
	if c.CertificateOIDCIssuerRegexp != nil {
		override.CertificateOIDCIssuerRegexp = *c.CertificateOIDCIssuerRegexp
		overrideGiven = true
	}
	if overrideGiven {
		cfg.Signing.TrustedIdentities = []config.TrustedIdentityConfig{override}
	}

	// For verify command, always enable verification (that's the point)
	cfg.Signing.VerifyEnabled = true

	logger.Debug("signing configuration", "allow_unsigned", cfg.Signing.AllowUnsigned)

	verifier, err := verify.FromSigningConfig(cfg.Signing, logger)
	if err != nil {
		return fmt.Errorf("configure signature verifier: %w", err)
	}

	req := platform.SignatureVerificationRequest{
		ImageRef: c.ImageURL,
		Auth:     registryAuth,
	}

	verification, err := verifier.Verify(ctx, req)
	if err != nil {
		return fmt.Errorf("verification failed: %w", err)
	}

	switch verification.Status {
	case platform.SignatureVerificationVerified:
		return cli.PrintOutf("Image %s: signature verified\n", c.ImageURL)
	case platform.SignatureVerificationUnsignedAccepted:
		return cli.PrintOutf("Image %s: unsigned image accepted by policy\n", c.ImageURL)
	case platform.SignatureVerificationDisabled:
		return cli.PrintOutf("Image %s: signature verification disabled\n", c.ImageURL)
	default:
		return cli.PrintOutf("Image %s: signature policy accepted (%s)\n", c.ImageURL, verification.Status)
	}
}
