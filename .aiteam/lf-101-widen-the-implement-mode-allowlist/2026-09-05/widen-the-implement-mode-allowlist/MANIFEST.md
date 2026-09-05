# Manifest
# Branch: lf/101-widen-the-implement-mode-allowlist
# Session: 2026-09-05

## Modes

- [x] **Research** — `RESEARCH_BRIEF.md` *(done)*
- [ ] **Plan** — `plan_brief.md` + per-story sub-issues *(not started)*
- [ ] **Implement** — code + tests *(not started)*
- [ ] **Spike** — `SPIKE.md` *(not started)*

Modes do not follow on from one another. A box is ticked by a run that a human
asked for; nothing here licenses starting the next one.

## History

- 2026-09-05 — session created
- 2026-09-05 — Plan requested (issue #101 comment "plan this out!"). Gate check
  failed: `RESEARCH_BRIEF.md` does not exist in this session. Per CLAUDE.md
  Gate rules, Plan cannot proceed without it and this run cannot skip the gate
  without explicit human approval (headless run, nobody to ask). No
  plan-orchestrator was invoked. No artifact was produced.
- 2026-09-05 — Research requested (issue #101 comment "ok research it first!").
  research-orchestrator invoked.
- 2026-09-05 — Research complete. `codebase-tour` mapped the tool-permission
  system (`internal/config/policy.go`, `internal/config/config.go`,
  `internal/harness/settings.go`, `internal/harness/runner.go`) and the
  "defence in depth, NOT the boundary" comment's occurrences. No web research
  needed — the design rationale is sourced from litefactory's own prior spike
  measurements (SD-2), not external docs. `RESEARCH_BRIEF.md` written with
  seven open questions, most notably: the exact target tool list and the
  386-denials evidence live only in the GitHub issue itself, not this repo;
  and the writable-scratch-directory ask has no existing implementation to
  extend. Recommended next step: run Plan mode on issue #101.
- 2026-09-05 — Research follow-up: new evidence from run 6 of #89 (a real
  implement run where an agent was refused 8 times across 5 strategies trying
  to delete its own scratch file, gave up and emptied it instead, and that
  emptied file was committed) added to the issue as a comment, targeting AC-3
  (workspace mutation) and AC-5 (scratch directory). A second `codebase-tour`
  pass confirmed "scope rm/mv to the worktree path" is new design work on two
  fronts (no path-scoped `Bash` rule precedent anywhere in this repo; the
  worktree path is not currently plumbed into settings construction). A
  `web-researcher` call to check whether Claude Code's settings schema even
  supports a path-scoped `Bash` qualifier returned no web access — the issue
  does not carry the `lf:tools=web` label. `RESEARCH_BRIEF.md` updated in
  place (not replaced): new evidence section, new Codebase Context findings,
  two new Open Questions (path-scoping feasibility pending the web-access
  label; whether the committed-dead-file workaround pattern needs its own
  safeguard), and the Recommended Scope section revised accordingly. Research
  remains complete/done; no mode change.
- 2026-09-05 — Research follow-up: a third issue comment argued the
  allowlist-widening approach is the wrong lever and proposed explicit
  `--permission-mode` selection instead (AC-7 through AC-11), asking that the
  original allowlist widening be demoted to a "fast path." A second
  `codebase-tour` pass and a `web-researcher` pass (this one, unlike the
  previous attempt, did have web access — the reason for the change was not
  established) evaluated the comment's four central claims against the repo
  and against Anthropic's official docs: (1) confirmed true — no
  `--permission-mode` flag is ever built in `runner.go`'s `args()`, sessions
  run in the CLI's hard-coded `"default"` mode; (2) partially true but
  overstated — the "constant in code, never a config key" line exists
  verbatim only inside a stale, unimplemented `.aiteam` planning session from
  a different feature, not as a decision this codebase currently honors; (3)
  confirmed accurate, with a real uncovered gap — `forbiddenFlag()` and
  `validateNoForbiddenFlag` exist as described but do not, today, check for
  `--permission-mode bypassPermissions`, so AC-9 names genuine new work; (4)
  unanswerable from the repo alone, then answered externally with primary
  Anthropic documentation — `auto` mode's classifier is real, and deny rules
  are documented to be evaluated before and unoverridable by the classifier
  (directly supporting AC-10), while the reporter's specific nested-subprocess
  "Blocked by classifier" anecdote remains unreproduced and undocumented
  anywhere found. `RESEARCH_BRIEF.md` updated in place with a new dedicated
  section, four new Constraints & Risks entries, four new Open Questions
  (#10-#13), and a revised Recommended Scope covering both proposals side by
  side. Research remains complete/done; no mode change.

## Notes

- Issue #101 reads as a well-scoped, evidence-backed ask (log-derived denial
  counts, a concrete allowlist diff, six acceptance criteria) that could
  plausibly go straight to Plan in a synchronous session where a human could
  waive the gate. In this headless run that waiver isn't available, so the
  run stopped rather than silently substituting Research or fabricating a
  brief.
- Research is now complete; `RESEARCH_BRIEF.md` exists in this session. Plan
  mode's gate is satisfied. A human comment is still required to start Plan.
- The `lf:tools=web` label is needed on issue #101 for a follow-up
  `web-researcher` pass to resolve whether Claude Code's own settings schema
  supports path-scoped `Bash` permission rules — this is currently the single
  largest unresolved technical question blocking a confident design for AC-3's
  "scope to worktree path" ask. **Update (third research pass):** a later
  `web-researcher` call in this same session did have web access for an
  unrelated question, so this may no longer be blocked — worth re-attempting
  before assuming the label is still needed.
- The issue now carries two candidate levers for the same underlying problem
  (headless hard-denials): widening the `allowed_tools` allowlist (original
  ask, AC-1..AC-6) and adopting an explicit `--permission-mode` per profile
  (new ask, AC-7..AC-11). Both are evaluated in `RESEARCH_BRIEF.md`; Plan mode
  will need to decide how they relate (combined story vs. sequenced) — see
  the brief's Open Question #10.

