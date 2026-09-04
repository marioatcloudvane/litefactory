# Manifest
# Branch: lf/69-we-need-to-make-more-defaults-apply-on
# Session: 2026-09-03

## Modes

- [x] **Research** — `RESEARCH_BRIEF.md` *(done)*
- [x] **Plan** — `plan_brief.md` + per-story sub-issues *(done)*
- [x] **Implement** — code + tests *(done)*
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
- 2026-09-04 — Research mode re-verification run (post-`main`-merge).
  Author asked to re-check the prior finding against the branch's
  current state after `main` (attachments/documents/images feature,
  #33's US-3/US-4/US-7/US-8) was merged in. `RESEARCH_BRIEF.md` updated
  in place with a new Part 5: Part 4's finding and recommended fix are
  confirmed unchanged in substance — `CLAUDE.md`'s Classify section still
  unconditionally re-infers mode, same line numbers (`:15-22`, Gate
  `:31-43`); `AGENTS.md`'s deferral is unchanged (`:41-44`); the
  `"- Mode: %s\n"` literal and its test pins are unchanged in content but
  the emitting functions moved (`writeHeader` now `prompt.go:477-525`,
  emission at `:480`; `writeResumeHeader` now `:621-663`, emission at
  `:623`; `writeInferGuidance` now `:680-691`) because new attachment-
  handling code was inserted earlier in the file. No new routing-
  competing file was introduced by the merge. Net effect: no re-scoping
  needed, but `US-2.md`'s T-2/AC-9 (and `US-1.md`'s `prepare.go`
  citation) reference stale line numbers that should be refreshed before
  implementation. Not diff-verified against `0df1fb4` (no diff tool
  available to the sub-agent) — read-and-confirmed only, flagged as a
  caveat in the brief.
- 2026-09-04 — Implement mode completed. Both stories delivered in one
  run, comment/documentation-only, no Go behavior change: US-1 inserted
  the mode-header trust precondition at the top of `CLAUDE.md`'s
  `## Classify` section (steps 1-3, the "sounds like" table, `## Gate`,
  and `AGENTS.md` all confirmed byte-for-byte/zero-diff unchanged); US-2
  added cross-reference comments at the `"- Mode: %s\n"` emission sites
  in `internal/harness/prompt.go` (`writeHeader` line 480,
  `writeResumeHeader` line 630 — located by content search per the
  re-verification's stale-line-number caveat, not by the cited numbers).
  Code review verdict ADVISORY_ONLY (2 non-blocking advisories, recorded
  in `CODE_REVIEW.md` and `notes.md`; no BLOCKING findings, no rework
  cycle needed). No real-dependency stories, so Phase 2 environment setup
  and integration tests were both skipped. Unit test verification: 974
  passed, 0 failed, `gofmt`/`go vet` clean, no test added/modified/removed
  (per plan's explicit "no new test" decision — existing literal pins
  confirmed intact). No bug loop needed. `DRIFT_REPORT.md` verdict: CLEAN.
  `APP_CONTEXT.md`/`APP_CONSTRAINTS.md` updated (also opportunistically
  fixed the previously-known-stale package map: `internal/agent`,
  `internal/codex`, `internal/planbrief`, `internal/attach` now listed;
  added `STYLE-8` documenting the CLAUDE.md/prompt.go literal contract).
  Code left uncommitted in the worktree per team convention; litefactory
  commits and pushes after this run ends.

## Notes

