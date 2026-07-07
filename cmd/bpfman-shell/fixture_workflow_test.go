package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/driver"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/runtime"
	"github.com/bpfman/bpfman/cmd/internal/cli"
)

type fixtureWorkflowRun struct {
	session *runtime.Session
	stdout  string
	stderr  string
	err     error
}

type fixtureExpectation struct {
	StdoutLines       []string `yaml:"stdout_lines"`
	StdoutContains    []string `yaml:"stdout_contains"`
	StdoutNotContains []string `yaml:"stdout_not_contains"`
	StderrContains    []string `yaml:"stderr_contains"`
	StderrNotContains []string `yaml:"stderr_not_contains"`
	ErrIs             string   `yaml:"err_is"`
	ErrText           string   `yaml:"err_text"`
	AssertFailures    *int     `yaml:"assert_failures"`
	DeferFailures     *int     `yaml:"defer_failures"`
	JobLeaks          *int     `yaml:"job_leaks"`
	NormaliseStdout   string   `yaml:"normalise_stdout"`
	NormaliseStderr   string   `yaml:"normalise_stderr"`
}

type fixtureWorkflowCase struct {
	name               string
	fixture            string
	wantOutLines       []string
	wantOutContains    []string
	wantOutNotContains []string
	wantErrContains    []string
	wantErrNotContains []string
	wantErrText        string
	wantErrIs          error
	wantAssertFails    *int
	wantDeferFails     *int
	wantJobLeaks       *int
	normaliseStdout    func(string) string
	normaliseStderr    func(string) string
	validate           func(*testing.T, string, fixtureWorkflowRun)
}

func workflowFixtureDir(fixture string) string {
	return filepath.Join("testdata", fixture)
}

func workflowFixtureMain(fixture string) string {
	return filepath.Join(workflowFixtureDir(fixture), "main.bpfman")
}

var pollTimeoutLine = regexp.MustCompile(`timed out after [^ ]+ every [^ ]+ across \d+ attempt\(s\)`)

func normalisePollStderr(stderr string) string {
	return pollTimeoutLine.ReplaceAllString(stderr, "timed out after <timeout> every <every> across <attempts> attempt(s)")
}

func normaliseJobsListing(out string) string {
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	for i, line := range lines {
		fields := strings.Fields(line)
		if len(fields) >= 5 && fields[0] != "PID" {
			fields[0] = "<pid>"
			fields[1] = "<time>"
		}
		lines[i] = strings.Join(fields, " ")
	}
	return strings.Join(lines, "\n")
}

var workflowStdoutNormalizers = map[string]func(string) string{
	"jobs_listing": normaliseJobsListing,
}

var workflowStderrNormalizers = map[string]func(string) string{
	"poll_timeout": normalisePollStderr,
}

const fixtureWorkflowTimeout = 30 * time.Second

func runFixtureWorkflow(t *testing.T, fixture string) fixtureWorkflowRun {
	t.Helper()

	timeoutCause := fmt.Errorf("fixture %s exceeded %s: %w", fixture, fixtureWorkflowTimeout, context.DeadlineExceeded)
	ctx, cancel := context.WithTimeoutCause(t.Context(), fixtureWorkflowTimeout, timeoutCause)
	defer cancel()

	mainPath := workflowFixtureMain(fixture)
	lr, err := driver.OpenScriptReader(mainPath)
	require.NoError(t, err)
	defer lr.Close()

	var outBuf, errBuf bytes.Buffer
	cli := &cli.CLI{Out: &outBuf, Err: &errBuf}
	session := runtime.NewSession()
	// Drive poll deadlines and cadence off a fake clock so poll
	// fixtures are deterministic regardless of host load: fake time
	// advances only by the poll's own sleeps, never by command
	// execution latency. Every fixture is self-contained, so no real
	// wall-clock progress is needed for a poll to converge.
	fakeNow := time.Unix(0, 0)
	runErr := driver.Run(ctx, driver.Config{
		CLI:          cli,
		LineReader:   lr,
		Session:      session,
		File:         mainPath,
		NoCheck:      false,
		Fallback:     commandFallback,
		BindFallback: bindCommandFallback,
		MakeAssert:   makeExecAssert,
		Now:          func() time.Time { return fakeNow },
		Sleep:        func(d time.Duration) { fakeNow = fakeNow.Add(d) },
	})
	return fixtureWorkflowRun{
		session: session,
		stdout:  outBuf.String(),
		stderr:  errBuf.String(),
		err:     runErr,
	}
}

func loadFixtureExpectation(t *testing.T, fixture string) *fixtureExpectation {
	t.Helper()

	exp, err := readFixtureExpectationDir(workflowFixtureDir(fixture))
	require.NoError(t, err)
	return exp
}

func TestFixtureExpectationRejectsConflictingErrorFields(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "expect.yaml"), []byte("err_text: boom\nerr_is: silent\n"), 0o644))

	_, err := readFixtureExpectationDir(root)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "err_text and err_is are mutually exclusive")
}

func TestApplyFixtureExpectation_KeepsGoSideCounterOverrides(t *testing.T) {
	t.Parallel()

	override := 7
	tc := applyFixtureExpectation(t, fixtureWorkflowCase{
		fixture:         "assertions/predicate-failure-keeps-script-running",
		wantAssertFails: &override,
	})

	assert.Equal(t, override, wantCounter(tc.wantAssertFails))
}

func TestApplyFixtureExpectation_LoadsCounterDefaultsFromYAML(t *testing.T) {
	t.Parallel()

	tc := applyFixtureExpectation(t, fixtureWorkflowCase{
		fixture: "assertions/predicate-failure-keeps-script-running",
	})

	assert.Equal(t, 1, wantCounter(tc.wantAssertFails))
}

func readFixtureExpectationDir(root string) (*fixtureExpectation, error) {
	path := filepath.Join(root, "expect.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("behaviour fixture must declare %s: %w", path, err)
	}

	var exp fixtureExpectation
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&exp); err != nil {
		return nil, fmt.Errorf("failed to parse %s: %w", path, err)
	}

	if err := validateFixtureExpectation(path, &exp); err != nil {
		return nil, err
	}

	if err := expandFixtureExpectationDir(root, &exp); err != nil {
		return nil, err
	}
	return &exp, nil
}

func validateFixtureExpectation(path string, exp *fixtureExpectation) error {
	if exp.ErrText != "" && exp.ErrIs != "" {
		return fmt.Errorf("%s: err_text and err_is are mutually exclusive", path)
	}
	return nil
}

func expandFixtureExpectationDir(root string, exp *fixtureExpectation) error {
	entries, err := os.ReadDir(root)
	if err != nil {
		return fmt.Errorf("read fixture directory %s: %w", root, err)
	}

	replacements := []string{
		"{root}", root,
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".bpfman" {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), ".bpfman")
		replacements = append(replacements, "{"+name+"}", filepath.Join(root, entry.Name()))
	}
	replacer := strings.NewReplacer(replacements...)
	expand := func(in string) string { return replacer.Replace(in) }

	exp.StdoutLines = expandFixtureStrings(exp.StdoutLines, expand)
	exp.StdoutContains = expandFixtureStrings(exp.StdoutContains, expand)
	exp.StdoutNotContains = expandFixtureStrings(exp.StdoutNotContains, expand)
	exp.StderrContains = expandFixtureStrings(exp.StderrContains, expand)
	exp.StderrNotContains = expandFixtureStrings(exp.StderrNotContains, expand)
	exp.ErrText = expand(exp.ErrText)
	return nil
}

func expandFixtureStrings(in []string, expand func(string) string) []string {
	if in == nil {
		return nil
	}
	out := make([]string, 0, len(in))
	for _, s := range in {
		out = append(out, expand(s))
	}
	return out
}

func applyFixtureExpectation(t *testing.T, tc fixtureWorkflowCase) fixtureWorkflowCase {
	t.Helper()

	exp := loadFixtureExpectation(t, tc.fixture)
	if exp == nil {
		return tc
	}

	if tc.wantOutLines == nil && exp.StdoutLines != nil {
		tc.wantOutLines = slices.Clone(exp.StdoutLines)
	}
	if len(tc.wantOutContains) == 0 && exp.StdoutContains != nil {
		tc.wantOutContains = slices.Clone(exp.StdoutContains)
	}
	if len(tc.wantOutNotContains) == 0 && exp.StdoutNotContains != nil {
		tc.wantOutNotContains = slices.Clone(exp.StdoutNotContains)
	}
	if len(tc.wantErrContains) == 0 && exp.StderrContains != nil {
		tc.wantErrContains = slices.Clone(exp.StderrContains)
	}
	if len(tc.wantErrNotContains) == 0 && exp.StderrNotContains != nil {
		tc.wantErrNotContains = slices.Clone(exp.StderrNotContains)
	}
	if tc.wantErrText == "" && exp.ErrText != "" {
		tc.wantErrText = exp.ErrText
	}
	if tc.wantErrIs == nil && exp.ErrIs != "" {
		tc.wantErrIs = workflowFixtureErrorSentinel(t, exp.ErrIs)
	}
	if tc.wantAssertFails == nil && exp.AssertFailures != nil {
		tc.wantAssertFails = exp.AssertFailures
	}
	if tc.wantDeferFails == nil && exp.DeferFailures != nil {
		tc.wantDeferFails = exp.DeferFailures
	}
	if tc.wantJobLeaks == nil && exp.JobLeaks != nil {
		tc.wantJobLeaks = exp.JobLeaks
	}
	if tc.normaliseStdout == nil && exp.NormaliseStdout != "" {
		tc.normaliseStdout = workflowFixtureNormalizer(t, "stdout", exp.NormaliseStdout, workflowStdoutNormalizers)
	}
	if tc.normaliseStderr == nil && exp.NormaliseStderr != "" {
		tc.normaliseStderr = workflowFixtureNormalizer(t, "stderr", exp.NormaliseStderr, workflowStderrNormalizers)
	}

	return tc
}

func workflowFixtureErrorSentinel(t *testing.T, name string) error {
	t.Helper()

	switch name {
	case "silent":
		return driver.ErrSilent
	default:
		t.Fatalf("unknown fixture err_is sentinel %q", name)
		return nil
	}
}

func workflowFixtureNormalizer(t *testing.T, stream, name string, registry map[string]func(string) string) func(string) string {
	t.Helper()

	fn, ok := registry[name]
	if !ok {
		t.Fatalf("unknown %s normalizer %q", stream, name)
	}
	return fn
}

func assertFixtureWorkflowOutcome(t *testing.T, tc fixtureWorkflowCase, run fixtureWorkflowRun) {
	t.Helper()

	switch {
	case tc.wantErrText != "":
		require.EqualError(t, run.err, tc.wantErrText)
	case tc.wantErrIs != nil:
		require.ErrorIs(t, run.err, tc.wantErrIs)
	default:
		require.NoErrorf(t, run.err, "unexpected script error\nstdout:\n%s\nstderr:\n%s", run.stdout, run.stderr)
	}

	out := run.stdout
	if tc.normaliseStdout != nil {
		out = tc.normaliseStdout(out)
	}
	errOut := run.stderr
	if tc.normaliseStderr != nil {
		errOut = tc.normaliseStderr(errOut)
	}

	if tc.wantOutLines != nil {
		assert.Equal(t, tc.wantOutLines, exactOutputLines(out))
	}
	for _, want := range tc.wantOutContains {
		assert.Contains(t, out, want)
	}
	for _, want := range tc.wantOutNotContains {
		assert.NotContains(t, out, want)
	}
	for _, want := range tc.wantErrContains {
		assert.Contains(t, errOut, want)
	}
	for _, want := range tc.wantErrNotContains {
		assert.NotContains(t, errOut, want)
	}
	if tc.wantErrText == "" && tc.wantErrIs == nil && len(tc.wantErrContains) == 0 && tc.normaliseStderr == nil {
		assert.Empty(t, errOut, "successful fixtures must not write unexpected stderr; declare stderr_contains for expected stderr")
	}
	assert.Equal(t, wantCounter(tc.wantAssertFails), run.session.AssertFailures(), "assert counter mismatch")
	assert.Equal(t, wantCounter(tc.wantDeferFails), run.session.DeferFailures(), "defer counter mismatch")
	assert.Equal(t, wantCounter(tc.wantJobLeaks), run.session.JobLeaks(), "job leak counter mismatch")
	if tc.validate != nil {
		tc.validate(t, workflowFixtureDir(tc.fixture), run)
	}
}

func wantCounter(v *int) int {
	if v == nil {
		return 0
	}
	return *v
}

func assertFixtureWorkflowMatrix(t *testing.T, tc fixtureWorkflowCase) {
	t.Helper()

	tc = applyFixtureExpectation(t, tc)

	run := runFixtureWorkflow(t, tc.fixture)
	assertFixtureWorkflowOutcome(t, tc, run)
}

func exactOutputLines(out string) []string {
	if out == "" {
		return []string{}
	}
	return strings.Split(strings.TrimSuffix(out, "\n"), "\n")
}

func TestExactOutputLinesPreservesWhitespaceAndBlankLines(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []string{}, exactOutputLines(""))
	assert.Equal(t, []string{"  alpha()", "", "beta(seed)  "}, exactOutputLines("  alpha()\n\nbeta(seed)  \n"))
}
