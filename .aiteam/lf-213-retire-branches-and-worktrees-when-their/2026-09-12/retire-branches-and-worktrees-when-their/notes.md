# Implement run 0006 notes

Resumed only frozen US-002 and US-003, preserving US-001 as a satisfied read-only dependency. Root verified existing branch state and executed build, focused checks per story and one aggregate gate in this run. No earlier output or timings were submitted. No production or test source changes were necessary.

All check references are story-prefixed, including the once-executed shared build under distinct immutable references: us002-build-r6, us002-scoped-r6, us003-build-r6, us003-scoped-r6. Root independently checked exact current bytes, hashes and log equality before helper preflight and submission.

Both engineering contributions are recorded and remote effects delivered: US-002 request a6a476067d4bf4b591430752b8c1c94d and US-003 request 895d43cf20f5ca7cd1b4078372efcb07. No acceptance decision or factory save is claimed.

The single make check completed with raw exit 2. Only the owner's waived pre-existing teamsource instruction-parity failures occurred; no subtree-timeout exception was needed. Formatting, vet, pinned lint and all other packages passed. Exact raw failures are retained in TEST_REPORT.md and run0006-executed/make-check.log, not recast as passed checks. No evidence was withdrawn because of these explicitly waived failures.

Existing conservative submodule and ignored-file preservation behavior remains. All destructive behavior was tested only through ordinary temporary dependency fixtures. No production cleanup, daemon/runtime state edits, commits or pushes occurred. No formal acceptance/code/architecture/plan review was commissioned. No scope questions or new design assumptions were needed.
