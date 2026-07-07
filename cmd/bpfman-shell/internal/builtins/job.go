// Async job control: start / wait / kill builtins for the
// 'let job <- start COMMAND ARGS' lifecycle.
package builtins

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/driver"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/jobsig"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/runtime"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/semantics"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/syntax"
	"github.com/bpfman/bpfman/cmd/internal/cli"
	"github.com/bpfman/bpfman/internal/execcancel"
)

func init() {
	Register(driver.Builtin{
		Name:     "start",
		Handler:  handleStart,
		Category: driver.CategoryJobs,
		Usage:    "start <command> [args]",
		Summary:  "Spawn a background process; primary is a $job handle (assignable).",
		Detail: "The job runs as a process-group leader so 'kill' reaches every " +
			"descendant. Output is captured into the handle's Stdout/Stderr; " +
			"the script reads them after 'wait' returns. " +
			"start registers the job in the active scope's ledger; the entry " +
			"persists through wait, kill, and beyond until an explicit 'reap' " +
			"or the scope's leak handler at unwind. An unwaited/unkilled job " +
			"is a leak (FAIL, exit 1).",
	})
	Register(driver.Builtin{
		Name: "jobs", Handler: handleJobs,
		Category: driver.CategoryJobs,
		Usage:    "jobs",
		Summary:  "List jobs registered in the current scope.",
		Detail: "Read-only: peeking at status does not mark any job Managed. Status " +
			"buckets are running, killing (kill issued, reaper has not yet " +
			"observed exit), exited N, killed SIG.",
	})
	Register(driver.Builtin{
		Name: "reap", Handler: handleReap,
		Category: driver.CategoryJobs,
		Usage:    "reap",
		Summary:  "Drop completed jobs from the registry; running jobs are left alone.",
		Detail: "Always explicit: nothing reaps automatically when wait or kill " +
			"returns, because the script may still want to inspect $job after " +
			"the call. After 'reap', the 'jobs' listing reflects only entries " +
			"whose process is still running.",
	})
	Register(driver.Builtin{
		Name:     "kill",
		Handler:  handleKill,
		Category: driver.CategoryJobs,
		Usage:    "kill [--signal=NAME] [--grace=DUR] $job",
		Summary:  "Terminate a job. Default: SIGTERM, 2s grace, SIGKILL if still alive; blocks until reaped.",
		Detail: "The default path sends SIGTERM, waits up to --grace (default 2s), " +
			"escalates to SIGKILL if the process is still alive, and blocks " +
			"until the reaper has settled. --grace=0 sends SIGTERM and SIGKILL " +
			"back-to-back. --signal=NAME (e.g. USR1, HUP) delivers a custom " +
			"signal and returns immediately without escalation; use this for " +
			"control-flow signals, not for termination. " +
			"kill marks the job as managed but leaves the entry in the ledger; " +
			"the killed status is observable in 'jobs' until 'reap' drops it. " +
			"'defer kill $p' is the canonical async cleanup idiom.",
	})
	Register(driver.Builtin{
		Name:     "wait",
		Handler:  handleWait,
		Category: driver.CategoryJobs,
		Usage:    "wait $job",
		Summary:  "Block until the job exits; primary is the captured result (assignable).",
		Detail: "The result carries ok, exit_code, stdout, stderr, killed, signal. " +
			"A killed job that the script asked to terminate reports killed=true " +
			"with signal set; the script distinguishes 'I asked for this' from " +
			"'real failure' via $r.killed rather than $r.ok. " +
			"wait marks the job as managed but leaves the entry in the ledger so " +
			"$job stays inspectable; use 'reap' to drop completed entries.",
	})
}

// handleStart spawns a background subprocess and returns a Value
// wrapping a *runtime.Job. The command runs as a process group
// leader so 'kill' can later signal the whole group, including
// any descendants the child fork-execs. stdout and stderr are
// captured into in-memory buffers; 'wait' reads them after the
// process exits.
//
// Adapter arguments (file:$var.path) are resolved to temp files
// before the spawn, the same way driver.RunExternal handles them, but
// the temp files outlive the start call: a wait or kill
// goroutine cleans them up when the job exits, so the script
// can use the captured paths until the job is reaped.
//
// Launch failure (command not found, permission denied) is
// reported as a Go error and produces no Job; this is
// 'structural failure' in the bind-result sense and propagates
// through ExecBind to halt the script.
//
// Origin is derived from the caller's source loc so the leak-walk
// diagnostic can cite the start site even when the leak fires
// far from it.
func handleStart(c driver.Ctx) (runtime.Value, error) {
	if len(c.Args) == 0 {
		return runtime.Value{}, fmt.Errorf("start requires at least one argument")
	}

	tempFiles, resolved, err := resolveAdapterArgs("start", c.Args)
	if err != nil {
		return runtime.Value{}, err
	}

	argv := driver.ArgTexts(resolved)

	// Best-effort identity: plain start populates target_binary
	// from argv[0] so a downstream uprobe attach has the
	// executable path the user pointed at. The semantic
	// guarantee (stable image for kernel attachment) belongs only
	// to fire kinds with NeedsBinary == true; here the field is
	// just what the user asked us to launch.
	job, err := spawnJob(c.Ctx, c.Env, spawnSpec{
		Argv:         argv,
		Origin:       c.Pos.Cite(),
		TempFiles:    tempFiles,
		TargetBinary: argv[0],
	})
	if err != nil {
		return runtime.Value{}, fmt.Errorf("start %s: %w", argv[0], err)
	}
	return runtime.ValueFromJob(job), nil
}

// handleJobs lists jobs registered in the active job scope.
// Rejects extra arguments at the dispatch boundary so the
// error message is consistent with the other arg-less builtins.
func handleJobs(c driver.Ctx) (runtime.Value, error) {
	if len(c.Args) > 0 {
		return runtime.Value{}, syntax.SpanErrorf(c.Span, "jobs takes no arguments")
	}
	return runtime.Value{}, listJobs(c.CLI, c.Env)
}

// handleReap drops completed jobs from the active scope's
// registry. Pure mutation; the caller has already typed
// 'jobs' to see what is there and now wants the listing
// trimmed.
func handleReap(c driver.Ctx) (runtime.Value, error) {
	if len(c.Args) > 0 {
		return runtime.Value{}, syntax.SpanErrorf(c.Span, "reap takes no arguments")
	}
	return runtime.Value{}, reapJobs(c.Env)
}

// handleKill adapts KillEnvelope to the builtin shape, wrapping the
// returned Envelope as a Value so the bind path ('let r <-
// kill $p') receives a usable primary.
func handleKill(c driver.Ctx) (runtime.Value, error) {
	env, err := KillEnvelope(c.Ctx, c.Args)
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.ValueFromEnvelope(env), nil
}

// handleWait adapts WaitEnvelope to the builtin shape, wrapping the
// returned Envelope as a Value so the bind path ('let r <-
// wait $p') receives a usable primary.
func handleWait(c driver.Ctx) (runtime.Value, error) {
	env, err := WaitEnvelope(c.Ctx, c.Args)
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.ValueFromEnvelope(env), nil
}

// spawnSpec is the parameter pack for spawnJob. The fields are
// the small superset of what start and fire need: argv, origin,
// adapter-temp files to clean up after exit, the optional process
// environment override, and the optional target-binary path to
// publish on the resulting Job.
type spawnSpec struct {
	Argv         []string // resolved argv; Argv[0] is the executable
	Env          []string // explicit environment; nil inherits os.Environ
	Origin       string   // source citation for leak diagnostics
	TempFiles    []string // adapter temp files reaped with the process
	TargetBinary string   // value to publish on Job.target_binary; "" leaves it unset
}

// spawnJob is the shared subprocess-launch path for start and
// fire. It sets the process up as its own process-group leader,
// captures stdout/stderr into buffers, and registers a *runtime.Job
// in env's active scope. A reaper goroutine waits on the child,
// records the final Stdout/Stderr/ExitCode/Signal, cleans up the
// temp files, and closes Done.
//
// On launch failure the temp files are removed before the error
// is returned, so the caller does not have to repeat the cleanup.
func spawnJob(ctx context.Context, env *runtime.Env, spec spawnSpec) (*runtime.Job, error) {
	cmd := exec.CommandContext(ctx, spec.Argv[0], spec.Argv[1:]...)
	execcancel.Configure(cmd)

	if spec.Env != nil {
		cmd.Env = spec.Env
	}

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	if err := cmd.Start(); err != nil {
		removeTempFiles(spec.TempFiles)
		return nil, err
	}

	job := &runtime.Job{
		PID:          cmd.Process.Pid,
		Done:         make(chan struct{}),
		Args:         spec.Argv,
		Origin:       spec.Origin,
		Started:      time.Now(),
		TargetBinary: spec.TargetBinary,
	}
	if env != nil {
		env.RegisterJob(job)
	}

	// Reap the process in a goroutine. The goroutine is the sole
	// writer of Stdout/Stderr/ExitCode/Signal; close(Done) is the
	// happens-before barrier for any reader (typically wait).
	// Signal is set when the process ended via a signal (whether
	// from our kill builtin, an external sender, or a parent
	// shutdown); the kill builtin also records its requested
	// signal up-front, but the reaper's value is what actually
	// ended the process.
	go func() {
		defer close(job.Done)
		defer removeTempFiles(spec.TempFiles)
		err := cmd.Wait()
		exitCode := 0
		var sigName string
		if err != nil {
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				if ws, ok := exitErr.Sys().(syscall.WaitStatus); ok && ws.Signaled() { //nolint:misspell // syscall.WaitStatus.Signaled is a Go stdlib method name
					sig := ws.Signal()
					sigName = jobsig.ShortName(sig)
					// Shell convention: signal-killed
					// processes report code 128+signum.
					exitCode = 128 + int(sig)
				} else {
					exitCode = exitErr.ExitCode()
				}
			} else {
				// Launch failure was caught at Start();
				// anything else here is unexpected. Use -1
				// as the conventional "abnormal" sentinel.
				exitCode = -1
			}
		}
		job.Mu.Lock()
		job.Stdout = stdout.String()
		job.Stderr = stderr.String()
		job.ExitCode = exitCode
		if sigName != "" && job.Signal == "" {
			// Don't overwrite the signal recorded by the
			// kill builtin -- it is more authoritative
			// (kill's intent vs. whatever signal the
			// kernel ultimately delivered).
			job.Signal = sigName
		}
		job.Mu.Unlock()
	}()

	return job, nil
}

// resolveAdapterArgs walks args, resolving file: adapter values
// to temp files and rejecting structured-value args that cannot
// flatten into argv text. The temp files are returned to the
// caller so it can choose when to remove them; driver.RunExternal
// removes immediately after the command exits, handleStart hands
// the cleanup to the wait goroutine.
func resolveAdapterArgs(name string, args []runtime.Arg) ([]string, []runtime.Arg, error) {
	var tempFiles []string
	resolved := make([]runtime.Arg, len(args))
	for i, a := range args {
		switch aa := a.(type) {
		case runtime.AdapterArg:
			if aa.Adapter != "file" {
				removeTempFiles(tempFiles)
				return nil, nil, fmt.Errorf("unknown adapter %q", aa.Adapter)
			}
			path, err := driver.WriteValueToTemp(aa.Value)
			if err != nil {
				removeTempFiles(tempFiles)
				return nil, nil, fmt.Errorf("adapter file: %w", err)
			}
			tempFiles = append(tempFiles, path)
			resolved[i] = runtime.ScalarValueArg{Text: path, Span: aa.Span}
		case runtime.StructuredValueArg:
			removeTempFiles(tempFiles)
			return nil, nil, fmt.Errorf("%s: argument %d is a %s value; use a scalar path (e.g. $name.field) or the file adapter (file:$name)", name, i+1, aa.Value.Kind())
		default:
			resolved[i] = a
		}
	}
	return tempFiles, resolved, nil
}

// removeTempFiles removes any temp files written by adapter
// resolution. Errors are ignored: removal failures are
// non-fatal and the OS reaps stale temp files on its own
// schedule anyway.
func removeTempFiles(paths []string) {
	for _, p := range paths {
		os.Remove(p)
	}
}

// WaitEnvelope blocks until the given job's reaper goroutine has
// settled the captured streams and exit code, then builds the
// captured-result Envelope from those fields. If the job had
// already completed before wait was called the select returns
// immediately with the cached values, so a job that exited
// between 'start' and 'wait' does not lose its result.
//
// The job is marked Managed regardless of outcome: the script
// has acknowledged the lifecycle, even if the result is a
// non-ok envelope. Scope-exit uses Managed to distinguish
// observed jobs from leaked ones.
//
// A killed job reports ok: false in the envelope: ok is tied to
// exit_code == 0, and a job ended by SIGTERM exits 143, not zero.
// The script distinguishes "I asked for this" from "real failure"
// via $r.killed (paired with $r.signal), not by overloading $r.ok.
// A non-zero exit on a job the script did not kill is a failure the
// consumer can act on through guard or by inspecting $rc.exit_code.
func WaitEnvelope(ctx context.Context, args []runtime.Arg) (runtime.Envelope, error) {
	if len(args) != 1 {
		return runtime.Envelope{}, fmt.Errorf("wait requires exactly one argument: a $job")
	}
	job, err := jobFromArg(args[0])
	if err != nil {
		return runtime.Envelope{}, err
	}

	select {
	case <-job.Done:
	case <-ctx.Done():
		return runtime.Envelope{
			ExitCode: -1,
			Stderr:   ctx.Err().Error(),
		}, nil
	}
	job.MarkManaged()

	job.Mu.Lock()
	stdout := job.Stdout
	stderr := job.Stderr
	exitCode := job.ExitCode
	killed := job.Killed
	signal := job.Signal
	job.Mu.Unlock()

	// ok stays tied to "exit code 0" so the field reads
	// consistently across synchronous and asynchronous
	// commands. A killed job is typically !ok with exit_code=143
	// (SIGTERM convention) plus killed=true and signal="TERM";
	// the script distinguishes "expected termination" from
	// "real failure" via $r.killed, not by overloading $r.ok.
	return runtime.Envelope{
		ExitCode: exitCode,
		Stdout:   stdout,
		Stderr:   stderr,
		Killed:   killed,
		Signal:   signal,
	}, nil
}

// defaultKillGrace is the window kill waits between SIGTERM
// and SIGKILL on the default-termination path. Long enough for
// a cooperative SIGTERM handler to flush state; short enough
// that a hung process does not stall a script's teardown for
// an irritating amount of time. systemd defaults to 90s,
// container runtimes to 10s, but in this shell most jobs are
// ephemeral test fixtures and 2s is the right ergonomic
// default. Adjust per call with --grace=DURATION.
const defaultKillGrace = 2 * time.Second

// KillEnvelope signals the process group of the given job and, on
// the default-termination path, escalates to SIGKILL if the
// process does not exit within the grace period. Three
// behaviours, chosen by flags:
//
//	kill $job                  default termination: SIGTERM,
//	                           wait up to --grace (default 2s),
//	                           SIGKILL if still alive, block
//	                           until reaped. Returns only when
//	                           the job is no longer alive.
//	kill --grace=DUR $job      same, with a custom grace
//	                           window. --grace=0 sends SIGTERM
//	                           immediately followed by SIGKILL
//	                           with no wait.
//	kill --signal=NAME $job    custom signal: deliver and return.
//	                           No escalation. NAME accepts both
//	                           the 'SIGTERM' and 'TERM'
//	                           spellings. Used for control-flow
//	                           signals (USR1, HUP) where the
//	                           script wants to nudge a daemon,
//	                           not terminate it.
//
// The kill targets the process group (-pgid) so descendants
// the child fork-execs (an 'ip netns exec ...' wrapper, a
// sh -c spawn) receive the signal too. ESRCH (the process
// already exited) is treated as success because the desired
// state (job not running) is true.
//
// Concurrency: Killed and Signal are written before the
// initial signal goes out, so a concurrent 'wait' that races
// the kill sees the requested termination. On escalation,
// Signal is rewritten to "KILL" before SIGKILL is sent so the
// final wait envelope reflects what actually ended the
// process; the reaper's "don't overwrite a non-empty Signal"
// rule preserves whichever value the kill builtin set last.
func KillEnvelope(ctx context.Context, args []runtime.Arg) (runtime.Envelope, error) {
	sig := syscall.SIGTERM
	sigName := "TERM"
	grace := defaultKillGrace
	explicitSignal := false
	var jobArg runtime.Arg
	for _, a := range args {
		text := driver.ArgText(a)
		switch {
		case strings.HasPrefix(text, "--signal="):
			name := strings.TrimPrefix(text, "--signal=")
			s, ok := jobsig.FromName(name)
			if !ok {
				return runtime.Envelope{}, fmt.Errorf("unknown signal %q (try SIGTERM, SIGKILL, SIGINT, SIGUSR1, ...)", name)
			}
			sig = s
			sigName = jobsig.ShortName(s)
			explicitSignal = true
		case strings.HasPrefix(text, "--grace="):
			d, err := time.ParseDuration(strings.TrimPrefix(text, "--grace="))
			if err != nil {
				return runtime.Envelope{}, fmt.Errorf("--grace: %w", err)
			}
			if d < 0 {
				return runtime.Envelope{}, fmt.Errorf("--grace must not be negative (got %s)", d)
			}
			grace = d
		default:
			if jobArg != nil {
				return runtime.Envelope{}, fmt.Errorf("kill takes one $job argument; got more than one")
			}
			jobArg = a
		}
	}
	if jobArg == nil {
		return runtime.Envelope{}, fmt.Errorf("kill requires a $job argument")
	}
	job, err := jobFromArg(jobArg)
	if err != nil {
		return runtime.Envelope{}, err
	}

	// A repeated kill after the job has already finished is a
	// no-op acknowledgement, not a lifecycle rewrite.
	select {
	case <-job.Done:
		job.MarkManaged()
		return runtime.Envelope{ExitCode: 0}, nil
	default:
	}

	// Mark up-front so a concurrent wait reads "killed"
	// regardless of whether the signal has been delivered yet.
	job.Mu.Lock()
	prevKilled := job.Killed
	prevSignal := job.Signal
	job.Killed = true
	job.Signal = sigName
	job.Mu.Unlock()

	if err := syscall.Kill(-job.PID, sig); err != nil {
		if errors.Is(err, syscall.ESRCH) {
			job.Mu.Lock()
			job.Killed = prevKilled
			job.Signal = prevSignal
			job.Mu.Unlock()
			job.MarkManaged()
			return runtime.Envelope{ExitCode: 0}, nil
		}
		return runtime.Envelope{
			ExitCode: 1,
			Stderr:   fmt.Sprintf("kill -%d -%d: %v", int(sig), job.PID, err),
		}, nil
	}
	job.MarkManaged()

	// Custom signals are not termination paths: deliver and
	// return. Escalation applies only when the user accepted
	// the default (no --signal flag).
	if explicitSignal {
		return runtime.Envelope{ExitCode: 0}, nil
	}

	// Default path: wait up to grace for the process to exit,
	// escalate to SIGKILL if needed, block until the reaper
	// closes Done. 'kill' returns only after the job is
	// genuinely gone, so 'defer kill $p' is a real cleanup
	// primitive rather than a hopeful suggestion.
	//
	// --grace=0 is "no waiting between TERM and KILL": always
	// escalate to SIGKILL regardless of whether SIGTERM already
	// reaped the process. The contract for the no-wait path is
	// "this call ends on SIGKILL" (Signal == "KILL"). The
	// race-check short-circuit below belongs only on the timed
	// grace path -- a process that happened to die during the
	// wait window legitimately ends on TERM -- but on the no-
	// wait path, applying the same short-circuit means a fast
	// SIGTERM kill flakes the Signal field between TERM and
	// KILL depending on scheduler timing.
	if grace > 0 {
		if waitForDone(ctx, job, grace) {
			return runtime.Envelope{ExitCode: 0}, nil
		}
		// Race: the process might have exited at the boundary
		// of the grace window. Re-check before escalating.
		select {
		case <-job.Done:
			return runtime.Envelope{ExitCode: 0}, nil
		default:
		}
	}
	job.Mu.Lock()
	job.Signal = "KILL"
	job.Mu.Unlock()
	if err := syscall.Kill(-job.PID, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return runtime.Envelope{
			ExitCode: 1,
			Stderr:   fmt.Sprintf("kill -KILL -%d: %v", job.PID, err),
		}, nil
	}
	// SIGKILL is uncatchable; the reaper will close Done
	// almost immediately. Block on it (respecting ctx) so we
	// return only after the kernel has reaped the process.
	waitForDoneIndefinitely(ctx, job)
	return runtime.Envelope{ExitCode: 0}, nil
}

// waitForDone blocks until job.Done closes, ctx is cancelled,
// or timeout elapses. timeout == 0 means "do not wait" --
// returns true only if Done is already closed. Returns true
// when Done observed, false on timeout or ctx cancellation.
func waitForDone(ctx context.Context, job *runtime.Job, timeout time.Duration) bool {
	if timeout == 0 {
		select {
		case <-job.Done:
			return true
		default:
			return false
		}
	}
	select {
	case <-job.Done:
		return true
	case <-time.After(timeout):
		return false
	case <-ctx.Done():
		return false
	}
}

// waitForDoneIndefinitely blocks until job.Done closes or ctx
// is cancelled. Used after sending SIGKILL where the kernel
// will reap the process imminently and we just need to wait
// for the reaper goroutine to settle the captured fields.
func waitForDoneIndefinitely(ctx context.Context, job *runtime.Job) {
	select {
	case <-job.Done:
	case <-ctx.Done():
	}
}

// The strict leak-handler body lives in driver/jobs.go as
// driver.StrictJobLeakHandler.

// listJobs lists the jobs registered in the active job scope.
// Read-only: peeking at status does not mark any job Managed
// and does not move it out of the registry, so a 'jobs' call
// after a kill still shows the killed entry until the enclosing
// scope's cleanup clears it. Output is column-aligned text so a long argv does not
// shift the earlier columns; each row is one job in
// registration order.
func listJobs(cli *cli.CLI, env *runtime.Env) error {
	jobs := env.ActiveJobs()
	if len(jobs) == 0 {
		return nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%-7s %-8s %-12s %-22s %s\n", "PID", "START", "STATUS", "ORIGIN", "ARGV")
	for _, j := range jobs {
		origin := j.Origin
		if origin == "" {
			origin = "<stdin>"
		}
		argv := strings.Join(j.Args, " ")
		fmt.Fprintf(&b, "%-7d %-8s %-12s %-22s %s\n", j.PID, j.Started.Format("15:04:05"), jobStatus(j), origin, argv)
	}
	return cli.PrintOut(b.String())
}

// reapJobs drops every job from the active job scope's
// registry whose Done channel has closed. Always explicit:
// the user invokes 'reap' when they want the listing
// trimmed; nothing happens automatically when a wait or kill
// returns, because the script may still want to inspect
// $job after observing its outcome. Running jobs are left
// alone. Returns no output: success is silent (Unix
// contract) and 'jobs' afterwards reflects the trimmed
// registry.
func reapJobs(env *runtime.Env) error {
	if env == nil {
		return fmt.Errorf("reap requires an active shell environment")
	}
	env.ReapJobs(func(j *runtime.Job) bool {
		select {
		case <-j.Done:
			return true
		default:
			return false
		}
	})
	return nil
}

// jobStatus reports the lifecycle stage of a Job for the 'jobs'
// listing. Four buckets:
//
//	running   - process still alive, no kill issued.
//	killing   - kill builtin has signalled but the reaper has
//	            not yet observed exit (Done open, Killed true).
//	exited N  - process exited with the given status code.
//	killed S  - process ended on signal S (whether from our
//	            kill builtin or an external sender).
//
// jobStatus does not block: it peeks Done with a non-blocking
// select so listing is O(jobs) and never waits for an
// in-flight process to die.
func jobStatus(j *runtime.Job) string {
	select {
	case <-j.Done:
		// Process has been reaped; fields below are stable.
	default:
		j.Mu.Lock()
		killed := j.Killed
		j.Mu.Unlock()
		if killed {
			return "killing"
		}
		return "running"
	}
	j.Mu.Lock()
	signal := j.Signal
	exitCode := j.ExitCode
	j.Mu.Unlock()
	if signal != "" {
		return fmt.Sprintf("killed %s", signal)
	}
	return fmt.Sprintf("exited %d", exitCode)
}

// jobFromArg unwraps the StructuredValueArg representing a
// $job reference and returns the underlying *runtime.Job. Any
// other Arg shape, or a structured value whose origin is not a
// Job, fails with a message that names the offending kind so
// the user can correct the call site.
func jobFromArg(a runtime.Arg) (*runtime.Job, error) {
	sva, ok := a.(runtime.StructuredValueArg)
	if !ok {
		return nil, fmt.Errorf("expected a $job argument, got %T", a)
	}
	if sva.Value.Kind() != semantics.OriginJob {
		return nil, fmt.Errorf("expected a $job argument, got a %s value", sva.Value.Kind())
	}
	job, ok := sva.Value.Origin().(*runtime.Job)
	if !ok {
		return nil, fmt.Errorf("$job has no underlying job handle (got %T)", sva.Value.Origin())
	}
	return job, nil
}
