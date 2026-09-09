# Approved delivery through a pull request

Separated workflow keeps task-branch saving in Implement. Once engineering/save
evidence, managed acceptance and every attached child are ready, the factory may
propose a concrete PR targeting `main`. It never switches modes automatically.

Normal readiness requires every currently attached child to be closed, including
unmanaged children and explicitly attached follow-ups. A managed child additionally
needs a retained accepted/owner-waived decision and completed acceptance projection;
manual remote closure cannot supply that evidence. Initial engineering/check
attribution and aggregate save evidence are checked independently. Readiness
exceptions match their exact recorded child, acceptance, evidence or aggregate
keys and preserve the original facts and reasons. Later children are not covered.

To explicitly request the readiness report or retry proposal delivery:

```
/deliver 1
```

Missing requirements produce specific blockers. Full exact blockers and exception reasons are retained in immutable readiness
records. Comments and PRs display bounded counts, sample stable obligation keys,
the full report digest and its record identity, so large scope stays reviewable
without truncating or silently losing the retained evidence. A complete proposal identifies
parent/generation, saved task branch, remote head, PR numeric identity, target main
and retained readiness report. PR creation does not authorize merging. Creation
uses a durable marker and numeric factory identity; uncertain responses reconcile
before another creation is considered. Existing factory PRs may be reused.
No generated PR title or body uses automatic issue-closing directives.

Only the numeric owner can explicitly approve the exact displayed proposal:

```
/merge 1 <64-character proposal digest> <PR number> <40-character head SHA> main
```

This approves integration and subsequent parent closure after actual confirmation.
A readiness waiver, earlier mode request, checkbox, manual closure or free prose
is not merge approval. Invalid/stale controls are visibly refused and consumed;
network/storage failures keep retry debt. A changed task/PR head requires a fresh
proposal and approval; historical story acceptance remains unchanged.

Delivery holds the same durable nonblocking session lock as task execution/save
and the parent progress lock, so approval cannot race a local task save. The
GitHub merge request also supplies the approved head SHA as its atomic guard.
Readiness and the exact PR/head/repository/main target are rechecked before merge.
GitHub provides no atomic expected-base parameter; the factory independently
checks target and merged state afterward and never closes the parent if those
facts cannot be confirmed.

PR creation, merge intent/confirmation and parent-close intent/confirmation are
separate durable effects. A successful API mutation response alone is not proof:
the factory independently reads a closed, merged PR with its exact approved head,
repository, intended main target and merge SHA before parent closure. Conflicts,
check restrictions, denied merges, changed targets and API errors remain pending
with diagnostics. Lost responses reconcile remote facts; confirmed operations and
factory comments are not duplicated. Confirmed parent closure marks the separated
feature completed; pending success-report delivery remains independently recoverable. Actual hosted integration occurs only when
the owner later supplies the concrete approval, not during implementation tests.
