# US-1 — Codex CLI probe

> As a maintainer, I want a read-only, reversible probe of the real `codex` binary
> recorded as `CODEX_CLI_CONTRACT.md`, so that argv, event-stream, and session-id
> design rests on measurement rather than on documentation.

Runs `codex` directly, in a scratch directory outside every repository worktree,
and writes down exactly what it does — converting every second-hand claim about
Codex in the research brief into either a confirmed fact or an explicit "could
not be measured."

## Why This Matters

Every claim this whole feature rests on about how Codex actually behaves — its
command-line shape, how it reports session/thread identity, what its sandbox flag
really restricts, whether its own config file can silently widen that sandbox —
comes from documentation, not from running the program. That gap is expensive to
carry forward: a wrong guess about session-id format shows up as a broken resume
months later, and a wrong guess about sandbox behavior shows up as a security hole
(the codebase has already been burned once by a permission mechanism that looked
restrictive on paper and admitted 250 ambient rules in practice). This story is
the one place all of that gets settled before anyone writes the Codex-specific
code that depends on it. It is also the story most likely to find bad news: if no
`codex` binary is actually available where this runs, that has to surface here,
loudly, rather than be discovered halfway through building the runner.

## Architecture Notes

- Model this document directly on `.aiteam/main/2026-08-25/session-resume/CLAUDE_CLI_CONTRACT.md`
  — same structure, same "re-verify after an upgrade" closing section, same
  practice of citing the literal command and its literal output for every claim.
- The single most valuable kind of finding here is a *refutation* — a `[W]`
  claim in `RESEARCH_BRIEF.md` that turns out to be wrong. Write this expecting
  to find at least one; a contract that confirms everything it set out to check
  is a weaker result than one that also catches a wrong assumption.
- Two probe questions were missing from the original scope and are the ones with
  actual security consequences, not just design consequences: whether
  `~/.codex/config.toml` can widen a sandbox mode passed on argv (and whether MCP
  servers autoload from it), and whether `OPENAI_BASE_URL`/`OPENAI_API_BASE`-class
  env vars are honored (if so, any prefix-based env forwarding for Codex is
  unsafe — see US-5's env allowlist decision). Both are acceptance criteria below,
  not optional extras.
- `$CODEX_HOME` (default `~/.codex`) is shared across every worktree and task on
  the machine. Probe whether it tolerates concurrent writers and whether a
  transcript from one task is visible to another — this bears directly on US-5's
  `resume_token` validation design.
- Platform: no probe run may modify any file inside this repository or any
  registered worktree; run everything in a scratch directory created for this
  purpose and torn down afterward.
- Risk: if the `codex` binary is not installed in this environment, this story
  fails loudly and records "unavailable" rather than proceeding on documentation.
  Per the plan's Workflow section, that does not stop the rest of the feature —
  it blocks only the Codex-gated tasks in later stories, which are named
  explicitly in each of their sub-issues.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-1.1 | Binary availability check: `which codex`, version, resolved path | Probe | — | S |
| T-1.2 | Argv/stdin probe: non-interactive invocation shape, whether a prompt can be delivered on stdin | Probe | T-1.1 | M |
| T-1.3 | `--json` event-stream probe: event type names and per-event field schema | Probe | T-1.1 | M |
| T-1.4 | Thread/session-id probe: exact format, where it is surfaced (stdout vs. a file); derive the validating regex US-5 needs | Probe | T-1.3 | M |
| T-1.5 | Resume semantics probe: `resume --last`, `resume <id>`, and behavior on an unknown id (does it fail closed?) | Probe | T-1.4 | M |
| T-1.6 | Exit code probe: success, tool-reported failure, refused resume | Probe | T-1.2 | S |
| T-1.7 | Auth env-var probe: which vars authenticate a non-interactive run; whether `OPENAI_BASE_URL`/`OPENAI_API_BASE`-class vars are honored | Probe | T-1.1 | M |
| T-1.8 | Sandbox flag probe: accepted values; whether `~/.codex/config.toml` can widen a sandbox mode passed on argv; whether MCP servers autoload from ambient config | Probe | T-1.1 | L |
| T-1.9 | `$CODEX_HOME` probe: concurrent-writer safety, cross-task transcript visibility | Probe | T-1.1 | M |
| T-1.10 | Subagent/hooks capability probe (feeds US-8's content) | Probe | T-1.1 | M |
| T-1.11 | Write `CODEX_CLI_CONTRACT.md`: full findings, `[W]`→confirmed/refuted/unmeasurable table against every tagged claim in `RESEARCH_BRIEF.md`, and a "re-verify after upgrade" section | Doc | T-1.2, T-1.3, T-1.4, T-1.5, T-1.6, T-1.7, T-1.8, T-1.9, T-1.10 | M |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-1.1 | `CODEX_CLI_CONTRACT.md` exists in this session directory and records the `codex` binary's version and resolved path, or explicitly records "unavailable" |
| AC-1.2 | If unavailable, the contract states this is a gate: every Codex-gated task named in US-4/US-5/US-6/US-7/US-8's sub-issues stays blocked, and says so |
| AC-1.3 | Every probe ran in a scratch directory outside every git worktree and every registered clone; no file inside this repository was modified by a probe |
| AC-1.4 | The contract answers, each with the literal command run and its literal output: (a) argv shape of a non-interactive run, (b) whether a prompt can be delivered on stdin, (c) the session/thread id's format and exactly where it is surfaced, (d) the per-event JSON field schema for each event type emitted with `--json`, (e) resume semantics for both `resume --last` and `resume <id>`, (f) resume behavior on an unknown id — specifically whether it fails closed, (g) exit codes for success, tool-reported failure, and refused resume, (h) which env vars authenticate a non-interactive run, (i) the exact accepted values of the sandbox flag, (j) whether subagent and hook primitives exist and how they are configured |
| AC-1.5 | The contract additionally answers, with the same literal-command/literal-output standard: (k) whether `~/.codex/config.toml` can widen a sandbox mode passed on argv, and whether MCP servers autoload from it, (l) whether `OPENAI_BASE_URL`/`OPENAI_API_BASE`-class env vars are honored, (m) whether `$CODEX_HOME` tolerates concurrent writers and whether one task's transcript is visible to another |
| AC-1.6 | Each claim in the contract is tagged `[V]` with its command, or explicitly marked "could not be measured" |
| AC-1.7 | A table maps every `[W]`-tagged Codex claim in `RESEARCH_BRIEF.md` to confirmed / refuted / unmeasurable |
| AC-1.8 | The contract carries a "re-verify after a Codex upgrade" section, matching `CLAUDE_CLI_CONTRACT.md`'s convention |
