# Manifest
# Branch: main
# Session: 2026-08-26

## Modes

- [x] **Research** — `RESEARCH_BRIEF.md` *(done)*
- [ ] **Plan** — `IMPLEMENTATION_PLAN.md` *(not started)*
- [ ] **Implement** — code + tests *(not started)*
- [ ] **Spike** — `SPIKE.md` *(not started)*

Modes do not follow on from one another. A box is ticked by a run that a human
asked for; nothing here licenses starting the next one.

## History

- 2026-08-26 — session created to re-run the external half of issue #5's research
- 2026-08-26 — `RESEARCH_BRIEF.md` written; blocking questions 1 and 2 then settled
  against the live API and recorded as an addendum. Plan tier is **Free**, which
  kills scope item (d) as designed.
- 2026-08-26 — Research done. `RESEARCH_BRIEF.md` written: the verified external half.
  All claims carry a URL and a 2026-08-26 retrieval date. Six of the predecessor's
  external claims were refuted (X-1..X-6), four confirmed and upgraded (X-7..X-10).
  Two refutations are load-bearing: Projects v2 has had a REST API since 2025-09-11
  (no GraphQL transport needed), and the top-ranked CODEOWNERS gate requires GitHub
  Pro on a private repo — this repo is private, on a personal account, plan unknown.
  Predecessor's F-1..F-14 and C-1..C-6 stand and are not restated.
  Blocking questions handed to Plan mode: account plan tier; sub-issues on a personal
  account; whether API version 2022-11-28 serves the projectsV2 endpoints.
  Includes "OD-1, restated" — a scoped recommendation for web tools in the headless
  tool surface, with the egress trade-off weighed. No configuration was changed.

## Notes

Predecessor: `.aiteam/lf-5-better-way-to-plan/2026-08-25/better-way-to-plan/RESEARCH_BRIEF.md`
(703 lines, merged as 4d9a843). Its Questions 2 and 4 are marked UNVERIFIED because the
headless run had no web tool — `--tools` in ~/.litefactory/config.yaml carries no
WebSearch/WebFetch (`internal/config/policy.go:26`), and `allowed_tools` qualifies Bash to
git/go only, so curl was denied too. That is lost decision OD-1 (F-6), and F-9 records the
run diagnosing its own blocker.

This session runs interactively, where both web tools are available, and re-does only the
external surveys. The codebase findings F-1..F-14 were read from the worktree and stand.
