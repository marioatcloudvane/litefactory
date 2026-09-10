# Manifest
# Branch: lf-170-modes-should-stay
# Session: 2026-09-08

## Modes

- [x] **Research** — `RESEARCH_BRIEF.md` *(done)*
- [x] **Plan** — `plan_brief.md` + managed JSON proposal *(artifact complete; factory publication pending)*
- [ ] **Implement** — code + tests *(blocked: US-004 protected Codex-copy synchronization)*
- [ ] **Spike** — `SPIKE.md` *(not started)*

Modes do not follow on from one another. A box is ticked by a run that a human
asked for; nothing here licenses starting the next one.

## History

- 2026-09-08 — session created; Research requested by factory run 0001 for issue #170.
- 2026-09-08 — Research completed for run 0001; two-pass codebase tour synthesized into RESEARCH_BRIEF.md. No implementation or external research performed.

- 2026-09-09 — Plan requested by factory run 0002; continuing the explicitly supplied retained session. Proposal generation 1; publication belongs to the factory.
- 2026-09-09 — Plan artifact completed: feature-spec.md, ARCH_REVIEW.md, shared plan_brief.md and root version-1 managed JSON proposal with four create stories. No per-story Markdown, implementation, commits or pushes. Factory validation/publication and story-link replacement remain pending.

## Notes

- Headless run: do not ask questions or wait for approval; record reasonable assumptions and unresolved decisions.
- Working interpretation: selected mode persists between runs, agents may recommend transitions, and owners control changing modes unless Plan explicitly defines factory-validated agent requests.
- Main follow-up decisions: explicit transitions against retained labels; iterative asking permissions; discussion-only Plan outcome and readiness; old cleanup-row recovery; managed-scope label alignment.

- 2026-09-09 — Run 0003 begins Implement from four factory-selected frozen revisions; per-story gates and evidence precede dependent work. Base is now synchronized at 592131d.

- 2026-09-09 — Run 0003: US-001/002/003 implemented, independently reviewed, acceptance-tested and verified (latest events 000010/000011/000012). US-004 runtime tests pass, but AC-004-4/5 remain blocked by read-only .codex; event 000013 and CODEX_SKILLS.patch retain the exact remaining work. Build, make check and full race/coverage gates passed after reducing concurrency and increasing test timeout. Implement is not marked complete. No commits or pushes.

## Run 0013 — generation 2 implementation verification

Current Implement status: BLOCKED on US-004 AC-004-4 and AC-004-5 (protected Codex instruction copies). US-001–003 passed fresh scoped gates and evidence events 000001–000003 were published; event 000004 records US-004 blocked. Four other US-004 criteria passed runtime verification. Added true empty-classifier retirement coverage only; no production edits. Aggregate build, make check and full race coverage passed. See RUN_0013_VERIFICATION.md and RUN_0013_CODE_REVIEW.md for this run's evidence and remaining repair. Historical entries above are not assertions of current completion.

## Run 0014 — US-004 verification complete

Generation 2, factory run 294, selected revision 3deeef28baf33a3a96fadea379a733ac93228e3ca4450b25b3859b7be5133154. All six criteria passed fresh scoped checks/review/tests. Verified event 000001.json published immediately; final snapshot audit matched. Aggregate build, make check and full race coverage passed. US-001–003 retained as completed dependencies. Previous protected-copy blocker resolved in the supplied merged branch; this run corrected one lookup sentence only. Current selected Implement verification is complete; factory save/push/projection and parent delivery are not claimed. Details: RUN_0014_VERIFICATION.md.
