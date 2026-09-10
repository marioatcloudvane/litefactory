# Run 0014 verification — modes-should-stay

Factory run 294; generation 2; parent github:marioatcloudvane/litefactory#170.
Starting worktree clean at 90e29f1. Selected scope US-004 (#191), issue ID 5399420651, revision 3deeef28baf33a3a96fadea379a733ac93228e3ca4450b25b3859b7be5133154. US-001–003 are factory-supplied completed read-only dependencies; no rework or new outcomes for them.

## Verified this run

All six US-004 criteria pass fresh verification. Scoped race tests: turn 27.125s, harness 2.183s, codex 1.938s. Additional readiness/frozen/completed-story checks: turn 25.103s, store 2.701s. Existing fake runners and migrated SQLite fixtures only; no real agent invocation or forge/runtime-state access.

Current Claude/Codex managed Plan contract blocks agree for the three affected roles. Harness-specific workflow links intentionally differ. Independent scoped and aggregate review is CLEAN: question permissions, identity-bound context discovery, cold/resumed/fallback parity, Plan handoff/advice, root/source/copy consistency and the updated engineering-event workflow fixture preserve all six requirements. The reviewer consumed recorded test outcomes without running tests or writing files.

## Changes made in this run

Corrected APP_CONTEXT's stale byte-equality claim to describe semantic agreement with distinct harness-specific workflow links. No production, test or protected-tree edits. The merged implementations and fixture changes were inspected, not claimed as work authored in this run.

## Evidence transport

Published .litefactory-story-events/294/000001.json immediately after scoped checks/review/tests. It binds all six exact AC identifiers and passing review/test entries to 78 snapshot files with digest b1a3ceaab6d2f386628ce5cc3ac700e83b2c2fcaba46b54c91d210f89265f42c. Transport is transient and must not be committed. The factory owns evidence ingestion, issue projection and save/push; those external outcomes are not asserted here.

## Aggregate gates

- go build ./...: passed.
- make check: passed, exit 0; formatting, vet, pinned lint (0 issues) and full race tests across 30 tested packages. Turn completed in 732.224s.
- go test -race -cover ./...: passed, exit 0; 30 tested packages. Turn completed in 723.744s with 79.0% statement coverage.
- Independent aggregate review: CLEAN, no blockers or advisory findings.

No remaining US-004 criterion gap was found. Final snapshot audit matched every published file byte and the manifest digest. Local origin/main is an ancestor of the current head. No commit, push, merge or forge projection was performed by this run.
