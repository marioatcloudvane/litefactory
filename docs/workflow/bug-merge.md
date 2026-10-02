# Bug integration

`lf_bug_merge` consumes the immutable active delivery review and its exact owner
approval. `EnsureBugMerge` snapshots both owner authorizations and the complete
repair/check chain before forge observation. An intent does not authorize a
mutation. `ClaimBugMerge` revalidates current approval, completed source, reliable
three-phase child evidence, publication, feedback and cancellation under the
execution revision, and atomically claims exclusive task work before dispatch.

The existing forge merge operation uses method `merge` and the approved head.
Branch-protection or merge-method refusal never selects another method. Recovery
of a dispatched operation reads the same PR and does not repeat a mutation just
because the PR remains open. A definitive identified HTTP refusal is retained as
failed. `PrepareBugMergeRetry` requires that refusal, subsequent independent open
PR reconciliation, an explicit distinct factory continuation and current guards;
it only permits a later invocation to perform fresh prechecks. It contains no
retry loop. Transport errors and an absent reply remain uncertain.

Confirmation requires independent PR, merge commit and main observations. The
PR must retain its numeric identity, repositories, branch, head H and main
base. It must be merged and closed. The independently read merge commit M must
have ordered parents exactly [T,H], and main must be observed at M. Both exact
owner approvals must precede the forge's merge timestamp. Missing timestamps or
missing authorization do not become retroactive permission. The factory records
external integration without attributing a merge request to itself.

The factory-provided `Observe` callback checks source, target-commit recipe bytes,
environment and permission revisions before dispatch and during confirmation.
It must not require the old remote main tip after integration: the activity owns
that observation and expects the transition T to M. Production wiring must use
real observations; a nil observer is refused.

There is no atomic lock on remote main. A different merge base, incompatible
merge shape, main moving away from M, or a contradictory retained observation
creates an enduring exception. Subsequent main ancestry does not remove it.
Reads cannot detect every transient movement between observations. Neither
rollback nor exception waiver is provided.

`ConfirmedBugIntegration` is the read-only closure handoff. It returns the
immutable operation, both approvals, complete reviewed evidence and independent
integration observations only while local authority remains applicable and no
exception exists. It adds no new target-tip rule and performs no issue close.
Graph success, result receipts and a successful merge request cannot replace this
handoff. Actual issue closure, parent consumption and report publication remain
separate obligations.
