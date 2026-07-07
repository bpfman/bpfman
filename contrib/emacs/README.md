# Emacs support for bpfman

This directory contains Emacs tooling for the bpfman-shell language.

## Files

- [`bpfman-mode.el`](bpfman-mode.el) -- major mode for editing
  `.bpfman` scripts: syntax highlighting, comment handling, and paired
  `()`/`{}`/`[]` delimiters so `show-paren-mode` picks up block and
  command-substitution structure.
- [`syntax-gallery.bpfman`](syntax-gallery.bpfman) -- a deliberately
  inert `.bpfman` file that exercises every construct in the
  language. Use it to verify highlighting after changes to the mode.
  It is not intended to be evaluated.

## Installing

### Load directly

```elisp
(load-file "/path/to/go-bpfman/contrib/emacs/bpfman-mode.el")
```

### Via `load-path`

```elisp
(add-to-list 'load-path "/path/to/go-bpfman/contrib/emacs/")
(require 'bpfman-mode)
```

### With `use-package`

```elisp
(use-package bpfman-mode
  :load-path "/path/to/go-bpfman/contrib/emacs/"
  :mode "\\.bpfman\\'")
```

The mode auto-associates with files ending in `.bpfman`. Invoke
`M-x bpfman-mode` to force it on a buffer that is not so named.

## Verifying highlighting

Open `syntax-gallery.bpfman` in Emacs:

```
emacs syntax-gallery.bpfman
```

Scroll top to bottom. The gallery is organised into 25 numbered
sections, each comment-delimited. Scan each section and confirm
tokens are fontified according to this table:

| Face                          | Applied to                                                      |
|-------------------------------|-----------------------------------------------------------------|
| `font-lock-keyword-face`      | `let`, `guard`, `defer`, `if`, `elif`, `else`, `foreach`, `break`, `continue`, `poll`, `retry`, `return`, `def`, `bpfman`, `assert`, `require`, `jq`, `range`, `zip`, `u32le`, `u64le`, `defs`, `exec`, `file`, `fire`, `import`, `jobs`, `kill`, `net`, `print`, `reap`, `start`, `tempdir`, `trace`, `wait`; the `${` and `}` delimiters of a string interpolation |
| `font-lock-builtin-face`      | domain subcommands (`dispatcher`, `doctor`, `gc`, `link`, `list`, `load`, `program`, `programs`, `show`, `attach`, `detach`, `get`, `image`, `status`, `unload`, ...), attach kinds (`fentry`, `fexit`, `kprobe`, `tc`, `tcx`, `tracepoint`, `uprobe`, `xdp`), assertion/predicate words (`fail`, `not`, `ok`, `contains`, `empty`, `missing`, `not-empty`, `null`, `path-exists`, `present`), logical and control-clause words (`and`, `or`, `in`, `timeout`, `every`, `unless`), `matches` / `exhaustive`, and expression operators (`==`, `!=`, `<`, `<=`, `>`, `>=`, `+`, `-`, `*`, `/`, `%`) |
| `font-lock-variable-name-face`| `$var` references, braced `${var}`, the identifier after `let`, adapter refs (`file:$var`), the body inside a `"${...}"` interpolation |
| `font-lock-string-face`       | literal runs of `"double-quoted"` and `'single-quoted'` strings (including the quote marks themselves) |
| `font-lock-comment-face`      | `# to end of line` (outside quotes)                             |
| `font-lock-constant-face`     | `--long` and `-x` flags                                         |
| (no face)                     | plain argument-position words (paths, numeric IDs), `[ ] { } ;` delimiters |

Specific lines to watch:

- Section 3 covers every variable-reference form, including the
  braced `${name.path[0]}` syntax.
- Section 4 exercises single and three-deep nested `[cmd]`
  substitution. After the inner `]` closes, the trailing argument in
  `[exec echo [json parse '"a"'] [json parse '"b"']]` is an argument
  to `exec echo` (not a fresh command), so it should not be
  keyword-highlighted.
- Section 5 has both the word (textual) and symbol (numeric)
  comparison operators. Both must highlight as builtin.
- Section 17 places `=` and `==` on adjacent lines to verify the
  tokeniser distinguishes them.
- Section 24 exercises `assert <expr> matches { ... }`. The keyword
  `matches` should be builtin-face; the path entries (e.g.
  `record.load.program_type:`) and the literal patterns are plain;
  `$var` patterns get the variable-name face; the `not-empty`
  predicate is builtin-face.
- Section 25 exercises `def NAME(P1, P2) { BODY }`. The keyword
  `def` is keyword-face; the def name and each parameter name (with
  trailing-comma support) get the variable-name face. Body
  statements fontify by the usual rules. Call sites of a registered
  def do *not* highlight the name specially -- the fontifier has no
  cross-line memory of which words have been registered.

## Maintaining

When the language gains or loses a keyword/builtin, update:

1. The relevant hash tables in `bpfman-mode.el`
   (`bpfman--commands` and `bpfman--subcommands`).
2. The state-machine transitions in `bpfman--fontify-line-tokens`
   if the new construct introduces a distinct statement position.
3. `syntax-gallery.bpfman` with a representative example.
4. The face table above if a new face is used.

Bump `bpfman-mode.el`'s `Version:` header on user-visible changes.
