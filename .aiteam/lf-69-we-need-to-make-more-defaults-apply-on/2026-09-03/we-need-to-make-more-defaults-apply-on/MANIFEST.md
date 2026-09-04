# Manifest
# Branch: lf/69-we-need-to-make-more-defaults-apply-on
# Session: 2026-09-03

## Modes

- [x] **Research** — `RESEARCH_BRIEF.md` *(done)*
- [x] **Plan** — `plan_brief.md` + per-story sub-issues *(done)*
- [ ] **Implement** — code + tests *(not started)*
- [ ] **Spike** — `SPIKE.md` *(not started)*

Modes do not follow on from one another. A box is ticked by a run that a human
asked for; nothing here licenses starting the next one.

## History

- 2026-09-03 — session created
- 2026-09-03 — Research mode completed; RESEARCH_BRIEF.md written. Agent
  defaulting confirmed already solved; mode defaulting confirmed only
  partially solved (LLM-classified fallback, no static default, no
  session-state shortcut). Several other onboarding/usage-friction gaps
  identified (silent unassigned-issue path, no agent-tooling detection at
  `init`, no user-facing label docs).
- 2026-09-04 — Research mode follow-up run; RESEARCH_BRIEF.md updated in
  place. Author closed open questions 1, 3, 4, 5 (agent-tool sense
  confirmed; init agent-detection declined; silent unassigned-issue
  behavior confirmed correct-by-design; empty-body doc-comment skipped) and
  confirmed question 2's premise (LLM-classification fallback is the
  intended mode-defaulting design). New Part 4 finding added: confirmed
  true that `CLAUDE.md`'s Classify section unconditionally re-infers mode
  even when the daemon already resolved one via the `- Mode: X` prompt
  header; recommended a documentation-only conditional-trust fix in
  `CLAUDE.md`/`AGENTS.md`. Recommended scope for Plan mode narrowed
  accordingly.
- 2026-09-04 — Plan mode completed (headless run; author had already closed
  all open questions in the research brief). requirements-engineer wrote
  `feature-spec.md`; go-architect reviewed it and recommended collapsing the
  spec's four candidate stories into two, since three of them are one
  paragraph in one file and cannot be authored, reviewed, or shipped
  independently. Wrote `plan_brief.md` plus two per-story sub-issues:
  US-1 (`US-1.md`) — insert a precondition branch at the top of
  `CLAUDE.md`'s `## Classify` section that trusts the daemon-resolved
  `- Mode: X` prompt header when present, skipping steps 1-3, while
  leaving `## Gate` and the existing fallback classification untouched;
  no `AGENTS.md` edit needed (confirmed it already defers unconditionally
  to `CLAUDE.md`). US-2 (`US-2.md`) — comment-only cross-reference at
  `internal/harness/prompt.go:423`/`:566` documenting that `CLAUDE.md`
  depends on the exact `- Mode: %s\n` literal, with no new test (existing
  tests already pin the literal). Documentation-only change throughout;
  no Go behavior change. Story Index links pending sub-issue creation by
  litefactory.

## Notes

