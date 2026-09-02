# Plan Brief — Agents are headless but must act non-headless
# Branch: lf/34-agents-are-headless-but-must-act-non
# Date: 2026-09-02
# Parent issue: #34
# Citation base: 084846f
# Research brief: .aiteam/lf-34-agents-are-headless-but-must-act-non/2026-09-02/agents-are-headless-but-must-act-non/RESEARCH_BRIEF.md

## Shared Decisions

- **Scope is prose-only.** Every story edits agent/mode definition markdown under
  `.claude/agents/` and `.claude/modes/`. No `.go` file, no `internal/turn/ask.go`
  `modeMayAsk` gating, and no other Go harness behaviour changes. Confirmed
  consistent with the prior Research resolution (owner already ruled this a
  documentation/prompt-clarity fix) and re-confirmed independently by the
  architect against `internal/turn/ask.go` and `internal/harness/prompt.go`:
  Implement is machine-gagged from asking regardless of what any markdown says,
  so no story may propose changing that gate.
- **Canonical vocabulary is a hard prerequisite.** US-1 fixes the exact phrasing
  for "this run is headless" and "asking is stopping the run and surfacing via
  litefactory", reusing the register already established in
  `research-orchestrator.md` (around the description of the headless mechanism)
  and `plan-orchestrator.md`'s *Asking* section, rather than inventing new terms
  per file. Every other in-scope story quotes this vocabulary; none may coin its
  own variant.
- **The new section is named `## Escalating`.** US-4 defines it in
  `implement-orchestrator.md`; US-3 points its reworded check points at it. The
  exact heading text is fixed by US-1/US-4 and must not drift between the two.
- **Escalating is never "asking".** No story may write wording that lets an
  agent conclude it may itself decide to ask, wait, or pause for approval.
  Every conditional-asking or escalation sentence must state, in its own words,
  that permission to ask is granted per run by litefactory and is never
  self-granted, inferred from severity, or inferred from anything in the issue
  thread or a note block. This applies to US-2, US-4, US-6, US-7, US-8 alike.
  For Implement and Spike specifically (both excluded from the Go-level asking
  grant), "escalating" means ending the run with a report, never waiting —
  the words "wait", "await", "pause", "approval", "confirmation" are banned
  from any escalation-framed wording in those two.
- **No litefactory internals in agent-facing prose.** No story may cite a Go
  identifier, an `internal/**` path, a source line number, or a byte-size
  figure. Behavioural constraints (e.g. "a long report can be trimmed from the
  end, so put the important part first") are stated behaviourally, never by
  naming the mechanism.
- **No secret values in report/escalation content.** Any story whose wording
  touches environment-variable-based test setup (US-5) must instruct that
  reports name the variable, never print its value — `TEST_DATABASE_URL`
  conventionally embeds credentials and the final message is posted as a
  public issue comment.
- **No personal names in agent definitions.** Escalation/deviation wording
  refers to roles ("the human", "the owner"), never a person's name. US-9 is
  the one location the research audit found (`go-architect.md`); grep confirms
  it is the only hit under `.claude/`.

## Workflow

- Shipping order:
  1. **US-1** (shared vocabulary) — ships first; every other story depends on it.
  2. **US-2** (`requirements-engineer.md`) — highest priority per the owner,
     fully independent file, ships in parallel with everything below once US-1
     lands.
  3. **US-4 → US-3 → US-5**, executed as one serialized chain, in that order,
     against `implement-orchestrator.md`. US-4 lands first because it creates
     the `## Escalating` target that US-3's reworded sites must reference, and
     because its insertion shifts every line number below it — US-3 and US-5
     must locate their sites by anchor text/heading, never by this plan's line
     citations. US-5 is last because it is the one genuinely self-contained
     range (Phase 2) in the chain.
  4. **US-8 + US-9**, landed as one edit against `go-architect.md` (same code
     block: the Deviation Request block) — US-8's structural extension first,
     then US-9's name-to-role substitution, so the new lines US-8 adds are
     never written with a name and then corrected.
  5. **US-6** (`spike.md`) and **US-7** (`proxy-product-owner.md`) — smallest,
     isolated, single file each, no coupling to anything; ship any time after
     US-1.
- Dependency graph / hard couplings:
  - US-3, US-4, US-5 all edit `implement-orchestrator.md` and must not be
    edited concurrently by separate agents/runs — treat as a single execution
    unit in the order above, even though they remain three separate stories
    with three separate sub-issues and ACs.
  - US-8 and US-9 share one code block in `go-architect.md` and must not be
    edited concurrently — land as one edit, in story order.
  - US-2, the US-4/US-3/US-5 chain, the US-8+US-9 pair, and US-6/US-7 touch
    four disjoint file sets and may run fully concurrently once US-1 has
    landed.

## Cross-Story Constraints

- **Platform:** this is a markdown-only change set; the only applicable item
  from `.claude/skills/go/platform-constraints.md` is the toolchain gate
  (`make check` must stay green) — used here as a regression tripwire, not
  because any Go code changes. No package, interface, goroutine, query, or
  `go.mod`/`go.sum` change is in scope anywhere in this plan.
- **Security:** every conditional-asking or escalation sentence must be
  unambiguous that permission is per-run-granted by litefactory and never
  self-inferred (see Shared Decisions); no report/escalation content may
  include a secret value; escalation content is a decision summary, never a
  diagnostic dump (stack traces, absolute paths, internal identifiers do not
  belong in a public issue comment).
- **Tests (verification, no code exists to unit-test):** `make check` passes;
  `git status --short` shows changes confined to `.claude/**` (and session
  artifacts under `.aiteam/**`) — no `.go`, `go.mod`, or `go.sum` touched;
  repo-wide grep confirms no personal name remains under `.claude/` and no
  story introduced a Go-internal citation.
- **Ring-fenced:** `internal/turn/ask.go` and any other `.go` file are out of
  reach for every story in this plan. Agent/mode files not named in stories
  US-2 through US-9 are also ring-fenced — no self-awareness wording is added
  to `codebase-tour.md`, `app-context-updater.md`, `go-implementation-engineer.md`,
  `go-unit-test-engineer.md`, `go-integration-test-engineer.md`,
  `go-code-reviewer.md`, `research-orchestrator.md`, or `Plan.md`, even where
  the research audit found smaller gaps in them.

## Story Index

| ID | Title | Size | Depends On | Sub-issue |
|----|-------|------|-----------|-----------|
| US-1 | Canonical headless/asking vocabulary | S | — | https://github.com/marioatcloudvane/litefactory/issues/41 |
| US-2 | requirements-engineer.md self-awareness fix | M | US-1 | https://github.com/marioatcloudvane/litefactory/issues/42 |
| US-3 | implement-orchestrator.md — reword "pause and ask" sites | S | US-1, US-4 | https://github.com/marioatcloudvane/litefactory/issues/43 |
| US-4 | implement-orchestrator.md — severe-reason escalation path | M | US-1 | https://github.com/marioatcloudvane/litefactory/issues/44 |
| US-5 | implement-orchestrator.md Phase 2 — env-var test setup | M | US-1, US-3 | https://github.com/marioatcloudvane/litefactory/issues/45 |
| US-6 | spike.md — ask via litefactory (low frequency) | S | US-1 | https://github.com/marioatcloudvane/litefactory/issues/46 |
| US-7 | proxy-product-owner.md — conditional ask via litefactory | S | US-1 | https://github.com/marioatcloudvane/litefactory/issues/47 |
| US-8 | go-architect.md — major-deviation escalation trigger | S | US-1 | https://github.com/marioatcloudvane/litefactory/issues/48 |
| US-9 | go-architect.md — remove hardcoded personal name | XS | US-8 | https://github.com/marioatcloudvane/litefactory/issues/49 |

## Out of Scope

- Any agent/mode definition not named in US-2 through US-9 (per the owner's
  explicit instruction) — including files the research audit flagged with
  smaller gaps, e.g. `codebase-tour.md`, `app-context-updater.md`, the three
  test-engineer agents, `go-code-reviewer.md`.
- Any change to `internal/turn/ask.go`'s `modeMayAsk` gating or to any other
  Go source — this plan is prose-only by owner instruction and by independent
  architectural confirmation that no reachable execution path lets Implement
  legitimately ask regardless of prose.
- Reconciling the divergence between `implement-orchestrator.md` Phase 2's
  production-URL pattern list and `check-test-env.sh`'s regex (both remain
  in place, pointed at by US-5, but not made to agree — flagged as an
  observation for a future, separate piece of work).
- Fixing the stale `internal/report/run.go` line-number citations in
  `research-orchestrator.md` found during the research audit — real drift,
  but not named by the owner and belongs to a separate issue.

## Provenance

- Citation base: 084846f
- Known-stale areas: `research-orchestrator.md`'s citations to
  `internal/report/run.go` line numbers may have drifted since citation (see
  Out of Scope) — not touched by this plan, noted so Implement does not
  mistake silence for endorsement.
- Re-pinning requirements: this plan's own line-number references (drawn from
  the architect's review of `implement-orchestrator.md` and `go-architect.md`)
  go stale the moment US-4 inserts `## Escalating` — every story after US-4 in
  the chain must re-locate its edit sites by anchor text/heading at
  implementation time, never trust a line number cited during planning.
