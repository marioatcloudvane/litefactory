# Pre-cutover pending-state fixture

`pending.db.gz` was written by the store at commit
`eb72718bc6443c082b26150797f795b613559259`, before this story's uncommitted
ownership changes. `manifest.json` records the values read back by that same
implementation. All actors, paths, credentials references and provider URLs are
synthetic. No provider call or running daemon is involved.

The fixture contains a running graph attempt and immutable team capture, an
interrupted run, a possibly-sent report, a protected owner proposal awaiting
confirmation, a consent grant and owner generation. It also contains a newer
current ticket connection alongside the original retained connection version.

Regenerate from the pinned baseline:

```sh
python3 scripts/factory-pending-state.py generate
```

Rehearse opening and migrating a temporary copy with baseline code, current
code, then baseline code again, comparing every recorded value after each open:

```sh
python3 scripts/factory-pending-state.py rehearse
```

Both commands extract the baseline with `git archive` into a temporary directory;
they never switch branches or create a worktree. Each child command has a finite
timeout. The rehearsal does not modify the checked-in fixture. Go may download
module dependencies. The command implementation is copied into the baseline
snapshot and uses its public store operations.

`TestFactoryPrechangePendingStateRecovery` opens a copy with the current store,
checks retained values, refuses completion without delivery evidence, confirms
and completes the original report without allocating a new run, and checks that
revoking the saved grant still fences the protected action. The separate
same-schema restart test covers additional restart behavior.

This is offline storage compatibility and recovery evidence. It does not prove
live daemon stop/drain, provider availability, uninterrupted upgrades or recovery
of every possible historical state. The AC-004 daemon test covers provider
publication recovery through controlled GitHub and Linear APIs.
