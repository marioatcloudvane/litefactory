# Feature Spec: Headless self-awareness and escalation for named agents
# Status: APPROVED
# Date: 2026-09-02

## Goal
Five named agent/mode definitions currently describe asking as a live conversation that a headless run cannot have; reword them so each knows it runs headless and knows the one real route — stop and surface, litefactory relays to the issue — and remove a hardcoded personal name from an escalation template.

## User Stories

| ID | Story |
|----|-------|
| US-1 | As an agent author, I want one canonical wording for "headless run" and "asking is stopping", reusing the register already in `research-orchestrator.md` / `plan-orchestrator.md`, so that US-2..US-8 apply the same framing instead of five divergent inventions |
| US-2 | As `requirements-engineer`, I want my Phase 1-3 flow to state that no live person is present and that an unresolved gap is returned in my report for the orchestrator to relay, so that I stop scripting a self-dialogue with a user who is not there |
| US-3 | As `implement-orchestrator`, I want the four "pause and ask the user" instructions replaced with headless framing plus a record-the-assumption-and-continue fallback, so that I stop promising an exchange this mode never gets |
| US-4 | As `implement-orchestrator`, I want a defined severe-reason escalation path — named triggers, named check points, a fixed final-message block, ends the run — so that a genuinely severe blocker reaches the owner instead of being improvised past |
| US-5 | As `implement-orchestrator`, I want Phase 2 test-environment setup resolved from the environment rather than from a fill-in-the-blank dialogue, keeping the production-URL refusal, so that the phase works headless and never runs tests against production |
| US-6 | As Spike mode, I want a "blocked, needs a decision" case alongside the existing "hand back rather than grow" case, so that a spike that cannot proceed stops and states the decision needed rather than guessing |
| US-7 | As `proxy-product-owner`, I want conditional escalation wording — not asking by default, but knowing the route when there is a real reason — so that "note it, ask if it's in scope" cashes out to something that actually happens |
| US-8 | As `go-architect`, I want an explicit escalation trigger for a major deviation from the plan or spec, so that a deviation reaches the owner via my report instead of a general Q&A habit |
| US-9 | As the repo owner, I want the hardcoded personal name removed from the architect's deviation template, so that no agent definition names a specific individual |

## Data Model

No runtime data model. This is a prose change to markdown agent/mode definitions. The artifacts touched:

| Artifact | Section touched | Change | Required | Constraints |
|--------|-------|------|----------|-------------|
| `.claude/agents/requirements-engineer.md` | header intro (L9-24), Phase 1 (L26-28), Phase 2 (L30-46), Phase 3 (L48-61), Reporting back (L109-113) | rewrite | Yes | US-2. Keep the 3-phase structure and the checklist; only the asking mechanic changes |
| `.claude/agents/implement-orchestrator.md` | L11, Gate A (L171), Phase 1.5 cycle exhaustion (L198), Phase 4 escalation (L292), Exit drift (L325), Detour rule (L335-341) | rewrite in place | Yes | US-3, US-4 |
| `.claude/agents/implement-orchestrator.md` | new `## Escalating` section | add | Yes | US-4. Placed after *The agents you invoke are foreground operations*, before Phase 1 |
| `.claude/agents/implement-orchestrator.md` | Phase 2 (L203-234) | rewrite | Yes | US-5. Prod-pattern list at L223-225 is retained verbatim |
| `.claude/modes/spike.md` | *Hand back rather than grow* (L32-43), *When you are done* (L58-61) | extend | Yes | US-6. One added case, not a new top-level section |
| `.claude/agents/proxy-product-owner.md` | Edge cases (L119-120), Reporting back (L125-127) | reword + extend | Yes | US-7 |
| `.claude/agents/go-architect.md` | Deviation block (L206-217) | extend | Yes | US-8 |
| `.claude/agents/go-architect.md` | L214 `Decision needed from: Mario (human)` | replace | Yes | US-9. Same block as US-8 — apply US-8 first, then US-9, or land both in one edit |

### Relationships
- US-1 → US-2..US-8 via the canonical wording it fixes; US-1 produces no file change of its own, its output is the phrasing the other stories paste and adapt.
- US-8 → US-9 via `.claude/agents/go-architect.md` L206-217 (same code block, ordered edits).
- US-3, US-4, US-5 → same file, distinct non-overlapping line ranges; independently deliverable but must not be edited concurrently by separate agents.

## API / Interface Changes

The "interface" here is the vocabulary and the escalation block shape. Both are contracts other agents read.

| Method | Endpoint / Action | Auth | Notes |
|--------|------------------|------|-------|
| Tier 1 — run owner | Mode orchestrator or mode doc (`implement-orchestrator`, Spike) stops work and writes the blocker into its **final message**; litefactory posts that message as a comment on the issue | run-level, granted by litefactory | Never waits, never polls, never claims permission to ask |
| Tier 2 — sub-agent | Sub-agent (`requirements-engineer`, `proxy-product-owner`, `go-architect`) stops its own work and returns the question/escalation in its **report to the invoking orchestrator**, clearly marked as needing an owner decision | none — sub-agents own no final message | The orchestrator decides whether to end the run with it. A sub-agent never states it can pause the run itself |
| Canonical phrase A | "This run is headless. No live person is reading it and nothing can be answered inside this run." | — | Used by all of US-2..US-8 |
| Canonical phrase B | "Asking is stopping." — the question goes into the message that leaves this run; the owner replies on the issue; a later run picks it up | — | Wording already established in `research-orchestrator.md` L265-268 and `plan-orchestrator.md` L217; reuse it, do not invent a synonym |
| Escalation block (US-4) | Fixed shape in `implement-orchestrator`'s final message: `## Escalation` → what stopped / which phase, story, task / which trigger fired / what is already done and left in the worktree / what decision is needed / options if any | — | One block covering everything found, never one message per item |

## Business Rules

- **No `.go` file is changed by this work.** `modeMayAsk`, the `lf: ask` tag, the Implement-mode refusal and the injected headless/asking prompt text all stay exactly as they are.
- **"Ask via litefactory" never means a Go permission change.** For modes the harness gags (Implement, Spike) it means: do not ask and do not wait — stop, state the blocker and the decision needed, end the run. That is compatible with the injected header, which already instructs a gagged run to report what it assumed; this extends it to reporting what blocked it.
- **No new files.** No shared include, no `.claude/shared/*`, no `ARCHITECTURE.md`. The canonical wording lives in this spec and is written into each of the named files directly.
- **No litefactory internals in the markdown.** No Go constant names (`headlessRule`, `askingRule`, `modeMayAsk`), no `internal/**` paths, no line-number citations. Describe the behaviour only. This is also what keeps the edit from going stale.
- **Lightweight edits.** Each file keeps its existing structure, headings and length profile. No file is restructured or rewritten wholesale; US-2 is the largest edit and is still a section-level rewrite, not a new document.
- **No agent definition names a specific individual.** Refer to "the owner" or "the human".
- **Severity bar for US-4 escalation.** Escalate only when continuing would (a) require a decision that changes scope, architecture or the plan, (b) risk a destructive or unsafe action, or (c) leave a gate permanently unmovable. Everything below the bar → take the most reasonable reading, continue, record the assumption in `$session/notes.md` and in the final report.
- **US-4 check points are existing gates, not a new phase.** Gate A (Phase 1 `BLOCKED` needing a scope/architecture decision), Phase 1.5 (BLOCKING findings after 2 cycles), Phase 2 (no usable non-production test environment), Phase 4 (bug loop exhausted after 2 cycles), plus a missing `plan_brief.md` or `US-<n>.md` at startup.
- **US-5 resolution order.** Stories touching real dependencies → read `TEST_BASE_URL` and `TEST_DATABASE_URL` from the environment. Both set and the URL matches no production pattern → proceed. Otherwise → do not ask, do not invent a URL, do not create a database: skip integration tests, continue with unit tests, and record the skip and its reason as an item in the final report's escalation block.
- **US-5 keeps the safety property.** The production-pattern refusal list (`api.` without a staging/test/dev subdomain, `prod.` anywhere, the known production domain) and the safe list (`localhost`, `127.0.0.1`, `staging.`, `test.`, `dev.`, `-staging`, `-test`, `-dev`) survive the rewrite unchanged; a matching URL is refused rather than re-prompted.
- **US-7 is conditional, not default.** `proxy-product-owner` asks nothing in the normal case; it gains only the route and the bar for when there is a genuine reason.
- **US-8 is a trigger, not a Q&A pattern.** `go-architect` escalates on a major deviation from plan or spec. It does not gain `requirements-engineer`-style general questioning.
- **Files not named in this spec are not touched.** Adding self-awareness prose to any other agent is out of scope and is a failing outcome, not a bonus.

## Edge Cases & Error Handling

- Sub-agent has a genuine gap but its orchestrator has no permission to ask → the sub-agent still returns the gap marked as an open question; the orchestrator records it as an assumption and carries it into its report. Never silently dropped.
- `implement-orchestrator` hits two severe triggers in one run → one `## Escalation` block listing both, not two runs and not two messages.
- Severe trigger fires mid-phase with work already on disk → the escalation states what is complete and left in the worktree; the orchestrator does not revert, stash or clean up. litefactory commits what it finds.
- US-5: `TEST_BASE_URL` set but `TEST_DATABASE_URL` missing → treated as "not usable"; skip integration tests and report. No partial run.
- US-5: env var set to a production-matching URL → refuse, skip, and say in the report that the configured URL was refused as production-shaped. Never re-prompt, never fall back to a guessed safe URL.
- US-2: `RESEARCH_BRIEF.md` and `APP_CONTEXT.md` both absent → `requirements-engineer` does not "ask the user to describe the application"; it returns immediately with that as the blocking gap, since a spec cannot be written from nothing.
- US-2: gaps remain after the brief is read → they are listed in the report as open questions and the spec is still written on the most reasonable reading, with the assumption recorded. A run that produces no spec because it was waiting for an answer is a failed run.
- US-6: a spike is blocked rather than merely oversized → `SPIKE.md` is still written, with the blocker under `## Not done` and the decision needed under `## Recommendation`. A blocked spike never ends with no artifact.
- Rewording removes an instruction some other file cross-references (e.g. Phase 2's `.claude/skills/shared/environment-validator.md` pointer) → keep the pointer, reword the surrounding instruction only.
- An edit is tempted into changing the meaning of an adjacent rule (git/commit rules, mode-lock, One Run One Mode) → out of bounds; those paragraphs are untouched.

## Out of Scope

- Any change under `internal/` or to any `.go` file — `modeMayAsk`, the `lf: ask` tag mechanic, the Implement-mode asking refusal, the injected headless/asking prompt text.
- Adding self-awareness or escalation wording to the nine files not named here: `research-orchestrator.md`, `plan-orchestrator.md` (both already compliant), `go-implementation-engineer.md`, `go-code-reviewer.md`, `go-integration-test-engineer.md`, `go-unit-test-engineer.md`, `app-context-updater.md`, `codebase-tour.md`, `web-researcher.md`, `.claude/modes/session.md`.
- Changing `CLAUDE.md`.
- Correcting the stale `internal/report/run.go` line-number citations in `research-orchestrator.md` (real drift, separate issue).
- Giving Spike or Implement actual `lf: ask` permission.
- `ARCHITECTURE.md`, a `docs/` directory, or a `README.md` expansion.
- The `lf:tools=web` / tool-profile labelling question.
- Any new artifact, template file or shared include.

## Acceptance Criteria

- [ ] `grep -ri "mario" .claude/` returns no match.
- [ ] `.claude/agents/go-architect.md` L206-217's deviation block names the decision-maker generically ("the owner" / "the human") and states that the deviation is surfaced in the architect's report to its invoking orchestrator.
- [ ] `.claude/agents/go-architect.md` states an explicit escalation trigger for a major deviation from the plan or spec, and states that it is not a licence for general questioning.
- [ ] `.claude/agents/requirements-engineer.md` contains no fill-in-the-blank prompt template awaiting typed input — the `"...Is that right?"` template at L32-34 and the `Proceed?` block at L50-59 are gone or rewritten as report content.
- [ ] `.claude/agents/requirements-engineer.md` states that the run is headless, that it holds no permission to pause the run itself, and that an unresolved gap is returned in its report to the plan orchestrator.
- [ ] `.claude/agents/requirements-engineer.md` still writes `$session/feature-spec.md` on a run where gaps remain, with the assumptions recorded — no path through the file ends in waiting.
- [ ] `.claude/agents/implement-orchestrator.md` contains no instance of "pause and ask the user" (previously L11, L171, L292, L339-340); each site instead states either the record-the-assumption-and-continue fallback or the escalation path.
- [ ] `.claude/agents/implement-orchestrator.md` has an `## Escalating` section naming: the severity bar (three conditions), the five check points, the `## Escalation` final-message block shape, and the rule that escalating ends the run without waiting.
- [ ] `.claude/agents/implement-orchestrator.md` Phase 2 contains no dialogue script and no `>` input placeholders; it resolves `TEST_BASE_URL` / `TEST_DATABASE_URL` from the environment and skips-and-reports when they are absent or production-shaped.
- [ ] `.claude/agents/implement-orchestrator.md` Phase 2 still carries the production-pattern refusal list and the safe-pattern list, unchanged in content.
- [ ] `.claude/modes/spike.md` has a "blocked, needs a decision" case that writes `SPIKE.md` and states the decision needed in the final message.
- [ ] `.claude/agents/proxy-product-owner.md` L119-120's "ask if it's in scope" states where that question goes (its report to the invoking orchestrator) and that it applies only when there is a genuine reason.
- [ ] Every edited file uses canonical phrases A and B (or their established equivalents from `research-orchestrator.md` / `plan-orchestrator.md`) rather than a locally invented synonym.
- [ ] No edited file cites a Go constant name, an `internal/**` path, or a source line number.
- [ ] `git status --short` shows changes only to the five files named in the Data Model table; no `.go` file and no new file appears.
- [ ] `make check` passes (unchanged — proves no Go file was touched).
