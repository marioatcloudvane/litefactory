# Plan Brief — Add Codex as second-party coding-agent tool
# Branch: lf-8-add-codex-as-second-party-item
# Date: 2026-09-01
# Parent issue: GitHub issue #8 ("Add Codex as second party item")
# Citation base commit: 794ab22
# Research brief: RESEARCH_BRIEF.md (this session directory)

This plan was produced headlessly (no human available mid-run) and reviewed by a
role-switching architect pass before being written. Five findings from that review
(B1–B5 below) are load-bearing corrections to the requirements-engineer's original
feature spec and are folded directly into the decisions and stories below, not
left as commentary.

## Shared Decisions

- **Label scheme (A-1).** The prefix is `lf:agent=<kind>`, mirroring `lf:tools=`'s
  regex, resolution ladder, and refusal vocabulary exactly. The requester's phrasing
  ("a label that says 'Coding Agent: Codex | Claude'") is read as intent, not a
  literal string.
- **Mid-session tool switch is refused, not resumed or silently restarted (A-2).**
  A resolved agent that differs from a segment's owning agent refuses the run.
  Honoring a switch would require a second segment on the session, which needs
  litefactory's own separate, unbuilt multi-segment milestone (confusingly also
  numbered "LF-8" in migration comments — an unrelated internal numbering scheme,
  not this GitHub issue). The `agent` column is added now so that a future
  `new_segment` policy is an additive code change with no further migration.
- **Permission profiles refuse rather than translate across tools (A-3, endorsed
  by architect review).** A `lf:tools=` profile carries explicit per-tool stanzas;
  a profile with no stanza for the resolved agent refuses the run
  (`ReplyProfileNotAvailableForAgent`) rather than falling back to the other
  tool's default surface or attempting a translation. `--tools` (an allowlist) and
  `--sandbox` (a coarse mode) have no faithful mapping in either direction, and
  the codebase has direct precedent for why guessing is unsafe: a permissive-
  looking mechanism previously admitted 250 ambient rules and executed Bash
  through a gap of exactly this shape. Reversible: a translation table can be
  added later as a pure addition; a silently-translated grant cannot be
  un-granted.
- **Session identity is split into two columns (A-5, endorsed, mechanics
  corrected by B1).** `session_ref` is litefactory's own pre-computed identity
  (always computable before any process runs, stays `NOT NULL`); `resume_token`
  is the tool's own reported handle (sometimes absent, nullable). Conflating
  these into one `claude_session_id` column was the root cause of Claude's
  pre-computed identity and Codex's discovered-after-the-fact identity being
  unable to share a column.
- **Migration mechanics (B1 — blocking correction).** SQLite refuses
  `ALTER TABLE ... ADD COLUMN x TEXT NOT NULL` without a default on a non-empty
  table. Both new discriminator columns (`segments.agent`, `runs.agent`) must be
  added as `ADD COLUMN agent TEXT NOT NULL DEFAULT 'claude'` — this single
  statement satisfies the constraint and performs the backfill; no separate
  `UPDATE` is needed or should be written. `session_ref`'s `RENAME COLUMN` keeps
  its existing constraint set unchanged, which a migration test must assert
  directly (snapshot-before/after), not assume.
- **`agent.Kind` validity is registry-driven, not a hardcoded switch (B3 —
  blocking correction).** A kind is valid if a runner is registered for it in the
  `RunnerSet` the factory built at startup — not membership in a fixed two-value
  constant set. This is required for the third-tool sizing test (a stub `Kind`
  must route end to end with no edit to `internal/turn` or `internal/report`) to
  be satisfiable at all, and it is also what the spec's own edge case
  (`lf:agent=codex` with no Codex configured → `ReplyUnknownAgent`) already
  implies.
- **`agent.Permissions` carries no vendor-named field (B2 — blocking
  correction).** Crossing the `internal/agent` boundary, `Permissions` carries
  only `Profile string` (already shape-validated, logging/metadata only) plus an
  opaque per-kind payload that only the resolved `Runner` implementation narrows
  into its own package-local type. The concrete per-tool shapes — Claude's tool
  surface and allowed-tools lists, Codex's sandbox mode — live inside
  `internal/harness` and `internal/codex` respectively and are never exported
  from `internal/agent`. This is the same defect class `ClaudeSessionID` was
  removed for; the original spec's `Permissions.ClaudeTools` /
  `Permissions.CodexSandbox` fields would have reintroduced it and must not ship
  as drafted.
- **The five new `ReplyKind`s land together, in one task, before the stories that
  need them start.** `ReplyAmbiguousAgentLabels`, `ReplyMalformedAgentLabel`,
  `ReplyUnknownAgent`, `ReplyAgentSwitchRefused`, and
  `ReplyProfileNotAvailableForAgent` are needed by US-2, US-3, and US-7, but all
  five touch the same two files (`internal/report/reply.go`'s const block and
  `internal/turn/report.go`'s `replyKind()` switch). This is a single US-5 task;
  no story adds a `ReplyKind` independently.
- **Refusal precedence is four stages, in a fixed order** — the original spec
  only covered two of them:
  1. Agent label ladder (`internal/daemon`, US-2).
  2. Tools profile ladder (`internal/daemon`, the existing `lf:tools=`
     mechanism, unchanged).
  3. Per-agent stanza narrowing (US-7) — depends on stage 1's outcome, so it
     cannot be folded into stage 2.
  4. Segment-ownership check (US-3) — needs the segment row, so it lives in
     `internal/turn`, evaluated later than the daemon-side stages.
  When both an agent-ladder refusal and a tools-ladder refusal apply in the same
  tick, the agent refusal is reported and the tools decision is never evaluated.
- **`resume_token` validation (B5 — blocking correction).** The token is
  attacker-influenced: it is parsed from a child process's stdout while that
  child is running third-party content (a GitHub issue's body/comments) as its
  prompt, and it is later passed back into argv (`codex exec resume <token>`).
  A strict shape regex, derived from US-1's measured thread-id format, is
  applied both before storage and before argv construction. The token is
  accepted only from the first `thread.started` event of a run. Empty or
  whitespace values are stored as `NULL` and can never reach argv
  (`codex exec resume ""` must be unconstructible). The token is never echoed
  into a rendered GitHub reply. `$CODEX_HOME` is shared across every worktree on
  the machine, so a forged-but-shape-valid token is a cross-task transcript
  disclosure risk, not merely a failed resume — this is why the regex and the
  first-event-only rule are both required, not either alone.
- **Codex env forwarding is an explicit name list, not a prefix.** The existing
  env allowlist forwards `ANTHROPIC_`-prefixed names for Claude; a matching
  `OPENAI_`-prefix policy for Codex would also forward `OPENAI_BASE_URL` /
  `OPENAI_API_BASE`, letting the ambient environment redirect model traffic (and
  every prompt, which contains repository content) to an arbitrary host — a
  materially worse failure than a leaked key. Codex forwarding uses a fixed list
  of specific names (exact set — e.g. `CODEX_API_KEY`, `OPENAI_API_KEY` — fixed
  by US-1's measurement), each with a documented reason, matching the existing
  allowlist's own convention.
- **The forbidden-value guard has two halves, both required.** The existing
  tree-grep test (`TestForbiddenFlagIsAbsentFromTheTree`) gains a Codex clause,
  but that alone does not stop an operator writing the forbidden sandbox value
  directly into a config file. A second, config-load-time refusal — mirroring
  `validateNoForbiddenFlag` — is required over `agents.codex.default_sandbox` and
  every profile's `codex.sandbox` stanza.
- **`naming.ClaudeSessionID` is renamed to `naming.SessionRef`** in the same
  slice as the schema rename (US-4), under a hard guardrail: the pinned
  namespace UUID and the `"<ref>/segment/<ordinal>"` input string must stay
  byte-identical across the rename, or every existing segment's derived
  identity changes and every existing Claude conversation becomes unresumable.
- **`internal/report/run.go`'s four vendor-named prose strings are in scope**
  (e.g. `` `claude` exited with status ``), not deferred as cosmetic — the
  moment a Codex run completes, that prose is posted to a GitHub issue and is
  factually wrong. (`internal/cli/router.go`'s tagline is genuinely cosmetic and
  stays deferred — see Out of Scope.)
- **New packages, existing name kept.** `internal/agent` (vendor-neutral types)
  and `internal/codex` (the Codex implementation) are new leaf-ish packages;
  `internal/harness` keeps its name and becomes the Claude implementation of
  `agent.Runner` — renaming it would touch 26 importing files for no behavioral
  gain.
- **Two `AC_AMENDMENTS.md` ledger entries are required follow-ups**
  (`APP_CONSTRAINTS.md` requires deviations from documented invariants to be
  recorded, not silently allowed to go stale):
  1. CONC-2's "four fixed goroutines… this bullet is the canonical statement"
     needs updating once a second runner adds its own two pipe-reader
     goroutines (same `os.Pipe` rationale as the existing harness; the
     concurrency ceiling itself is unchanged).
  2. SEC-4's "nothing else" env allowlist statement needs the new Codex auth
     variable set recorded alongside the existing fixed list, each entry with
     its documented reason, per SEC-4's own convention.

## Workflow

- **Shipping order.** US-1 (Codex CLI probe) and US-4 (store schema) start in
  parallel — both are independent of everything else and of each other. US-5
  (vendor-neutral interface + factory) starts once US-4 lands; its three
  Codex-gated tasks (the `internal/codex` runner, its stream parser and
  `resume_token` extraction, and tests against a fake Codex binary) additionally
  wait on US-1, but the rest of US-5 — the `internal/agent` package itself,
  `internal/harness`'s adaptation to `agent.Runner`, `internal/report`'s import
  change and prose fix, the five `ReplyKind`s, `turn.RunnerSet`, and the
  `daemon.buildRunners` factory — is a pure refactor of existing Claude code and
  does not wait on US-1's outcome. US-2 (label routing) starts once US-5's
  non-gated core lands. US-3 (switch refusal) needs both US-2 and US-4. US-6
  (capability tiers) and US-7 (permission profiles) both need US-5, not merely
  US-1 — `agent.Permissions` and the factory do not exist before US-5 — with one
  gated task each additionally waiting on US-1. US-8 (`AGENTS.md`) is
  independent of US-5 and only soft-gated by US-1's subagent/hooks probe, which
  informs its content but does not block drafting it.
- **Why this order.** It maximizes what ships even if US-1 finds no usable
  `codex` binary. The architect review found no `codex` binary and no
  `~/.codex` directory on the machine this plan was reviewed on (filesystem
  checks in the standard install locations; a direct `which codex` was denied
  by that session's own permissions, so this is strong evidence, not
  conclusive proof, and should be re-checked in whatever environment actually
  runs the daemon). If the binary turns out to be unavailable there too, the
  vendor-neutral seam — US-4's schema, the non-gated bulk of US-5, US-2, US-3,
  and the non-gated portions of US-6/US-7 — still ships and is directly useful:
  it is what the requester actually asked for ("an interface that I can
  exchange… from a factory"). Only the concrete Codex runner itself fails to
  materialize, and every gated task names exactly what it is waiting on so this
  is never a silent partial failure.
- **Dependency graph / hard couplings.** US-5's five-`ReplyKind`s task is a hard
  prerequisite for US-2, US-3, and US-7 starting their own report-facing work.
  `naming.SessionRef`'s rename (US-4) must land with its byte-identical-guardrail
  test passing before any code that computes a session id runs — a mismatch
  there breaks every existing Claude conversation, not only new Codex ones.

## Cross-Story Constraints

- **Platform:** `internal/config`'s stdlib-only purity invariant is preserved —
  no TOML decoder for Codex's own `config.toml` enters `internal/config`;
  litefactory only ever reads/writes its own YAML config and passes flags/env to
  `codex`, never the reverse. Both `internal/agent` and `internal/codex` get a
  `doc.go` per the existing package convention; `internal/harness`'s existing
  `doc.go` gains a line stating it is now the Claude implementation of
  `agent.Runner`. Any future Codex scratch/`CODEX_HOME` path must go through a
  `config.Paths` method, never joined against `$LF_HOME` directly outside
  `internal/config`.
- **Security:** no test may spawn a real `codex` or `claude` binary — Codex
  testing extends `internal/turn/fixture_test.go`'s existing configurable-fake-
  Claude pattern rather than inventing a second fake-binary mechanism. The
  existing fixed env allowlist and the forbidden-flag guard both gain
  Codex-specific extensions (see Shared Decisions) rather than being loosened to
  accommodate a second vendor.
- **Tests:** `turn/gate_test.go` pins exactly three `CloneGate.Acquire` call
  sites today — a second runner implementation must not add a fourth.
  `surface_test.go`, `policy_test.go`, and `purity_test.go` are expected to need
  updates as a direct consequence of this design change; each story that
  touches one of them states which specific assertions are expected to change,
  so nobody "fixes" a test that is supposed to change. `config/template_test.go`
  must keep decoding the shipped template back to `Defaults()` after US-6/US-7
  add new config keys.
- **Ring-fenced:** `internal/repos`' aiteam-presence detection (the
  `CLAUDE.md`-only marker) is unchanged — onboarding Codex-only repositories is
  out of scope. Whether Codex's `/agent` subagents and hooks are rich enough to
  host litefactory's four-mode team is out of scope — US-1 records what exists;
  a later feature would design against it. Codex's own `config.toml` /
  `CODEX_HOME` contents are never read or written by litefactory.

## Story Index

| ID | Title | Size | Depends On | Sub-issue |
|----|-------|------|-----------|-----------|
| US-1 | Codex CLI probe | L | — | |
| US-4 | Store schema: tool-aware session identity | M | — | |
| US-5 | Vendor-neutral Runner interface + factory | L | US-4 (bulk); US-1 (3 gated tasks) | |
| US-2 | Per-turn label routing | M | US-5 | |
| US-3 | Mid-session tool-switch refusal | S | US-2, US-4 | |
| US-6 | Capability-tier vocabulary | M | US-5 (bulk); US-1 (1 gated task) | |
| US-7 | Per-tool permission profiles | M | US-5 (bulk); US-1 (2 gated tasks) | |
| US-8 | Root `AGENTS.md` | S | US-1 (soft, content only) | |

## Out of Scope

- OpenCode implementation (the named future third tool). The interface is
  *sized* for it — and that sizing is tested via US-5's third-tool sizing test
  — but no `internal/opencode` package is written.
- Multi-segment / multi-turn sessions and the resulting `new_segment`
  tool-switch policy. `refuse` is the shipped switch behavior; the schema is
  deliberately additive so this can change later with no further migration.
- Deciding whether Codex's `/agent` subagents and hooks are rich enough to host
  litefactory's four-mode team. US-1 records capabilities only.
- `internal/repos` accepting `AGENTS.md` as an aiteam-presence marker (onboarding
  Codex-only repositories).
- `internal/cli/router.go`'s "runs Claude Code in git worktrees" tagline —
  genuinely cosmetic, deferred until a second tool is usable end to end.
- Retiring the top-level `claude:` config key — kept as a deprecated alias for
  `agents.claude:`.
- litefactory reading or writing Codex's own `config.toml` / `CODEX_HOME`
  contents.
- Changing any specialist's actual model-tier assignment. US-6 moves the source
  of truth only (`opus → flagship`, `sonnet → balanced`); no behavioral change.

## Provenance

- Citation base: `794ab22`.
- Known-stale areas: every Codex-behavior claim not yet converted to `[V]` by
  US-1 remains `[W]` (second-hand, documentation-sourced) until
  `CODEX_CLI_CONTRACT.md` exists. Do not treat any argv shape, event field name,
  sandbox flag value, or env var name for Codex as settled before then. The
  architect review's finding that no `codex` binary exists on the reviewing
  machine is itself unconfirmed by a direct `which codex` (denied by that
  session's permissions) and must be re-checked in the actual target
  environment before being treated as settled.
- Re-pinning requirements: US-1 must re-confirm every `[W]`-tagged claim in
  `RESEARCH_BRIEF.md` before US-5's Codex-gated tasks, US-6/US-7's gated tasks,
  or US-8's content are finalized.
