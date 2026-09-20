# Implementation notes

- Run 0007 implements frozen US-001 and US-002 with dependency order preserved.
- Existing session retained. Referenced docs/workflow/session.md and docs/story-contract-instruction-manifest.md are absent; supplied workflow and submission contracts govern this run.
- Research-only production registration and activation defaults remain the approved scope; the second mode is test-only.
- No acceptance review or remote publication is authorized in Implement.
- User-required make check takes precedence over the skill recommendation against whole-repository race execution.

## US-001 engineering checkpoint

- Request a3ec3c473c942c85c1a5bd7bf5076e70, snapshot run563-us001-v1. Preflight complete/valid without diagnostics; matching receipt read.
- Input valid; engineering recorded; acceptance not applicable; remote effects pending. This is not publication or acceptance.
- Scoped race tests passed across turn, store, teamsource and daemon; pure policy/role unit tests passed.
- One overly broad unit regex selected unrelated TestShared turn tests and was interrupted at 237.419s. The exact intended declaration test passed afterward; the interrupted selection is not passing evidence. Aggregate make check still must execute the repository suite.
- US-002 started only after the US-001 engineering checkpoint. Shared-file changes will require refreshing story snapshot evidence.

## US-002 engineering checkpoint

- Request 95337f898b49ddea6a4ad181e04ac6d9, snapshot run563-us002-v1. Preflight valid/complete with no diagnostics; matching receipt read. Input valid, engineering recorded, acceptance not applicable, effects pending.
- Generic mechanics retain mode policy; the explicit scoped brief effect now gates Research publication and continuation.
- Second-mode tests found and drove fixes for a Research-only meter fence and historical owner wait interfering with later roles.
- Assembled Research tests found worker-created approval and changed-candidate approval defects; authenticated displayed-candidate authorization now rejects both and owner revision requests.

## Current-byte engineering refresh

- Factory preflight required explicit invalidation before replacing already recorded unaccepted engineering. Old checkpoints were invalidated with their cause preserved, then final-byte completions were submitted. No historical acceptance existed or was changed.
- US-001 current request: 51aa204da1fa84644fd44c0bc29f631f; snapshot run563-us001-v2; check us001-final-scoped-v2.
- US-002 current request: a0d73a1fb07c22268b340b7c183e4cbc; snapshot run563-us002-v2; check us002-final-scoped-v2.
- Both current snapshots: aba06d126e1d460e6f98f4c0ddf52351d834edeeb596d7c290f9f3347fba70a9, 52 implementation/test/documentation files. Preflights valid and complete; matching receipts read. Input valid, engineering recorded, acceptance not applicable, effects pending.

- Aggregate fix cycle 2 updates only the static production-dispatch invariant to the approved shared gateway plus Research-only entry. The exact corrected check passes; no runtime behavior changed.

## Final engineering state (2026-09-21)

- Both frozen stories are engineering complete. Final make check, go build ./... and empty gofmt -l . passed; no unresolved engineering failures or scope changes remain.
- Aggregate fix cycle 2 changed only the static dispatch-owner test. Fresh full scoped verification passed (turn 84.623s, plus six other packages and separate shared policy checks).
- Current US-001: snapshot run563-us001-v3, check us001-final-scoped-v3, request 49c059bd59f7337db4cb06a3abbd2863.
- Current US-002: snapshot run563-us002-v3, check us002-final-scoped-v3, request e8c6fd4fd80a74931a232ba13309b8aa.
- Both current story snapshots contain 53 files with digest 280bfefd826aeb5af6da03ec3b50e5dbb44cfcd9aa6c3b0b372281c1024f8c5f. Matching receipts read: input valid, engineering recorded, acceptance not applicable, effects pending.
- Final aggregate capture must include all changed files, including session reports. Reports are finalized before capture so no subsequent artifact edit invalidates retained aggregate evidence; the final aggregate receipt is returned in the application handoff.
- No commits, pushes, daemon-state edits, protected instruction-tree changes, acceptance review, issue closure or provider observation were performed. The factory retains save/final application ownership.
