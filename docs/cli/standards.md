# Standards book

`litefactory standards` manages one shared book in the initialized factory database.
Commands work without a terminal and emit JSON. Category changes belong to explicit
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
