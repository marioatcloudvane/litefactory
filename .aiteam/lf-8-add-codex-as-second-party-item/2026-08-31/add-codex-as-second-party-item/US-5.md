# US-5 — Vendor-neutral Runner interface + factory

> As a developer, I want `Request`/`RunResult` and the `Runner` interface to live
> in a vendor-neutral package with no Claude-named fields, and a factory
> selecting the implementation from the resolved label, so that adding OpenCode
> is a new implementation and not a new seam.

Introduces `internal/agent` holding vendor-neutral request/result/session types
and the `Runner` interface, turns `internal/harness` into the Claude
implementation of that interface, adds a Codex implementation, and wires a
factory at daemon startup that replaces today's single hardcoded Claude runner.

## Why This Matters

This is the actual "interface I can exchange, from a factory" the requester
asked for — the part of the feature that makes a second (and eventually third)
coding-agent tool a matter of writing one new implementation rather than
touching every place that currently assumes Claude. It is also the part with the
most subtle failure mode: the previous attempt at a "vendor-neutral" boundary
(a field literally named `ClaudeSessionID`) leaked the very vendor identity it
was supposed to hide, and a design review caught the replacement spec repeating
that exact mistake with `ClaudeTools`/`CodexSandbox` fields. Getting the
boundary genuinely neutral — not just renamed — is what lets a future OpenCode
implementation be additive instead of another round of re-cutting the seam.
Most of this work is a refactor of code that already exists and works today; it
does not require Codex to be installed anywhere, and it ships value on its own
even if it turns out Codex isn't available in this environment.

## Architecture Notes

- **Permissions carries no vendor-named field (blocking correction).**
  `agent.Permissions` crosses the interface with only `Profile string`
  (metadata/logging only) plus an opaque per-kind payload that only the
  resolved `Runner` implementation narrows into its own package-local type.
  Claude's tool-surface/allowed-tools lists and Codex's sandbox mode live inside
  `internal/harness` and `internal/codex` respectively — never exported from
  `internal/agent`. The original spec's `Permissions.ClaudeTools` /
  `Permissions.CodexSandbox` fields must not ship; they reintroduce the exact
  defect this story exists to remove.
- **`agent.Kind` validity is registry-driven, not a hardcoded switch (blocking
  correction).** A kind is valid if a runner is registered for it in the
  `RunnerSet` the factory built — not membership in a fixed two-constant set.
  This is required for the third-tool sizing test below to be satisfiable at
  all without editing `internal/turn` or `internal/report`.
- `daemon.buildRunners(cfg) (turn.RunnerSet, error)` should return a concrete
  type, not an interface — "accept interfaces, return structs." It constructs
  every configured implementation once, at startup; a Codex binary that cannot
  be resolved fails startup with a named error, not a later-tick surprise (same
  posture the codebase already applies to reporter dependencies).
- `internal/report` drops its `internal/harness` import in favor of
  `internal/agent` and keeps not importing `internal/turn` (would cycle, since
  `turn` imports `report` to render replies). It also fixes its four
  vendor-named prose strings (e.g. `` `claude` exited with status ``) — in
  scope here, not deferred, since a completed Codex run would otherwise post a
  factually wrong status line to a GitHub issue.
- The five new `ReplyKind`s needed by US-2/US-3/US-7
  (`ReplyAmbiguousAgentLabels`, `ReplyMalformedAgentLabel`, `ReplyUnknownAgent`,
  `ReplyAgentSwitchRefused`, `ReplyProfileNotAvailableForAgent`) land together
  in one task here, in `internal/report/reply.go`'s const block and
  `internal/turn/report.go`'s `replyKind()` switch — not split across the three
  stories that consume them.
- Both implementations must preserve every invariant the Claude harness measured
  into existence: prompt delivered on stdin, never argv; a fixed env allowlist,
  never `os.Environ()` wholesale; line reading via `bufio.Reader.ReadBytes` with
  a 64 KiB starting buffer, never `bufio.Scanner` (a Scanner silently truncates
  on an over-long line, and one has been measured in practice); the run's event
  stream written to `NNNN.jsonl`; a `RunResult` returned for a *failed* run,
  with only setup failures returning a Go `error`.
- The Codex-facing `resume_token` extraction is where B5's validation lives:
  a strict shape regex derived from US-1's measured thread-id format, applied
  before storage and before argv construction; accepted only from the first
  `thread.started` event of a run; empty/whitespace stored as `NULL` and never
  reaching argv; never echoed into a rendered reply.
- Codex env forwarding is an explicit name list (exact set fixed by US-1), not
  an `OPENAI_`-prefix policy — a prefix would also forward
  `OPENAI_BASE_URL`/`OPENAI_API_BASE`, letting the ambient environment redirect
  model traffic (carrying repo content) to an arbitrary host.
- No test may spawn a real `codex` binary. Extend
  `internal/turn/fixture_test.go`'s existing configurable-fake-Claude pattern
  for Codex rather than building a second faking mechanism.
- `turn/gate_test.go` pins exactly three `CloneGate.Acquire` call sites — this
  story's new runner must not add a fourth.
- Risk: three tasks below (the Codex runner, its stream parser/`resume_token`
  extraction, and tests against the fake Codex binary) depend on US-1's
  measurement and are marked accordingly. The rest of this story does not.
- Follow-up ledger entries required by `APP_CONSTRAINTS.md`: `AC_AMENDMENTS.md`
  needs an entry updating CONC-2's "four fixed goroutines" canonical statement
  once the Codex runner adds its own two pipe-reader goroutines (ceiling
  unchanged), and an entry recording the new Codex env-var names alongside
  SEC-4's existing fixed allowlist, each with a documented reason.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-5.1 | `internal/agent` package: `Kind`, `SessionRef`, `Request`, `RunResult`, `Permissions` with neutral vocabulary (no vendor-named exported identifier) | Code | US-4 (`naming.SessionRef` rename) | L |
| T-5.2 | `internal/harness` becomes the Claude implementation of `agent.Runner`; `doc.go` updated to say so | Code | T-5.1 | M |
| T-5.3 | `internal/report`: drop `internal/harness` import in favor of `internal/agent`; fix the four vendor-named prose strings in `run.go` | Code | T-5.1 | M |
| T-5.4 | Five new `ReplyKind`s + switch cases, in one task, shared by US-2/US-3/US-7 | Code | T-5.1 | M |
| T-5.5 | `turn.Runner` / `turn.RunnerSet` declared at the consumption point; registry-driven `Kind` validity | Code | T-5.1 | M |
| T-5.6 | `daemon.buildRunners` factory: constructs every configured implementation once at startup, returns a concrete type, fails boot on an unresolvable configured Codex binary | Code | T-5.2, T-5.5 | M |
| T-5.7 | 🔒 `internal/codex` runner: argv/env/kill, per US-1's measured argv/env shape | Code | T-5.1, US-1 | L |
| T-5.8 | 🔒 `internal/codex` stream parser + `resume_token` extraction with strict shape-validating regex (first `thread.started` event only; empty/whitespace → `NULL`, never argv; never echoed to a reply) | Code | T-5.7, US-1 | L |
| T-5.9 | Fake Codex binary fixture, extending `internal/turn/fixture_test.go`'s existing configurable-fake-Claude pattern | Test | T-5.1 | M |
| T-5.10 | Neutrality test (no exported `internal/agent` identifier contains Claude/Anthropic/Codex/OpenAI) + third-tool sizing test (stub `Kind` routes end to end with no edit to `turn`/`report`) | Test | T-5.5, T-5.6 | M |
| T-5.11 | 🔒 Codex runner tests against the fake binary: event stream, an over-64KiB line read in full, `ErrSessionNotResumable` → exactly one cold rerun recorded as `cold_after_resume_refused` | Test | T-5.7, T-5.8, T-5.9 | M |
| T-5.12 | `AC_AMENDMENTS.md` entries: CONC-2 goroutine-count wording update, SEC-4 allowlist update with the new Codex env-var names and their documented reasons | Doc | T-5.7 | S |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-5.1 | `internal/agent` exists and imports only `internal/config` and `internal/naming` |
| AC-5.2 | No exported identifier in `internal/agent` contains "Claude", "Anthropic", "Codex" or "OpenAI" — including in `Permissions` |
| AC-5.3 | `internal/report` imports `internal/agent` and no longer imports `internal/harness`; it still does not import `internal/turn` |
| AC-5.4 | `internal/report/run.go`'s four vendor-named prose strings are fixed to name the actual agent that ran |
| AC-5.5 | `turn.Options` carries a `RunnerSet`, not a single runner; a `nil` `RunnerSet` is refused at construction, never treated as a no-op |
| AC-5.6 | `agent.Kind` validity is determined by runner registration in the built `RunnerSet`, not by a hardcoded constant switch |
| AC-5.7 | The factory constructs every configured implementation at daemon startup; a Codex binary that cannot be resolved fails startup with a named error rather than failing on a later tick |
| AC-5.8 | `go vet ./...`, `gofmt -l .`, `make lint`, and `make test -race` are green |
| AC-5.9 | Both implementations deliver the prompt on stdin; a test asserts the prompt string appears in no argv |
| AC-5.10 | Both implementations build the child environment from a fixed allowlist; a test asserts `os.Environ()` is not forwarded wholesale, and Codex's forwarded set is an explicit name list, not a prefix |
| AC-5.11 | A Codex run writes an `NNNN.jsonl` event stream and populates `Events`, `Malformed`, and `LongestLine`, tested against the fake binary only |
| AC-5.12 | A Codex event-stream line larger than 64 KiB is read in full (no `bufio.Scanner` in either implementation — a review failure if found) |
| AC-5.13 | `agent.ErrSessionNotResumable` returned by either implementation triggers exactly one cold rerun, recorded as `cold_after_resume_refused` |
| AC-5.14 | A failed Codex run returns a `RunResult` (not a Go error) and is reported, logged, and has its metadata written |
| AC-5.15 | Third-tool sizing test: a test-only third `Kind` with a stub runner routes end to end through label resolution, the factory, `turn`, and `report`, with no edit to `internal/turn` or `internal/report` |
| AC-5.16 | `resume_token` extraction is accepted only from the first `thread.started` event, validated against a strict shape regex before storage, and a malformed/empty value never reaches argv |
| AC-5.17 | `turn/gate_test.go`'s three pinned `CloneGate.Acquire` call sites are unchanged in count |
| AC-5.18 | `AC_AMENDMENTS.md` gains the CONC-2 and SEC-4 entries described in Architecture Notes |
