# Plan Brief — Failed runs get committed under their failure text
# Branch: lf/126-failed-runs-get-committed-under-their
# Date: 2026-09-06
# Parent issue: github:marioatcloudvane/litefactory#126
# Citation base commit: 878b12a
# Research brief: .aiteam/lf-126-failed-runs-get-committed-under-their/2026-09-06/failed-runs-get-committed-under-their/RESEARCH_BRIEF.md

This is a headless run: the requirements engineer and this synthesis both proceeded
without a human in the loop, resolving the brief's open questions from the product
decisions the issue owner already gave in a comment, and resolving two further
questions (VF-1/VF-2/VF-3, VF-4/VF-5) by reading the code rather than leaving them
open. Those findings are recorded in full in `feature-spec.md`'s Verified Facts
table and are treated here as settled context, not open items.

## Shared Decisions

- **A non-success run's worktree is not committed at all** — no placeholder, no
  WIP-marked commit. The work is not lost: it stays in the worktree and the next
  run on the same task reuses that worktree, where its own commit step publishes
  it. A placeholder commit would keep the false-history problem alive in a weaker
  form — a commit on the branch that no successful run authored. Binds US-1, US-2,
  US-3.
- **Resume after a failed run stays enabled.** Disabling it would discard real
  conversational context to fix what is actually a labelling problem, and would
  make every transient failure restart cold. Instead, a resumed run is told,
  explicitly, that its predecessor failed and that the predecessor's claims are
  not this run's accomplishments. Binds US-4, US-5.
- **Two distinct `Succeeded()` predicates exist and must never be collapsed or
  converted into each other.** `agent.RunResult.Succeeded()` (already existed,
  nil-safe pointer receiver) is the live-outcome predicate that drives the two
  turn-side commit gates (US-1, US-2). `store.Run.Succeeded()` (new) is the
  persisted-row predicate that drives resume-side plumbing (US-4). Different
  sources of truth, different lifetimes. A task that adds a conversion helper
  between them, or routes one through the other, has misread the plan.
  Corollary: US-1 and US-2 have no dependency on the new store method and can
  start immediately; only US-4 needs it.
- **No schema change.** `runs.status` and `runs.error` are already columns;
  `runs.error` is empty for exactly one outcome (success) and survives the move
  to `reported` status, which `status` alone does not. A new column would
  duplicate a fact the row already carries. No migration file is added.
- **`runs.error` gains a second reader.** It has been report-and-log text only;
  US-4/US-5 make it prompt input as well. That widening of its blast radius is
  documented in its own doc comment, and is exactly why the security guardrail
  below exists.

## Workflow

- Shipping order:
  1. **US-6** is fully independent — a self-contained rendering fix with no
     shared files and no ordering constraint. Ship it anytime, first or in
     parallel with everything else.
  2. **US-1 and US-2 ship as one unit, never US-1 alone.** `fileStoryIssues`
     (US-2) stages its write-back commit with `git add --all`. Landing US-1
     without US-2 relocates the bug rather than fixing it: a failed run's work
     would still get committed, now under litefactory's own message and
     attached to no runs row at all — strictly worse than the status quo.
  3. **US-3 ships after US-1/US-2.** Its entire point is to describe a state
     ("nothing was committed, the work is at `<path>`") that only exists once
     the commit gate is actually widened. Shipping it first makes the report
     claim something false.
  4. **US-4 ships before US-5.** US-5 renders the fields US-4 defines and
     populates; it can be written against those field definitions in parallel,
     but its end-to-end tests need US-4 landed first.
- Dependency graph / hard couplings: `{US-1, US-2}` is a hard-coupled pair →
  `US-3`. `US-4` → `US-5`. `US-6` has no edges to any other story.

## Cross-Story Constraints

- **Platform**: every edit lands inside an existing `internal/` package — no
  new module, no `pkg/` candidate, no `cmd/` change. No new import edge between
  `internal/report` and `internal/turn` in either direction. Nothing in this
  feature reads a clock; `internal/harness.BuildPrompt` must stay pure (no
  clock, filesystem, or env access) after the US-5 branch is added. `doc.go`
  files in `internal/turn` and `internal/harness` describe the behaviour this
  feature changes and must be updated as part of the change, not as follow-up.
  No `make build` and no writes into `dist/`; verification is `go build ./...`,
  `go test ./...` (`make test` adds `-race`), `go vet ./...`, `gofmt -l .`,
  collectively `make check`.
- **Security**: the invariant "agents never commit, only litefactory's own
  commit step does" is preserved exactly as-is — verified by reading
  `internal/harness/settings.go` and `internal/config/policy.go` rather than
  assumed (see `feature-spec.md` VF-1/VF-2). No story here grants, implies, or
  requires an agent-initiated `git add`, `commit`, `stash`, or `push`, and the
  Claude-path deny list plus the read-only git allowlist stay untouched. The
  Codex path has no equivalent deny list (VF-3) — that asymmetry is
  acknowledged, not closed by this plan; see Out of Scope. The one place this
  plan adds new attack surface is binding: `PreviousReason` (sourced from
  `runs.error`, which can carry up to 200 bytes of agent-authored text on one
  classify path) **must not** be interpolated raw into the resume header — it
  must be neutralised (`neutraliseNoteTags`, ideally `<note>`-framed and
  `capText`-bounded like every other quoted body in the prompt) before it
  reaches the child process. `excerpt`'s 200-byte cap bounds length, not
  meaning, and is not sufficient on its own. Full detail lives in US-5's
  Architecture Notes.
- **Tests**: no mocking framework and no new module — hand-written fakes only,
  consistent with the rest of the tree. `internal/turn/gate_test.go` (the test
  pinning which call sites acquire the clone gate) runs unmodified and must
  keep passing — no site is added or removed by this plan. The resume header
  for a **succeeded** predecessor, and the cold-start prompt, must both be
  provably byte-for-byte unchanged from before this change — this is a hard
  regression bar, not an aspiration, because the insertion point chosen for the
  US-5 corrective note is the only one that preserves that guarantee. `make
  check` (fmt, vet, lint, test) must be green at the end, including the
  existing abandoned-run, no-changes, and push-failure tests in `internal/turn`
  and the outcome-rendering tests in `internal/report`.
- **Ring-fenced**: `turn.Result.Succeeded()` (`internal/turn/result.go:236`) is
  load-bearing for session state, `Reportable()`, and `ConsumesComment()` — no
  task in this plan touches it; a task that finds itself editing it has left
  the plan. `internal/harness/prompt.go`'s `"- Mode: %s\n"` literal is trusted
  verbatim by this repository's own `CLAUDE.md` `## Classify` step and no test
  catches that literal drifting — it is not to be touched by US-5's insertion.
  Hardening the Codex sandbox to match the Claude git-verb deny list is
  explicitly out of reach for this plan (separate work, tracked by VF-3).

## Story Index

| ID | Title | Size | Depends On | Sub-issue |
|----|-------|------|-----------|-----------|
| US-1 | A non-success run commits nothing | M | — | US-1.md |
| US-2 | The sub-issue phase skips a non-success run | S | US-1 | US-2.md |
| US-3 | The report says plainly nothing was committed, and where the work is | S | US-1, US-2 | US-3.md |
| US-4 | A resumed run knows its predecessor failed | M | — | US-4.md |
| US-5 | The prompt states the predecessor failed and disowns its claims | M | US-4 | US-5.md |
| US-6 | An unknown cost renders "unknown" | XS | — | US-6.md |

## Out of Scope

- Any form of placeholder, WIP, or "failed run" commit for a non-success
  worktree.
- Disabling resume after a failed run, or any redesign of segment/resume
  semantics beyond making the predecessor's outcome visible to the prompt.
- Any change that grants, enables, or implies an agent-initiated `git add`,
  `commit`, `stash`, or `push`.
- Hardening the Codex sandbox to match the Claude-path git-verb deny list
  (VF-3 records the asymmetry; closing it is separate work).
- Token counting or token accounting of any kind — confirmed by reading the
  code that no token-count field exists anywhere (`agent`, `harness`, `store`,
  `report`, `cli`); adding one would be a new feature, not this bug fix.
- Reconciling the two existing outcome vocabularies (`agent.Outcome` /
  `harness.Outcome` versus `store.RunStatus`), and adding any durable outcome
  column — the columns already stored are sufficient.
- Retrying, auto-resuming, or otherwise reacting to a failed run beyond
  labelling it correctly for the next run.
- Changing what `resumeDecision`/`terminalRunStatus` treats as a resumable
  predecessor.
- Redaction or sanitisation changes to `ResultText` on the success path — it
  keeps its existing `Litefactory-` trailer stripping and is untouched by this
  plan.

## Provenance

- Citation base: `878b12a` (the commit that landed `RESEARCH_BRIEF.md`).
- Known-stale areas: none identified — the architect re-verified every fact in
  the spec's Verified Facts table (VF-1 through VF-7) against the source in
  this same session and all held character-for-character.
- Re-pinning requirements: none beyond the standard implementation-time
  re-read of each edit site immediately before it is changed, since this is a
  same-session citation with no elapsed drift window.
