# Plan Brief — litefactory-owned mode routing
# Branch: lf/32-change-the-way-how-modes-are-derected
# Date: 2026-09-02
# Parent issue: https://github.com/marioatcloudvane/litefactory/issues/32
# Citation base: 0a117b1f05d12aa6a5d888f021919afe43ca24d8
# Research brief: [RESEARCH_BRIEF.md](RESEARCH_BRIEF.md)

## Shared Decisions

- Litefactory owns the closed `research`, `plan`, `implement`, and `spike`
  vocabulary. Ordinary work agents receive exactly one concrete mode and never
  infer or change it.
- The canonical forge labels are `Mode: Research`, `Mode: Plan`, `Mode:
  Implement`, and `Mode: Spike`. They select one run and are removed only after
  that run has been reported and consumed.
- A slash command and label are two declarations of the same decision. Matching
  declarations agree; conflicting declarations refuse the run before any
  worktree or run row is created.
- An absent label and absent slash command invokes a dedicated, tool-free mode
  classifier owned by the configured `default_agent`. Classification is not an
  ordinary task run and creates no task worktree, segment, or run row.
- A durable selection record binds session, trigger, concrete mode, label, and
  cleanup state. It prevents duplicate classifier calls and makes label cleanup
  recoverable across process exits.
- `default_agent` uses the same canonical agent name and runtime registry as the
  `Coding Agent: <Name>` selector. Its compatibility default is `Claude`.

## Workflow

- Shipping order: US-1 establishes the vocabulary and explicit turn contract;
  US-2 makes the default agent configurable; US-3 adds classification; US-4
  adds targeted label operations and durable lifecycle state; US-5 integrates
  the complete daemon workflow and removes inference. Each stage leaves a
  testable seam for the next.
- Dependency graph / hard couplings: US-3 depends on US-1 and US-2; US-4 depends
  on US-1; US-5 depends on every preceding story. The production switch from
  `infer` must ship atomically with classifier fallback and durable label
  cleanup, so US-3 through US-5 are one release even though they remain separate
  review units.

## Cross-Story Constraints

- Platform: preserve the package dependency direction, keep SQL exclusively in
  `internal/store`, external process execution in the existing agent
  implementations, GitHub calls in `internal/github`, and workflow timing in
  `internal/daemon`.
- Security: issue text and repository routing text are untrusted input and must
  travel on stdin with existing environment allowlists and redaction; label text
  is only a lookup key and never reaches prompts, argv, or echoed refusals.
- Tests: keep deterministic table-driven tests, real SQLite for transactional
  claims, channel-based daemon synchronization, and the existing post-before-
  watermark guarantee. `make check` is the completion gate.
- Concurrency: classification and mode lifecycle consume the existing bounded
  worker slot and per-session in-flight guard; no unbounded goroutine or
  independent classifier pool is introduced.
- Failure semantics: an attempted external operation that fails is reported or
  retried as durable state; it is never converted into silence or an advanced
  watermark.
- Ring-fenced: no migration library, forge abstraction, new dependency,
  integration-test build tag, registered-clone write, `dist/` write, or runtime
  `$LF_HOME` edit belongs to this work.

## Story Index
| ID | Title | Size | Depends On | Sub-issue |
|----|-------|------|------------|-----------|
| US-1 | Make mode a typed, explicit turn input | M | — | https://github.com/marioatcloudvane/litefactory/issues/36 |
| US-2 | Configure and validate the default coding agent | M | US-1 | https://github.com/marioatcloudvane/litefactory/issues/37 |
| US-3 | Classify unlabelled requests through the default agent | L | US-1, US-2 | https://github.com/marioatcloudvane/litefactory/issues/38 |
| US-4 | Persist and mutate one-run mode labels safely | L | US-1 | https://github.com/marioatcloudvane/litefactory/issues/39 |
| US-5 | Integrate routing and retire agent-side inference | L | US-1, US-2, US-3, US-4 | https://github.com/marioatcloudvane/litefactory/issues/40 |

## Out of Scope

- Automatically advancing modes from artifacts or from work-agent output.
- Letting a work agent authorize a transition through a structured control
  message.
- Repository-specific mode vocabularies beyond the four established modes.
- Parallel or background classification outside the daemon's existing worker
  budget.
- Retroactively rewriting historical run rows whose mode is `infer`.

## Provenance

- Citation base: `0a117b1f05d12aa6a5d888f021919afe43ca24d8`.
- Known-stale areas: daemon dispatch, agent routing, plan sub-issue filing, and
  store migrations are active development areas and may move before Implement.
- Re-pinning requirements: before implementation, re-check the latest migration
  number, `daemon.Forge` and reporter boundaries, configured runner registry,
  prompt inference sites, and the current go-github label APIs.

