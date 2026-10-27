# Project checks (`.litefactory/bug-checks.yaml`)

[Documentation](README.md) · [Configuration](configuration/README.md)

A repository can tell the factory how to test it with a recipe in `.litefactory/bug-checks.yaml`. The factory reads the recipe from the exact commit being tested, never from a working branch the agent could change. Today it is used by:

- **[Validate](workflow/validate.md):** the suite step uses the `full` phase as its complete obligation inventory, with conservative automatic selection when its captured workflow supports it.
- **[Spike](workflow/spike.md):** the check after a small fix runs the `focused` and then the `full` phase.

(The Bug workflow lets its fixer run the project's own commands and runs no recipe of its own; see [Bug](workflow/bug-runtime.md).)

A recipe does not grant permission; the operator's `bug_checks` sandbox settings in [config.yaml](configuration/config-yaml.md) do. All three phases must validate before any command runs, even if a workflow only uses one. This is the format (the executable names are placeholders, not a real project recipe):

```yaml
version: 1
phases:
  regression:
    commands:
      - argv: [./project-regression-wrapper]
  focused:
    commands:
      - argv: [./project-focused-wrapper]
  full:
    commands:
      - argv: [./project-full-wrapper]
        cwd: .
        timeout_seconds: 1800
        env: [PROJECT_TEST_ENDPOINT]
```

Each configured `argv` is literal data. There is no implicit shell, interpolation
or agent-added argument. Capability-checked Validate adapters may derive bounded
selectors and invocation-only freshness controls while preserving the configured
conditions; unsupported commands retain their unchanged full invocation. Explicit
interpreters receive the same containment. `cwd` defaults to the repository root and must remain inside
the worktree, including after symlink resolution. Bare executable names use only
the operator's absolute PATH directories; relative executable paths resolve from
`cwd`. Environment names refer to operator-authorized values; inline values,
protected process settings, credentials and capability variables are refused.

Optionally a command can have a `rerun` block, so a failure in `go test` output can be attributed to named tests by rerunning only those tests: `failures: go-test`, an `argv` containing the whole elements `{tests}` and `{packages}` once each, and an optional `timeout_seconds`. The declaration remains valid. Targeted execution additionally requires proven adapter equivalence and observed requested identities; missing observations never count as passing tests.

Version 1 limits: recipe 1 MiB; 64 commands per phase; 128 argv entries and 64 KiB
combined argv bytes per command; 64 distinct environment names; integer timeout
1–86400 seconds, default 1800. Unknown/duplicate YAML keys, multiple documents,
anchors/aliases/merge keys, scalar coercion, NUL arguments, and missing/empty
phases are refused. The target file must be a regular Git blob, not a symlink.

A command or project-owned wrapper must return **0 for completed successful
checks**, **1 only for ordinary test/assertion failure**, and another code for
infrastructure or indeterminate results. The factory does not write wrappers for
the project. Signals, cancellation, timeout, output overflow, incomplete capture,
source drift and uncertain cleanup override exit status. Output wording is never
interpreted as success. A wrapper must report known flakiness as blocked rather
than hide failed internal retries. Undisclosed internal retries cannot be detected
from arbitrary output. Retained reliability flags survive new generations for
the same source, recipe and target.

Commands execute serially and stop on the first nonpassing observation. Each has
a 4 MiB combined stdout/stderr bound; overflow cancels execution and records a
partial capture. Declared environment values and recognized credential shapes are
redacted before output enters evidence. Environment names, not their values, are
retained in command metadata.

Every command runs under the **Codex sandbox** (`codex sandbox`: Seatbelt on
macOS, Landlock on Linux), the same kind of containment the Codex agents run under. The
factory passes one explicit permission profile per command: the whole filesystem
is readable except `$LF_HOME` and the operator's `deny_read` paths; writes are
limited to the worktree, a private temporary directory (`TMPDIR`), the system
`/tmp` (as in Codex's `workspace-write`) and the operator's `writable_roots` (for example compiler caches); the network is off
unless the operator grants `network`, which opens it loopback included.
The command sees only `PATH`, `HOME`, `LANG`, `TMPDIR` and declared environment
names; nothing is inherited from the daemon. Each command runs in its own process
group. When it ends, every remaining member is stopped and cleanup is confirmed
only once the group is observed empty; otherwise cleanup stays unknown and the
worktree guard is retained. There is no fallback to running tests with daemon
permissions, and an unavailable sandbox launcher blocks before launch.

The factory binds each phase to task, repair, caller generation, source commit
and tree, target repository/main/commit, retained recipe digest and bytes, and
operator environment/permission revisions. It observes clean committed source
and target before and after commands and at completion. Ignored generated outputs
may remain; changed tracked files or unexpected nonignored files invalidate the
run. A worktree guard excludes fixer/save and overlapping check work.

The phase ledger claims each command before launch. Replaying the same input returns the retained progress; conflicting input is refused. Completed commands are not repeated. After an uncertain launch or cleanup, recovery stays blocked and keeps the worktree guard; it never signals a stored PID without a verified process identity. A cancellation is confirmed only with observed cleanup.

Each phase ends as `passed`, `failed`, `blocked` or `cancelled` with an evidence reference. Passing evidence stays as history if it is later invalidated; consumers must also check that it is still valid, reliable and bound to the same commit.

Right after launch the factory retains the command's process group with the
kernel boot identity and the leader's start time (macOS and Linux). A crash
before that record keeps the claimed launch unresolved. Recovery never signals:
it only probes the recorded group. A changed boot or an observed empty group
can settle interruption as blocked (or confirmed cancellation when requested).
Unreadable observations, legacy launch records without kernel identity, and
claimed launches without a recorded process remain unresolved. No stored PID is
signalled during recovery. Completed command evidence is reused without reruns;
only fresh source/target/context observation can finalize a pending passing phase.

## Validate coverage and freshness

Plain `/validate` selects automatically; exact `/validate full` requests new full
execution rather than reuse, even on an unchanged commit. A repeated delivery of
the same request replays its own generation. Repository suite configuration does
not change, and `make test` remains this repository's full-suite entry point.
Invocation-only cache controls bypass test-result reuse, preserving build caches
and original plain/race groups.

Capability-checked adapters cover Go, pytest, distinct .NET VSTest and
Microsoft.Testing.Platform invocations, and xcodebuild tests. Selection depends on
bounded discovery and complete result evidence, not the executable name alone.
An unsupported wrapper, unknown dependency or dynamic fixture consumer, changed
shared input, generated asset, toolchain or cross-language dependency requires
complete project checks or the original full recipe. Opaque commands run fully
when their relevance or earlier completion cannot be established. Legacy captures
without the assessment contract retain full execution.

Every inherited unit retains its original complete execution source and anchor.
Re-checks compare all changes since that anchor, including merged-base changes,
and require compatible conditions and proven ancestry. Incomplete or incompatible
records stay readable but cannot establish inherited coverage. The retained
assessment distinguishes newly executed and inherited passes, transient retries,
matching pre-existing failures and unresolved obligations. Raw failed executions
remain failed. Test deletion, skipping, capture truncation or a zero-match selector
cannot prove a repair.

Persistent test failures receive one uncached retry before equivalent comparison
at a pinned configured delivery target commit. Only complete matching case and
diagnostic evidence removes the suite failure's delivery effect; it proves no
unrelated criterion and grants no delivery approval. Sandbox permissions, process
claims, locks, cleanup, unchanged-tree guards and owner-controlled delivery apply
to discovery, targeted work and target-branch comparisons alike. No automatic
merge or target-branch repair is performed.
