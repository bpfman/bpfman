package builtins

import (
	iofs "io/fs"
	"os"
	"os/exec"
	goruntime "runtime"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/driver"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/runtime"
)

func processCtx(t *testing.T, args ...runtime.Arg) driver.Ctx {
	t.Helper()
	return driver.Ctx{Ctx: t.Context(), Args: args}
}

func childrenOf(t *testing.T, pid int) (int, []int) {
	t.Helper()
	v, err := handleProcess(processCtx(t,
		runtime.WordArg{Text: "children"},
		runtime.WordArg{Text: strconv.Itoa(pid)}))
	require.NoError(t, err)

	countV, err := v.LookupValue("r", "count")
	require.NoError(t, err)
	countS, err := countV.Scalar()
	require.NoError(t, err)
	count, err := strconv.Atoi(countS)
	require.NoError(t, err)

	pidsV, err := v.LookupValue("r", "pids")
	require.NoError(t, err)
	raw, ok := pidsV.Raw().([]any)
	require.True(t, ok, "pids must be a list")
	require.Len(t, raw, count, "count must equal len(pids)")
	pids := make([]int, 0, len(raw))
	for _, p := range raw {
		n, err := strconv.Atoi(p.(interface{ String() string }).String())
		require.NoError(t, err, "every pid must be numeric")
		pids = append(pids, n)
	}
	return count, pids
}

// ppidOf reads the parent pid of a process from /proc/<pid>/status.
func ppidOf(t *testing.T, pid int) int {
	t.Helper()
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/status")
	require.NoError(t, err)
	for line := range strings.SplitSeq(string(data), "\n") {
		if rest, ok := strings.CutPrefix(line, "PPid:"); ok {
			ppid, err := strconv.Atoi(strings.TrimSpace(rest))
			require.NoError(t, err)
			return ppid
		}
	}
	t.Fatalf("no PPid line in /proc/%d/status", pid)
	return 0
}

func TestProcessChildren_ObservesSpawnedChild(t *testing.T) {
	t.Parallel()

	cmd := exec.Command("sleep", "30")
	require.NoError(t, cmd.Start())
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })

	// The spawned sleep has no children of its own.
	count, pids := childrenOf(t, cmd.Process.Pid)
	assert.Equal(t, 0, count, "a plain sleep has no children")
	assert.Empty(t, pids)
}

func TestProcessChildren_ReturnsTheForkedWorkerPid(t *testing.T) {
	t.Parallel()

	// `sh -c 'sleep 30 & wait'` keeps sh alive as the parent of the
	// forked sleep, mirroring the unshare --fork launcher shape.
	cmd := exec.Command("sh", "-c", "sleep 30 & wait")
	require.NoError(t, cmd.Start())
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })

	var worker int
	require.Eventually(t, func() bool {
		count, pids := childrenOf(t, cmd.Process.Pid)
		if count != 1 {
			return false
		}
		worker = pids[0]
		return true
	}, 5*time.Second, 10*time.Millisecond, "sh should report exactly one forked child")

	// The returned pid names a live process whose parent really is
	// the launcher.
	assert.Equal(t, cmd.Process.Pid, ppidOf(t, worker), "returned pid must be a direct child of the launcher")
}

// TestProcessChildren_AggregatesAcrossThreads pins the process-level
// contract deterministically: the child is forked from a thread that
// is provably not the thread-group leader, so it lands in a
// non-leader task's children file and a leader-only implementation
// would miss it.
func TestProcessChildren_AggregatesAcrossThreads(t *testing.T) {
	t.Parallel()

	cmd := startFromNonLeaderThread(t)
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })

	// The child appears in the aggregated children only once the kernel
	// has updated the forking (non-leader) task's children file, which
	// lags cmd.Start(); poll rather than read once, mirroring
	// TestProcessChildren_ReturnsTheForkedWorkerPid.
	require.Eventually(t, func() bool {
		_, pids := childrenOf(t, os.Getpid())
		return slices.Contains(pids, cmd.Process.Pid)
	}, 5*time.Second, 10*time.Millisecond,
		"a child forked from a non-leader thread must be observed")
}

// startFromNonLeaderThread starts `sleep 30` from a goroutine locked
// to an OS thread whose tid differs from the process id, so the
// kernel records the child against a non-leader task. Locked threads
// are held until the test ends, so successive attempts cannot land on
// the same thread and the loop terminates almost immediately.
func startFromNonLeaderThread(t *testing.T) *exec.Cmd {
	t.Helper()
	for range 8 {
		type result struct {
			cmd *exec.Cmd
			err error
		}
		ch := make(chan result, 1)
		release := make(chan struct{})
		go func() {
			goruntime.LockOSThread()
			if syscall.Gettid() == os.Getpid() {
				ch <- result{}
				<-release // hold the leader thread so the retry gets another
				return
			}
			cmd := exec.Command("sleep", "30")
			ch <- result{cmd: cmd, err: cmd.Start()}
		}()
		r := <-ch
		t.Cleanup(func() { close(release) })
		if r.cmd == nil && r.err == nil {
			continue // goroutine landed on the leader thread; retry
		}
		require.NoError(t, r.err)
		return r.cmd
	}
	t.Fatal("could not obtain a non-leader thread")
	return nil
}

func TestProcessChildren_MissingProcessFails(t *testing.T) {
	t.Parallel()

	_, err := handleProcess(processCtx(t,
		runtime.WordArg{Text: "children"},
		runtime.WordArg{Text: "999999999"})) // deliberately absurd; pids cannot reach this
	require.Error(t, err)
}

func TestProcessChildren_BadArgsFail(t *testing.T) {
	t.Parallel()

	_, err := handleProcess(processCtx(t, runtime.WordArg{Text: "children"}))
	require.Error(t, err, "missing pid must fail")

	_, err = handleProcess(processCtx(t,
		runtime.WordArg{Text: "children"}, runtime.WordArg{Text: "notapid"}))
	require.Error(t, err, "non-numeric pid must fail")

	for _, bad := range []string{"0", "-1"} {
		_, err = handleProcess(processCtx(t,
			runtime.WordArg{Text: "children"}, runtime.WordArg{Text: bad}))
		require.Error(t, err, "non-positive pid %s must fail", bad)
	}

	_, err = handleProcess(processCtx(t, runtime.WordArg{Text: "parents"}))
	require.Error(t, err, "unknown subcommand must fail")
}

// fakeProcFS is the in-memory interpretation of procFS for fault
// injection: each map entry either provides content or forces an
// exact error, so every policy branch of procChildren runs
// deterministically.
type fakeProcFS struct {
	dirs    map[string][]string // dir -> entry names; absent dir reads as ENOENT
	files   map[string]string   // path -> content; absent path reads as ENOENT
	fileErr map[string]error    // path -> forced readFile error
	statErr map[string]error    // path -> forced stat error
}

type fakeDirEntry struct{ name string }

func (e fakeDirEntry) Name() string                 { return e.name }
func (e fakeDirEntry) IsDir() bool                  { return true }
func (e fakeDirEntry) Type() iofs.FileMode          { return iofs.ModeDir }
func (e fakeDirEntry) Info() (iofs.FileInfo, error) { return nil, nil }

func (f fakeProcFS) readDir(name string) ([]os.DirEntry, error) {
	names, ok := f.dirs[name]
	if !ok {
		return nil, iofs.ErrNotExist
	}
	entries := make([]os.DirEntry, 0, len(names))
	for _, n := range names {
		entries = append(entries, fakeDirEntry{name: n})
	}
	return entries, nil
}

func (f fakeProcFS) readFile(name string) ([]byte, error) {
	if err := f.fileErr[name]; err != nil {
		return nil, err
	}
	content, ok := f.files[name]
	if !ok {
		return nil, iofs.ErrNotExist
	}
	return []byte(content), nil
}

func (f fakeProcFS) stat(name string) (os.FileInfo, error) {
	if err := f.statErr[name]; err != nil {
		return nil, err
	}
	return nil, nil
}

func TestProcChildren_AggregatesAndSorts(t *testing.T) {
	t.Parallel()

	fs := fakeProcFS{
		dirs: map[string][]string{"/proc/100/task": {"100", "101"}},
		files: map[string]string{
			"/proc/100/task/100/children": "301 105 ",
			"/proc/100/task/101/children": "203",
		},
	}
	children, err := procChildren(fs, "/proc", 100)
	require.NoError(t, err)
	assert.Equal(t, []int{105, 203, 301}, children, "children from every task, ascending")
}

func TestProcChildren_VanishedTaskIsSkipped(t *testing.T) {
	t.Parallel()

	// Task 100 vanished between the listing and its read -- its
	// children file is gone and so is the task directory itself;
	// task 101 reads fine. The observation succeeds with the
	// readable half.
	fs := fakeProcFS{
		dirs:    map[string][]string{"/proc/100/task": {"100", "101"}},
		files:   map[string]string{"/proc/100/task/101/children": "203"},
		statErr: map[string]error{"/proc/100/task/100": iofs.ErrNotExist},
	}
	children, err := procChildren(fs, "/proc", 100)
	require.NoError(t, err)
	assert.Equal(t, []int{203}, children)
}

func TestProcChildren_AllTasksVanishedButAliveIsZero(t *testing.T) {
	t.Parallel()

	// Every listed task vanished (each task directory confirmed
	// gone) but the task dir still stats: a momentarily
	// quiescent-but-alive process observes as zero children, not as
	// an error.
	fs := fakeProcFS{
		dirs: map[string][]string{"/proc/100/task": {"100", "101"}},
		statErr: map[string]error{
			"/proc/100/task/100": iofs.ErrNotExist,
			"/proc/100/task/101": iofs.ErrNotExist,
		},
	}
	children, err := procChildren(fs, "/proc", 100)
	require.NoError(t, err)
	assert.Empty(t, children)
}

func TestProcChildren_AllTasksVanishedAndDirGoneFails(t *testing.T) {
	t.Parallel()

	// The process exited mid-scan: tasks were listed, every read hit
	// ENOENT, and the task dir itself is gone. That is a failed
	// observation, not zero children.
	fs := fakeProcFS{
		dirs: map[string][]string{"/proc/100/task": {"100"}},
		statErr: map[string]error{
			"/proc/100/task/100": iofs.ErrNotExist,
			"/proc/100/task":     iofs.ErrNotExist,
		},
	}
	_, err := procChildren(fs, "/proc", 100)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "vanished mid-scan")
}

func TestProcChildren_AllTasksVanishedStatFailureIsReportedAsStat(t *testing.T) {
	t.Parallel()

	// A non-ENOENT stat failure must not be mislabelled as the
	// process vanishing.
	fs := fakeProcFS{
		dirs: map[string][]string{"/proc/100/task": {"100"}},
		statErr: map[string]error{
			"/proc/100/task/100": iofs.ErrNotExist,
			"/proc/100/task":     iofs.ErrPermission,
		},
	}
	_, err := procChildren(fs, "/proc", 100)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "stat /proc/100/task")
	assert.NotContains(t, err.Error(), "vanished")
}

// TestProcChildren_ChildrenFileMissingForLiveTaskFails pins the
// contract on kernels without CONFIG_PROC_CHILDREN: the task is
// alive but its children file does not exist, which is a failed
// observation, never zero children.
func TestProcChildren_ChildrenFileMissingForLiveTaskFails(t *testing.T) {
	t.Parallel()

	fs := fakeProcFS{
		dirs: map[string][]string{"/proc/100/task": {"100"}},
	}
	_, err := procChildren(fs, "/proc", 100)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "children file missing for live task")
}

// TestProcChildren_TaskStatFailureIsReportedAsStat covers the
// remaining disambiguation branch: the children file is ENOENT and
// the task's own stat fails with something other than ENOENT.
func TestProcChildren_TaskStatFailureIsReportedAsStat(t *testing.T) {
	t.Parallel()

	fs := fakeProcFS{
		dirs:    map[string][]string{"/proc/100/task": {"100"}},
		statErr: map[string]error{"/proc/100/task/100": iofs.ErrPermission},
	}
	_, err := procChildren(fs, "/proc", 100)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "stat /proc/100/task/100")
}

func TestProcChildren_NonENOENTReadFails(t *testing.T) {
	t.Parallel()

	// EACCES on one task's children file is a failed observation and
	// must surface rather than being skipped as a vanished task.
	fs := fakeProcFS{
		dirs:    map[string][]string{"/proc/100/task": {"100", "101"}},
		files:   map[string]string{"/proc/100/task/101/children": "203"},
		fileErr: map[string]error{"/proc/100/task/100/children": iofs.ErrPermission},
	}
	_, err := procChildren(fs, "/proc", 100)
	require.Error(t, err, "a failed observation must not masquerade as no children")
}

func TestProcChildren_MalformedChildrenFileFails(t *testing.T) {
	t.Parallel()

	fs := fakeProcFS{
		dirs:  map[string][]string{"/proc/100/task": {"100"}},
		files: map[string]string{"/proc/100/task/100/children": "203 banana"},
	}
	_, err := procChildren(fs, "/proc", 100)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "malformed pid")
}

func TestProcChildren_MissingProcessFails(t *testing.T) {
	t.Parallel()

	_, err := procChildren(fakeProcFS{}, "/proc", 100)
	require.Error(t, err, "no proc entry for the pid must fail")
}
