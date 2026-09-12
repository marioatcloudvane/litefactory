# Implement run 0004 notes

Implemented frozen US-001, US-002 and US-003 only, separated workflow 2. Production work and scoped tests complete; aggregate verification incomplete. No questions were asked and no acceptance/code/architecture/plan review performed.

Assumptions retained: uncertain ownership/default identity or submodule contents keep resources; an uncertain old intent encountering a present resource is reconciled without deleting it that pass; a future pass evaluates afresh. Daily sweep processes one owner per Tick after normal dispatch with a 30-second owner budget. It reuses existing task/clone coordination and worktree recreation. No automatic archival. Narrow SEC-7/S-6 policy amendment was explicitly approved.

No production cleanup, daemon restart/runtime edit, dist write, commit/push, forge mutation or mode transition occurred. Fixture Git mutations were confined to temporary origins/worktrees.

Aggregate gate did not complete within bounded execution after two fix/retest cycles. Previous scoped captures are historical only after subsequent edits. Invalidated contribution request IDs: US-001 ffa4a5f8be4542ff6399ff6c5249dc3a; US-002 781b1b7b7a5166041f589d8cb38e7919; US-003 2eadd905aca800c6a15cd1d0305979bb. Input valid, engineering rejected, acceptance not applicable, remote effects pending. Do not claim Validate readiness or successful save.


## Implement run 0005 — resume authority and assumptions

The owner requests verification and fresh engineering evidence for US-001/002/003, not reimplementation. Current HEAD 7d037854a135602e6200c178ecb1d18fc12db2a1 contains 57dbe11 and the factory main merge. Prior reported tests are not used as fresh results. Fresh go build ./... exited 0. No source changes were identified by US-001 and US-002 engineering self-checks.

The owner explicitly grants an aggregate make check exception only for pre-existing internal/teamsource instruction-parity failures and TestRunTimeoutKillsTheWholeSubtree under parallel load. Those failures, if observed, remain failures and are recorded separately from passes; they do not withdraw scoped engineering. Other failures require diagnosis. The complete gate will run once after targeted story checks.

Conservative existing behavior is retained: uncertain submodule/local content is kept. No production cleanup, daemon state edits, instruction-tree updates, commit/push or acceptance review is performed.

Final aggregate make check exited 2 only for the explicitly waived teamsource parity group (four top-level tests, 23 failing subtest lines). All other packages passed, including turn 1113.534s and daemon 722.212s. No subtree-timeout failure occurred. No aggregate rerun was performed. Evidence recording for US-002/003 remains blocked by the pending first US-002 request; no passing scoped evidence was withdrawn.
