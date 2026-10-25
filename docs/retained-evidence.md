# Retained workflow and repair evidence

Workflow and Doctor handoffs use the same versioned evidence manifest. The
manifest names the owning records and exact revisions. It is a reference to
captured data, not an instruction, permission, claim of truth, or approval to
execute a repair. Evidence recovery never reruns commands or effects.

Workflow invocations receive an evidence pointer beside their existing result
helper. Its `retained-evidence/index-000000.json` names the durable manifest
revision, entries, and authorized view files. Pages contain at most 64 entries;
`next` names the following page. A source entry retains its original identity,
revision, digest, channel, and captured extent. A view identifies its own digest
because redaction and result field projection can change the disclosed bytes.
The source digest must never be used to verify a redacted view.

The workflow index covers the explicitly retained launch resource closure,
application result revision history, prior same-session provider logs, and exact
checkpoint references supplied by the factory. Launch and result records remain
in their existing store owners; provider logs remain under their runtime owner.
The manifest does not promise retention after those owners delete their records.
The current invocation's growing log is outside this prior-evidence scope.

A complete index means its declared retained source set is accounted for. It
never means every fact was captured, every log is complete, or all bytes were
disclosed. Capture coverage is separately `complete`, `partial`, `empty`, or
`unknown`; availability is `available`, `unavailable`, or `restricted`. Legacy
provider logs retain unknown capture completeness and unknown channel ordering.
Missing launch identities and missing logs are explicit entries. An empty
captured source is distinguishable from missing content.

Each workflow content view is limited to 256 KiB, within a total 4 MiB invocation
allowance. A larger source retains its exact reference and an explicit terminal
disclosure limit. Index pagination is not a budget reset. Use only the existing
permitted read surface and its budgets to inspect files or follow references.
Validate invocations without an existing helper read grant receive an explicit
artifact disclosure limit; evidence preparation does not add that grant.

Every materialized source is checked against the current factory binding and
scope. Historical result revisions disclose only currently granted fields;
whole logs and checkpoint receipts require the complete result read grant.
Redaction runs before any source bytes are written. Artifacts live inside the
existing invocation sandbox, use rooted paths, and reject symlink components.
Reusing a sandbox clears previous views before rebuilding them, so a narrower
grant does not inherit stale files. Artifacts are disposable caches, not the
retained evidence owner.

A previously supplied manifest revision can be recovered explicitly. Recovery
keeps its original source set and matches the exact source identity and digest.
Missing or changed bytes produce an unavailable view; they never cause a newer
log, finding, or installed guidance package to replace historical content.
Required launch corruption continues to refuse admission. Manifest persistence
must succeed before a handoff is described as recoverable. Failed authorization,
redaction, storage, or cancellation never creates a successful completeness claim.

Repair manifests link the diagnosis attempt and retained instruction capture to
findings, captured command evidence, repair operations, and step journals. An
agent report, an observed command result, and a verified effect remain separate
records. Repair eligibility, approval, reconciliation, and publication receipts
remain with their existing owners.

Doctor invocation indexes declare the first 128 source records, sorted by kind,
identity and revision when the handoff is prepared. Any remaining history has an
explicit terminal disclosure limit. Content views share a 128 KiB allowance;
the index has a separate 256 KiB metadata ceiling. A restart of the same admitted
reader restores its frozen manifest rather than adding later captures. Each read
checks that the reader still owns the active conversation. Captured guidance is
decoded into a text view of its full resource closure before redaction, so encoded
file content cannot bypass redaction. That view has its own digest and is not a
replacement instruction capture.

Manifest links follow the owning task's existing archive lifecycle. Doctor's
existing archive guard still protects conversations and repair records. A link
does not extend source retention or promise that deleted content can be recovered.
