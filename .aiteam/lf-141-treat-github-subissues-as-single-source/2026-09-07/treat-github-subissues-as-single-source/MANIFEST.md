# Manifest
# Branch: lf-141-treat-github-subissues-as-single-source
# Session: 2026-09-07

## Modes

- [x] **Research** — `RESEARCH_BRIEF.md` *(done)*
- [x] **Plan** — `plan_brief.md` + five complete per-story publication bodies *(done; #143–#147 links retained; run 0004 body synchronization and #148 withdrawal pending)*
- [ ] **Implement** — code + tests *(runtime/tests verified in run 0006; make check and build passed; full completion blocked by unapplied protected Codex workflow parity)*
- [ ] **Spike** — `SPIKE.md` *(not started)*

Modes do not follow on from one another. A box is ticked by a run that a human
asked for; nothing here licenses starting the next one.

## History

- 2026-09-07 — session created; research requested for issue #141, run 0001.
- 2026-09-07 — Research completed for issue #141, run 0001; repository tour, applicable constraints and official GitHub API semantics evaluated in `RESEARCH_BRIEF.md`. Headless assumptions and unresolved Plan decisions recorded; no implementation or GitHub mutation performed.

- 2026-09-07 — Plan completed for issue #141, run 0002: feature-spec.md, ARCH_REVIEW.md (approved; no deviation), plan_brief.md and US-1.md through US-6.md prepared with 29 tasks. Headless assumptions recorded. GitHub publication and real Story Index links remain litefactory post-run work; Implement is not started.

- 2026-09-07 — Plan revised for issue #141, run 0003: owner clarified that each sub-issue must close immediately when its own verified implementation finishes. This supersedes the run 0002 save/push prerequisite; story completion is reported during the active parent run, separately from final delivery. Existing links to child issues #143–#148 retained. Updated publication bodies require synchronization to existing GitHub children; no GitHub mutation, implementation, commit or push performed.

- 2026-09-07 — Plan revised for issue #141, run 0004: owner removed story 6 and all legacy plan/story migration. Active plan now has five stories (US-1–US-5), 25 tasks and retained child links #143–#147. US-6.md was removed from the publication handoff; existing GitHub #148 is withdrawn from local scope but its remote disposition remains pending synchronization. New-workflow instruction/docs alignment and parity checks stay in existing tasks. Run 0003 immediate story closure and affected-story reopening remain unchanged. No GitHub mutation, implementation, commit or push performed.

## Notes

- Headless run: resolve ambiguity through stated assumptions; no questions or approval waits.

- 2026-09-08 — Implement resumed, run 0006. Independently verified local retained implementation and remote task-branch baseline; fresh build passed. Required checks, acceptance audit, documentation and drift accounting in progress.

- 2026-09-08 — Implement run 0006 verification finished: inspected retained implementation rather than restarting it; added three missing proposal rejection cases and fixed test-local lint; baseline rooted team reads/type naming cleanup and concurrent lock repair completed. Final make check passed (format/vet, pinned lint 0 issues, race tests 27 packages passed/0 failed); final go build and whitespace check passed. APP_CONTEXT/APP_CONSTRAINTS, CODE_REVIEW, notes and DRIFT_REPORT updated. Both preserved Codex parity patches pass read-only applicability check but cannot be installed in protected .codex; Implement remains incomplete for that environmental release gate. No forge mutation, commit, push or runtime deployment performed.
