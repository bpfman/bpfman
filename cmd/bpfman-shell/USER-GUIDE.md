# bpfman-shell User Guide

`bpfman-shell` is a script runner for bpfman development, tests, and
operations. It is deliberately close to shell command syntax: copied
`bpfman ...`, `bpftool ...`, `ip ...`, and `sh ...` commands should usually
need little rewriting. The language adds structured binding, guards,
assertions, polling, cleanup, imports, and small pure helpers around that
command-first model.

## Running And Checking Scripts

Run a script by passing its path:

```sh
bpfman-shell e2e/scripts/TestXDP_LinkRoundTrip.bpfman
```

Many bpfman and network scripts need root because they load BPF programs or
create network devices:

```sh
sudo bpfman-shell examples/xdp.bpfman
```

Use parse-only modes before running a script against real system state:

```sh
bpfman-shell --check path/to/script.bpfman
bpfman-shell --fmt path/to/script.bpfman
bpfman-shell --fmt -w path/to/script.bpfman
bpfman-shell --lowered path/to/script.bpfman
bpfman-shell --symbols path/to/script.bpfman
```

`--check` parses and runs the static pre-flight without evaluating commands.
Normal execution also runs that pre-flight first unless `--no-check` is used.
`--trace` traces each statement to stderr; scripts can also toggle tracing
with `trace on` and `trace off`.

`-C DIR` changes directory before opening the script, resolving imports, or
running commands. With no script, or with `-`, `bpfman-shell` reads one whole
program from stdin.

Script metadata labels can be listed without running scripts:

```sh
bpfman-shell --list-scripts --selector 'program in (tc,xdp),external' e2e/scripts
```

## Mental Model

A statement is either a bpfman-shell form or a command. Reserved words such as
`let`, `guard`, `foreach`, `if`, `poll`, `defer`, `def`, `return`, `assert`,
and `require` start language forms. Most other leading words are command
heads.

Commands run through this order:

1. User-defined `def`s.
2. Builtins such as `bpfman`, `exec`, `start`, `wait`, `kill`, `jobs`,
   `net`, `tempdir`, `file`, `fire`, `process`, `print`, `jq`, `range`,
   and `zip`, plus the session-introspection builtins `defs` and
   `registry`.
3. External process execution for unknown command heads.

This means a `def` named `load` would shadow an external `load` command at
command position. Prefer explicit, descriptive def names.

Whitespace matters at binding and expression sites. Write:

```bpfman
let count = 3
guard loaded <- bpfman program load file prog.o --programs xdp:pass
assert $count > 0
```

Do not rely on glued forms such as `let count=3`, `let x<-cmd`, or
`$count>0`; command-like text is preserved as words unless the parser is in a
documented expression or binding position.

## Commands, Bind, And Guard

Bare commands inherit stdout/stderr and fail the script if the external
process exits non-zero:

```bpfman
print "starting"
bpfman program list
```

`let NAME <- CMD` captures the result envelope instead of halting on a non-ok
command result:

```bpfman
let probe <- exec test -f /tmp/ready
if $probe.ok {
    print "ready"
} else {
    print "missing: exit=${probe.exit_code}"
}
```

The envelope has fields such as `ok`, `exit_code`, `stdout`, and `stderr` for
ordinary command capture. Some builtins also return a primary structured value.

`guard NAME <- CMD` is the success path. It requires an ok result and binds the
primary value directly:

```bpfman
guard loaded <- bpfman program load file \
    e2e/testdata/bpf/xdp_pass.bpf.o \
    --programs xdp:pass
let prog = $loaded.programs[0]

guard link <- bpfman link attach xdp --iface eth0 $prog --priority 50
```

Use `_` to discard a value when the command is only for its side effect:

```bpfman
guard _ <- ip link set bpfman-xdp up
```

If you accidentally omit the `bpfman` prefix from a domain command, the shell
reports that domain commands require the prefix rather than treating `program`
or `link` as ordinary external commands.

## Variables And Expressions

`let NAME = EXPR` binds expression values:

```bpfman
let pid = $prog.record.program_id
let ids = [1 2 3]
let label = "program=${pid}"
```

Variable paths use dotted fields and simple indexes:

```bpfman
let first = $loaded.programs[0]
let mapID = $first.status.maps[0].id
```

Expressions support booleans, comparisons, arithmetic, lists, parentheses,
threading, and pure calls:

```bpfman
let spec = jq "." "{\"names\":[\"alpha\",\"beta\",\"gamma\"]}"

foreach (idx name) in (zip (range 3) ($spec |> jq ".names")) {
    print "${idx}-${name}"
}

assert ($mapID + 1) > $mapID
```

The thread operator passes the value on the left as the final input to the
command-like expression on the right:

```bpfman
let doc = $raw.stdout |> jq "fromjson"
let mapCount = $prog |> jq ".status.maps | length"
```

Double-quoted strings support `${...}` interpolation. Single quotes are
literal and are often clearer for jq filters that contain double quotes:

```bpfman
let program_filter = '[.programs[] | select(.name == "pass")][0]'
let rendered = "pid=${pid} map=${mapID}"
```

Use `file:$var` when a command needs a structured value materialised as a
temporary file. `exec` also has an explicit helper:

```bpfman
guard path <- file temp $doc
guard checked <- exec jq . file:$doc
```

## Typed Defs

`def` declares a user command. Declarations are top-level only; imported files
are def-only libraries.

```bpfman
def expect_count(got: number want: number) {
    assert $got == $want
}

def describe(name: string ok: bool) {
    if $ok {
        print "${name}: ok"
    }
}
```

Parameters are whitespace-separated. There is no comma form. Optional
annotations are `number`, `string`, and `bool`, written with a space after the
colon: `want: number`, not `want:number`.

Annotated parameters parse bare word arguments into the declared type and
require already-typed arguments to match. Quoting asserts string-ness, so this
is rejected for a `number` parameter:

```bpfman
expect_count "5" 5
```

Defs can return a value:

```bpfman
def load_xdp(path iface) {
    guard loaded <- bpfman program load file $path --programs xdp:pass
    let prog = $loaded.programs[0]
    guard link <- bpfman link attach xdp --iface $iface $prog --priority 50
    return [$prog $link]
}

guard pair <- load_xdp e2e/testdata/bpf/xdp_pass.bpf.o eth0
let (prog link) = $pair
```

At command position, a returned value is discarded. At bind position,
`guard x <- helper ...` binds the returned value directly on success, while
`let r <- helper ...` binds an inspectable outcome and exposes the returned
value as `$r.value`.

## Imports

Use `import PATH` at top level to load helper defs:

```bpfman
import ../lib.bpfman

expect_program_load $prog xdp extension pass
```

Relative imports resolve against the directory of the script containing the
`import` statement, not the current working directory. Library files should
contain defs; nested/transitive imports from imported libraries are rejected by
the current loader.

Defs are hoisted, so a helper can call another def declared later in the same
main file or imported library. Duplicate def names across main and imported
files are rejected.

## Foreach And Bind-Collect

`foreach` iterates over a list:

```bpfman
foreach name in ["alpha" "beta" "gamma"] {
    print $name
}
```

Use parenthesised names to destructure list elements:

```bpfman
foreach (prio proceed_on) in (zip [50 60] [ok pipe]) {
    print "prio=${prio} proceed-on=${proceed_on}"
}
```

`break` and `continue` apply to the innermost `foreach`.

When the right-hand side of `guard` or `let` is a `foreach`, the final command
in each iteration produces the collected value:

```bpfman
def square(x) {
    return $x * $x
}

guard squares <- foreach n in [1 2 3] {
    square $n
}

foreach x in $squares {
    print $x
}
```

The bind-collect body must end in a command statement. Use `guard` for the
normal success path and `let` when you need to inspect per-iteration outcomes.

## Poll And Retry

`poll` retries a block until it completes without `retry` or until its timeout
expires:

```bpfman
poll timeout 5s every 100ms {
    retry "waiting for ack.1" unless path-exists "${ack}.1"
}
```

Both `timeout` and `every` are mandatory and use Go duration strings such as
`100ms`, `5s`, or `1m`.

Use `retry` only for recoverable "not ready yet" states:

```bpfman
poll timeout 100ms every 1ms {
    let probe <- exec test -f $state
    if $probe.ok {
        guard raw <- exec cat $state
        let doc = $raw.stdout |> jq "fromjson"
        retry "waiting for ready status" unless $doc.status == ready
    } else {
        retry "waiting for state file"
    }
}
```

Helpers called from inside a poll may execute `retry`; the retry applies to the
caller poll attempt. `retry` outside an active poll is a runtime error.

`require` is fatal immediately, including inside a poll. `assert` is not valid
inside a poll. Ordinary command, guard, and expression failures inside a poll
are fatal unless the script handles them and turns the state into an explicit
`retry`.

## Defer Cleanup

`defer CMD` registers cleanup in the current scope. Defers unwind in LIFO
order:

```bpfman
guard pair <- net veth-pair
defer net release $pair

guard loaded <- bpfman program load file \
    testdata/bpf/xdp_pass.bpf.o \
    --programs xdp:pass
let prog = $loaded.programs[0]
defer bpfman program unload $prog

guard link <- bpfman link attach xdp --iface $pair.host_link $prog --priority 50
defer bpfman link detach $link
```

Register resource cleanup at the scope that owns the resource. A def has its
own defer scope; def-local defers run before the caller receives the returned
value. For helpers that create resources intended to escape, return the
handles and let the caller register cleanup.

For background jobs, the common cleanup idiom is:

```bpfman
guard job <- start sleep 60
defer kill $job
```

An unmanaged job at scope exit is reported as a leak. The job
lifecycle primitives are `wait $job` (block for exit and capture its
result), `kill $job` (terminate it), `jobs` (list the live jobs), and
`reap` (collect finished jobs). `process children <pid>` is a separate
observation builtin that returns the direct children of a process,
used to find a worker spawned behind a launcher.

## Assertions And Matches

`assert` records a failure and lets the script continue. `require` halts
immediately. Most assertions are ordinary expressions:

```bpfman
require path-exists $path
assert not-empty $out.stdout
assert contains $out.stdout "ready"
assert $delta >= 5
```

The command-status forms are still command-shaped:

```bpfman
assert ok bpfman program get $pid
assert fail bpfman link get $linkID
require not ok bpfman program get $missing
```

Use `matches` for structured values:

```bpfman
assert $prog matches {
    record.load.program_type:   xdp
    status.kernel.program_type: extension
    status.kernel.id:           $prog.record.program_id
    status.kernel.tag:          not-empty
}
```

Indexed paths are allowed:

```bpfman
assert $doc matches {
    items[0].id: 1
    items[1].id: 2
}
```

`matches exhaustive` additionally checks structural coverage at that level.
Use nested `matches exhaustive` blocks rather than dotted paths inside an
exhaustive block:

```bpfman
assert $doc matches exhaustive {
    left: matches exhaustive {
        keep: 1
    }
    right: matches exhaustive {
        keep: 1
    }
}
```

Entries in a `matches` block are newline-separated. Commas and semicolons are
not entry separators.

## Jq And Pure Builtins

Pure builtins are expression forms:

```bpfman
let doc = jq "." "{\"status\":\"ready\"}"
let status = $doc |> jq ".status"
let ids = range 3
let pairs = zip [1 2] [a b]
let le32 = u32le 4660
let le64 = u64le 42
```

`jq FILTER VALUE` applies a jq filter to a value. Threading is usually the
clearest form once you already have a value:

```bpfman
let count = $listed |> jq ".links | length"
```

`range N` produces `[0, 1, ..., N-1]`. `zip A B` pairs two equal-length lists
into two-element lists. `u32le` and `u64le` encode unsigned integers as
lowercase little-endian hex strings with no `0x` prefix, which is useful for
global data arguments:

```bpfman
guard loaded <- bpfman program load file \
    testdata/bpf/multi_prog_tcx_counter.bpf.o \
    --programs tcx:mtcx_a,tcx:mtcx_b \
    -g "weight_a=0x${u64le $weight_a}"
```

Predicate builtins return booleans:

```bpfman
path-exists $path
contains $haystack $needle
null $value.field
present $value.field
missing $value.field
empty $value.field
```

`present`, `missing`, `null`, and `empty` distinguish absent paths, JSON null,
and present non-null values.

## bpfman And Net Helpers

The `bpfman` builtin exposes the bpfman domain commands used by the e2e
scripts:

```bpfman
bpfman program list
guard loaded <- bpfman program load file prog.o --programs xdp:pass
guard prog <- bpfman program get $pid
bpfman program unload $prog

guard link <- bpfman link attach xdp --iface eth0 $prog --priority 50
guard listed <- bpfman link list -o json
bpfman link detach $link

guard dispatchers <- bpfman dispatcher list --type xdp
guard image <- bpfman image inspect quay.io/example/image
```

Assignable bpfman operations return structured values with typed origins, so
handles can be passed back to later bpfman commands:

```bpfman
let prog = $loaded.programs[0]
let pid = $prog.record.program_id
defer bpfman program unload $prog
```

The `net` builtin is an e2e fixture helper for veth-pair topologies. It
offers two: the host-end pair (`net veth-pair`) keeps one end in the
root namespace, and the isolated pair (`net netns-veth-pair`) puts both
ends in their own owned namespaces.

```bpfman
guard pair <- net veth-pair
defer net release $pair

guard _ <- net exec $pair ping -c 5 -i 0.05 -W 1 $pair.host_addr
guard pinger <- net start $pair ping $pair.host_addr
defer kill $pinger
```

`net veth-pair` returns fields including `ns`, `host_link`, `peer_link`,
`host_addr`, and `peer_addr`. With no flags it auto-names and auto-
addresses from a shared pool; the full explicit form is `net veth-pair
--ns=NS --host-link=NAME --host-addr=CIDR --peer-link=NAME
--peer-addr=CIDR`.

The isolated topology takes no flags and returns a pair of two
symmetric endpoints, `$pair.a` and `$pair.b`, each carrying `ns`,
`link`, `addr`, `ifindex`, and `nsid`. There is no privileged host
side, so `net exec` and `net start` take an explicit endpoint, not the
bare pair; `net release` takes the pair and tears down both namespaces:

```bpfman
guard pair <- net netns-veth-pair
defer net release $pair

guard _ <- net exec $pair.b ping -c 5 -i 0.05 -W 1 $pair.a.addr
```

`net release` is idempotent. `net exec` captures a command result from
inside the namespace, while `net start` returns a background job
handle.

Other e2e helpers are intentionally specialised:

```bpfman
guard d <- tempdir test-prefix
defer exec rm -rf $d.path

guard target <- uprobe target
guard fn <- kfunc acquire
defer kfunc release $fn

guard worker <- fire uprobe $sentinel $ack --count 5
defer kill $worker
```

`uprobe target` resolves and can drive the in-process fixture target;
`uprobe-target` is the lighter sibling, returning just `.path` and
`.symbol` for attach-only tests that want a valid uprobe target without
the fire wave-protocol overhead.

The `linkinfo` and `proginfo` builtins read kernel link and program
metadata directly through the bpf() syscall, so tests need not parse
`bpftool` output. In particular `bpftool link show id` can exit
non-zero while still printing the link, which otherwise forces callers
to ignore the exit code and assert on stdout. `linkinfo id N` returns
`id`, `prog_id`, and `type`; `proginfo id N` and `proginfo pinned
PATH` return `id`, `type`, `name`, and `tag`. Both carry typed
origins, so `--check` validates field access and rejects typos:

```bpfman
guard info <- linkinfo id $kernelLinkID
assert $info.prog_id == $prog.record.program_id

guard pinned <- proginfo pinned /sys/fs/bpf/my_prog
let kernelID = $pinned.id
```

Prefer `linkinfo` and `proginfo` over raw `bpftool link show` and
`bpftool prog show`. Use raw `ip`, `bpftool`, `sh`, or `exec` for
cases outside the helper surface.

## Common Sharp Edges

- Use spaces around `=`, `<-`, comparison operators, `-`, and `/` when they are
  language syntax.
- Parameters, destructuring, `foreach` names, and list elements are
  whitespace-separated; commas are rejected in these binding/list positions.
- `let x = helper` binds the literal string `"helper"` if `helper` is not being
  called via `<-`. Use `let x <- helper ...` or `guard x <- helper ...` to call
  a def and capture its return value.
- `assert` failures are counted but do not stop the script. Use `require` or
  `guard` for fail-fast checks.
- `poll` has no implicit retry. The body must execute `retry` for a recoverable
  not-ready state.
- `defer` accepts one command form, not a block.
- List literals are `[a b c]`; there is no command substitution syntax.
- `$x[index]` accepts an integer index or `$ident`, not an arbitrary expression.
