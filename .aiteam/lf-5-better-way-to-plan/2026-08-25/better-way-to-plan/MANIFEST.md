# Manifest
# Branch: lf-5-better-way-to-plan
# Session: 2026-08-25

## Modes

- [x] **Research** — `RESEARCH_BRIEF.md` *(done — 14 findings; 2 of 5 questions unverifiable, web access disabled)*
- [ ] **Plan** — `IMPLEMENTATION_PLAN.md` *(not started)*
- [ ] **Implement** — code + tests *(not started)*
- [ ] **Spike** — `SPIKE.md` *(not started)*

Modes do not follow on from one another. A box is ticked by a run that a human
asked for; nothing here licenses starting the next one.

## History

- 2026-08-25 — session created for issue #5 "Better way to plan"
- 2026-08-25 — Research complete; `RESEARCH_BRIEF.md` written. Codebase tour over `cmd/` +
  `internal/` (186 Go files) and all 12 agent definitions; a git-forensics pass over
  `.aiteam/` (size census, churn, merge behaviour, plan-vs-shipped-code drift); two external
  surveys on GitHub's planning APIs and on OpenSpec.
  **Verified in-tree:** 14 findings (F-1..F-14) and 6 counter-arguments (C-1..C-6).
  Headlines: the four-mode process is substantially fictional — `/plan` and `/research` are
  refused by litefactory (`turn/command.go:18-21`) and have no `.claude/commands/` entries
  either; three incompatible plan templates; `go-architect` requires four documents that do
  not exist; the `$session` path has four spellings. 20% of all repo commits are pure
  planning churn with no code. A 503-line plan had a 12-minute shelf life. A 746-line plan's
  central "do not re-open" rule was contradicted by the shipped fix 5 hours later while its
  MANIFEST still reads *not started*. Two owner decisions (OD-1 WebSearch/WebFetch, D-CI-1
  session date) died inside a superseded file and were never shipped.
  **Root cause identified:** no mechanism exists by which a commit can invalidate a plan.
  **Correction made during the run:** a fairness claim (C-5, "plan-to-code drift is mostly
  small") was drafted from one spot-check, refuted by the full audit, and withdrawn in place
  rather than deleted.
  **Not verifiable:** `WebSearch`/`WebFetch`/network were disabled session-wide, so the
  GitHub-capabilities and OpenSpec sections are confidence-labelled recall with a verification
  checklist, not findings. That blocker was itself caused by the lost OD-1 decision.
  **Recommendation:** not OpenSpec as a tool (unverified, npm/Node into a pure-Go repo, likely
  collides with `CLAUDE.md`) — but adopt its living-spec/delta/archive idea as dependency-free
  convention. Use GitHub more via cheap REST already on the M1 roadmap (labels as projection,
  artifact permalinks, sub-issues, draft PRs, CODEOWNERS as the real gate); **not** Projects v2.
  Plans stay in-repo — `C-1`, no `--resume`, makes committed files the agent's only cross-run
  memory — but should shrink, archive, and stop outliving their PR.

## Notes

Request (issue #5 + triggering comment, @marioatcloudvane):
- Research how issues are planned here.
- We use GitHub; we should use more of GitHub's capabilities.
- Reason *against* how plans are done today — is it the right way?
- The user's own hypothesis: move to OpenSpec.
- Question: should plans live in the git repo at all, or somewhere else?
- Explicitly: "This is research mode now, nothing else!"
