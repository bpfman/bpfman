Behavioural shell fixtures live here. They are the gold standard
for user-visible language availability: if the shipped DSL
expects users to write a feature, that feature should appear here
in at least one realistic checked-in script.

Layout:

- `testdata/<feature>/<case>/main.bpfman`
- imported helpers for the case sit beside `main.bpfman`
- `testdata/<feature>/<case>/expect.yaml` holds the observable
  contract for that end-to-end behavioural fixture

Conventions:

- Use checked-in scripts for outside-in, user-visible behaviour:
  script mode, imports, jobs, foreach/bind-collect, poll/retry,
  return/defer workflows, diagnostics, and cleanup.
- Prefer adding a behavioural fixture when a feature graduates from
  unit-test-only coverage into supported language surface. The
  fixture corpus is the authoritative answer to "can users write
  this?".
- Keep tiny parser/evaluator unit tests inline in Go when the
  whole point is a single minimal construct rather than a script
  a user would realistically write.
- Prefer real shell behaviour over synthetic placeholders:
  `sh`, `sleep`, `cat`, `test`, `rm`, temp files, and temp dirs
  make the DSL surface easier to read and reason about.
- Keep the expectation close to the script. `expect.yaml` supports:
  `stdout_lines`, `stdout_contains`, `stdout_not_contains`,
  `stderr_contains`, `stderr_not_contains`, `err_is`,
  `err_text`, `assert_failures`, `defer_failures`, `job_leaks`,
  `normalise_stdout`, and `normalise_stderr`.
- Diagnostics now carry the script file and absolute line/column
  from lex time, so fixture expectations should assert the exact
  `{main}:N`, `{lib}:N`, or `{helpers}:N:M` citation the user
  should see.
- `expect.yaml` is required for these fixture-driven behavioural
  tests. Unknown keys are rejected so typos do not silently weaken
  coverage.
- Supported normalisers today are `jobs_listing` and
  `poll_timeout`.
- Path placeholders expand relative to the fixture directory:
  `{root}` plus one placeholder per sibling `.bpfman` file such as
  `{main}`, `{lib}`, or `{helpers}`.
- Placeholder names come from sibling `.bpfman` filename stems, so
  keep those stems simple and intentional.
