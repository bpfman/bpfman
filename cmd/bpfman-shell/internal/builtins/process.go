// process is the e2e built-in for observing processes the shell does
// not own. Job control (start/wait/kill) covers processes the shell
// launched; `process children` answers questions about the process
// tree around them -- the canonical case being a worker forked by an
// `unshare --pid --fork` launcher, where the script holds the
// launcher's pid but bpfman needs the worker's.
package builtins

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/driver"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/runtime"
)

func init() {
	Register(driver.Builtin{
		Name:     "process",
		Handler:  handleProcess,
		Category: driver.CategoryJobs,
		Usage:    "process children <pid>",
		Summary:  "Observe the direct children of a process.",
		Detail: "process children returns {count, pids} for the direct " +
			"children of <pid>, aggregated across /proc/<pid>/task/*/children " +
			"so children forked from any thread are observed; pids are " +
			"reported in ascending order. " +
			"count=0 is a successful observation, not an error, so a poll " +
			"can retry on the count while the child is still being forked " +
			"(`let r <- ...` wraps the result in the outcome envelope, so " +
			"the poll form is `unless $r.value.count > 0`; a guard bind " +
			"exposes $r.count directly); the call fails when <pid> does not " +
			"exist or procfs cannot provide the observation. One " +
			"observation per call: timing policy belongs to the " +
			"surrounding poll.",
	})
}

func handleProcess(c driver.Ctx) (runtime.Value, error) {
	if len(c.Args) == 0 {
		return runtime.Value{}, fmt.Errorf("process: subcommand required (valid: children)")
	}
	sub := driver.ArgText(c.Args[0])
	rest := c.Args[1:]
	switch sub {
	case "children":
		return handleProcessChildren(rest)
	default:
		return runtime.Value{}, fmt.Errorf("process: unknown subcommand %q (valid: children)", sub)
	}
}

func handleProcessChildren(args []runtime.Arg) (runtime.Value, error) {
	if len(args) != 1 {
		return runtime.Value{}, fmt.Errorf("process children: requires exactly one <pid>")
	}
	pid, err := strconv.Atoi(driver.ArgText(args[0]))
	if err != nil {
		return runtime.Value{}, fmt.Errorf("process children: pid: %w", err)
	}

	if pid <= 0 {
		return runtime.Value{}, fmt.Errorf("process children: pid must be positive (got %d)", pid)
	}

	children, err := procChildren(osProcFS{}, "/proc", pid)
	if err != nil {
		return runtime.Value{}, fmt.Errorf("process children: %w", err)
	}

	pids := make([]any, 0, len(children))
	for _, child := range children {
		pids = append(pids, json.Number(strconv.Itoa(child)))
	}

	return runtime.ValueFromMap(map[string]any{
		"count": json.Number(strconv.Itoa(len(pids))),
		"pids":  pids,
	}), nil
}

// procFS is the filesystem effect surface procChildren needs. The
// interface exists so the scan's policy -- which failures are races
// to skip, which are failed observations -- is testable with exact
// fault injection; osProcFS is the real interpretation.
type procFS interface {
	readDir(name string) ([]os.DirEntry, error)
	readFile(name string) ([]byte, error)
	stat(name string) (os.FileInfo, error)
}

type osProcFS struct{}

func (osProcFS) readDir(name string) ([]os.DirEntry, error) { return os.ReadDir(name) }
func (osProcFS) readFile(name string) ([]byte, error)       { return os.ReadFile(name) }
func (osProcFS) stat(name string) (os.FileInfo, error)      { return os.Stat(name) }

// procChildren returns the direct children of pid in ascending order,
// observed under procRoot via fs.
//
// Each task's children file lists the children that task forked,
// space separated, so process-level children are the aggregate over
// {procRoot}/{pid}/task/*/children -- a multi-threaded parent (any Go
// program, for one) parents children to whichever thread called fork,
// not necessarily the leader. The files exist on kernels built with
// CONFIG_PROC_CHILDREN (everything this project targets); an
// unreadable task directory means the process is gone, which is an
// error, unlike "no children yet". A task vanishing mid-scan is just
// the tree changing under observation and is skipped, but only when
// the task directory itself is confirmed gone: a children file
// missing for a live task means this kernel cannot provide the
// observation at all. Neither that nor any other read failure may
// masquerade as "no children".
func procChildren(fs procFS, procRoot string, pid int) ([]int, error) {
	taskDir := filepath.Join(procRoot, strconv.Itoa(pid), "task")
	tasks, err := fs.readDir(taskDir)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", taskDir, err)
	}

	var children []int
	readTasks := 0
	for _, task := range tasks {
		taskPath := filepath.Join(taskDir, task.Name())
		path := filepath.Join(taskPath, "children")
		data, err := fs.readFile(path)
		if errors.Is(err, os.ErrNotExist) {
			if _, statErr := fs.stat(taskPath); errors.Is(statErr, os.ErrNotExist) {
				continue // task vanished mid-scan
			} else if statErr != nil {
				return nil, fmt.Errorf("stat %s: %w", taskPath, statErr)
			}
			return nil, fmt.Errorf("read %s: children file missing for live task", path)
		}
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		readTasks++
		for f := range strings.FieldsSeq(string(data)) {
			child, err := strconv.Atoi(f)
			if err != nil {
				return nil, fmt.Errorf("malformed pid %q in %s", f, path)
			}
			children = append(children, child)
		}
	}
	// Every task vanishing between the directory listing and the
	// per-task reads usually means the process exited mid-scan;
	// distinguish that from a momentarily empty-but-alive process so
	// "process gone" stays an error rather than a zero observation.
	if len(tasks) > 0 && readTasks == 0 {
		if _, err := fs.stat(taskDir); errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%s vanished mid-scan: %w", taskDir, err)
		} else if err != nil {
			return nil, fmt.Errorf("stat %s: %w", taskDir, err)
		}
	}

	sort.Ints(children)
	return children, nil
}
