# Standards book

`litefactory standards` manages one shared book in the initialized factory database.
Commands work without a terminal. Book and proposal commands emit JSON; run
inspection supports text and JSON. Category changes belong to explicit
proposals; nothing changes the published book until publication succeeds.

Create a proposal using a file, or `--file -` to read stdin. Input must be one
JSON object of at most 8 MiB:

```sh
litefactory standards proposal create first --file changes.json
```

```json
{
  "base_revision": 0,
  "operations": [
    {"kind":"category.add","category":{"id":"go","name":"Go","description":"Go guidance"}},
    {"kind":"standard.add","standard":{"id":"errors","title":"Handle errors","body":"Return useful errors.","category_ids":["go"]}}
  ]
}
```

Zero means the empty, unpublished base. For later proposals use the revision ID
from `standards book current`. Stable IDs survive replacements. Operation kinds
are `category.add`, `category.replace`, `category.remove`, `standard.add`,
`standard.replace`, and `standard.remove`. Remove operations take `id`.

`standards proposal edit first --expected-version 1 --file changes.json` saves a
new immutable version. The file replaces the entire change set, including its
base. To rebase, explicitly select the current book revision in that file, then
review and approve the new version. Published proposals are retained; use a new
proposal for later changes.

```sh
litefactory standards proposal review first --version 2
litefactory standards proposal approve first --version 2 --digest REVIEW_CHANGE_DIGEST
litefactory standards proposal publish first --version 2 --digest REVIEW_CHANGE_DIGEST
```

Review includes complete before/after data, change and resulting-book digests,
and the exact proposal version and base. Read that complete output before
approving. Approval and publication resolve the configured numeric owner from
storage; neither accepts an actor override. Local machine access implies the
owner, as with other owner-only CLI operations. Standards text is data and cannot
change factory permissions or enforcement.

Use `proposal list`, `proposal show ID --version N`, and `proposal validate ID
--version N` to inspect work. `category add|replace ID --proposal PROPOSAL
--expected-version N --name NAME --description TEXT` and `category remove ID
--proposal PROPOSAL --expected-version N` save a category operation in a new
proposal version. Replacing a pending addition edits that addition; removing it
removes the pending addition. The resulting proposal must still change the book
and keep every standard assigned to an existing category. `category list` inspects the current book; add `--proposal ID --version
N` to inspect the proposed result.

`book current`, `book history`, `book show REVISION`, and `book diff FROM TO`
inspect complete retained revisions and publication/approval provenance. An
unpublished database is reported explicitly. Missing or corrupt historical data
is an error, never a fallback to the current book.

Publication commits a complete snapshot, approval linkage and book pointer in one
transaction. It rejects a stale base without merging. Invalid changes and missing
exact approval cannot publish. After a lost response, inspect the proposal or
retry publication with the same version and digest; an identical retry returns
the retained publication. A busy/conflict error leaves the book intact; retry
the same command or inspect that identity before choosing another operation.

SQLite uses WAL with `synchronous=NORMAL`. It preserves database consistency, but
power loss can lose a recently committed transaction. Immutable history protects
against later application edits, not disk loss or database administrator
tampering. Back up the database using appropriate SQLite backup procedures.

## Standards retained for a run

`litefactory standards run github:acme/api#123 --run 2` inspects a run's retained
standards. Omit `--run` for the newest run; use `--repo owner/name` to disambiguate
a bare issue number. Add `--json` for machine-readable output. Inspection is read
only: it verifies retained bytes afresh and shows the pinned revision, source,
digest, capture outcome, each consumer's choices and reasons, and retrieval gaps.
Automatic recovery resolves the original invocation. Inspection never repairs a
binding or selects a newer book.
Normal archival keeps these retained records while the run remains inspectable.
The existing explicit forget operation removes them together with the forgotten
runs, so a later reused run identity cannot inherit old guidance. It leaves the
shared published book intact.

Before substantive work, agents use `standards_categories_list` to discover the
retained categories, `standards_select` to record relevant category IDs and a
short reason, and `standards_read` to retrieve the selected standards. Category
choice is an agent assertion; it grants no permissions. An explicit empty choice
is valid and differs from unavailable guidance or no selection. Additions retain
previous choices and use the expected version returned by the service. On a lost
reply, recover the retained selection reference and reread the same revision.
Damaged retained selections have a separate corruption status. Tools refuse to
read or extend them and never replace their book.

One owner request and its graph, fallback and automatic recovery launches share
one immutable book. A coordinator that starts work mechanically before any agent
run uses its immutable graph launch as the invocation anchor (shown as a negative
invocation ID). Factory-launched story workers inherit that anchor across their
separate sessions; each work item keeps its own selection. A new owner
request binds anew and supersedes standards from the prior conversation context.
On resume, agents restore selections and reread bodies from the retained book.
Unavailable, corrupt, unsupported or oversized books remain explicit while work
continues under existing controls; failures never mean an empty book. Disabled
MCP is reported as unavailable delivery, without enabling tools.

Category pages default to 25 entries and permit at most 100. Results are bounded
to 32 KiB including both MCP content copies; follow explicit cursors until
complete. Body ranges use UTF-8-safe byte offsets, total lengths and digests.
Initial canonical snapshot capture is capped at 16 MiB; calls retain the existing
30-second broker timeout. No automatic unbounded fetch loop runs. Selection is
separate from delivery: offered ranges show what the server supplied, and a
completion acknowledgment records the agent's report, not comprehension or
guaranteed receipt. Interrupted or partial retrieval stays visible. SQLite's
power-loss and administrator-tampering limits above apply to retained books too.

## Analyze selected bugs

`standards analyze bugs` performs one bounded, advisory analysis of explicitly
selected issues in one registered GitHub repository. It retains observations and
coverage gaps, then asks an isolated prevention analyst to explain causes and
propose supported prevention guidance. It does not repair issues or approve or
publish proposals.

```sh
litefactory standards analyze bugs start --repo acme/api --issues 123,124 --request-key review-2026-10-21
litefactory standards analyze bugs list --json
litefactory standards analyze bugs show ANALYSIS_ID
litefactory standards analyze bugs evidence ANALYSIS_ID --json
litefactory standards analyze bugs result ANALYSIS_ID --json
```

Save the analysis ID even when launch fails. Repeating the same request key and
scope inspects retained work; it does not refresh evidence or restart the model.
A changed scope needs a new request key. An abandoned attempt is retained as
interrupted, and a durable accepted result is recovered before interruption is
recorded.

The default ceilings are five issues, five direct fix references, thirty remote
requests (including failures), twenty items per page, 1 MiB per response, 4 MiB
of source material and 256 KiB of retained evidence. The ten minute deadline
includes collection. The analyst gets one launch, at most twenty-four turns,
three correction submissions and five proposals. Command flags may lower these
limits. Reaching a limit records an omission; it does not establish complete
coverage or justify a global no-change conclusion.

Evidence consists of retained, sanitized excerpts and source metadata, including
recorded diagnosis review status when available. Remote observations are collected
over an interval and are not an atomic repository snapshot. Redaction removes
known tokens; it cannot detect every unknown secret. Missing causes are reported
as insufficient evidence. Recorded causes and analyst inferences remain distinct,
and a merged fix alone does not prove cause.

Each analysis pins one verified standards binding and retains category selection
and delivery status. Unavailable, corrupt or partly retrieved guidance leaves a
degraded analysis and cannot create validated proposals. Result inspection retains
host coverage, causal citations, uncertainty and exact unapproved proposal
versions, base revisions and digests. Review those proposals through the existing
`standards proposal show`, `review`, `edit`, `approve` and `publish` commands.
Publication remains subject to stale-base review and fresh approval. The same
SQLite NORMAL power-loss limits described above apply to analysis records.
