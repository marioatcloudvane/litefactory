# Project checks for Bug repairs

The factory reads `.litefactory/bug-checks.yaml` from the exact recorded **main
commit**, never from the repair branch. A recipe does not grant permission. All
three phases must validate before any command runs. This is the format (the
executable names below are placeholders, not a configured project recipe):

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

Each `argv` is passed literally. There is no implicit shell, interpolation,
command selection, or agent-added argument. Explicit interpreters receive the
same containment. `cwd` defaults to the repository root and must remain inside
the worktree, including after symlink resolution. Bare executable names use only
the operator's absolute PATH directories; relative executable paths resolve from
`cwd`. Environment names refer to operator-authorized values; inline values,
protected process settings, credentials and capability variables are refused.

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
macOS, Landlock on Linux), the same containment the Codex agents run under. The
factory passes one explicit permission profile per command: the whole filesystem
is readable except `$LF_HOME` and the operator's `deny_read` paths; writes are
limited to the worktree, a private temporary directory (`TMPDIR`) and the
operator's `writable_roots` (for example compiler caches); the network is off.
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

The phase ledger claims each command before launch. Exact replay returns retained
progress; conflicting input is refused. Completed commands are not repeated.
After an uncertain launch or cleanup, recovery remains blocked and retains the
worktree guard; it never signals a stored PID without verified process identity.
Durable cancellation prevents subsequent launches; the activity's `Cancel`
method also stops its live process group. A cancellation is confirmed only
with observed cleanup. The child owner is responsible for routing cancellation
and scheduling reconciliation after restart.

`lf_bug_checks` executes only one phase and returns `passed`, `failed`, `blocked`
or `cancelled` with an evidence reference. Storage failures remain errors.
The test child owns phase order and deliberate new infrastructure generations;
this activity neither retries checks nor changes analysis or repair budgets.
Passing evidence remains historical when invalidated; consumers must require
its current-validity and reliability fields as well as matching bindings.

Right after launch the factory retains the command's process group with the
kernel boot identity and the leader's start time (macOS and Linux). A crash
before that record keeps the claimed launch unresolved. Recovery never signals:
it only probes the recorded group. A changed boot or an observed empty group
can settle interruption as blocked (or confirmed cancellation when requested).
Unreadable observations, legacy launch records without kernel identity, and
claimed launches without a recorded process remain unresolved. No stored PID is
signalled during recovery. Completed command evidence is reused without reruns;
only fresh source/target/context observation can finalize a pending passing phase.
