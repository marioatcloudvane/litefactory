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

## Assisted authoring

`standards author` saves work after each command and needs no interactive
terminal. Start a new standard with an intent, or add `--target STANDARD_ID` to
edit an existing standard. The session fixes that target and retains the verified
published book used at the start. It uses existing categories.

```sh
litefactory standards author start safe-tests --intent 'Explain how to isolate test data'
litefactory standards author continue safe-tests --expected-version 1
litefactory standards author questions safe-tests --json
litefactory standards author answer safe-tests --expected-version 2 --question QUESTION_ID --answer 'Use dedicated test data'
litefactory standards author show safe-tests --json
```

Use the version returned by the preceding command for each mutation. `start`
also accepts intent text through `--file FILE` or `--file -` for stdin. `answer`
accepts a JSON array through `--file`, with `question_id`, `text` and optional
`replace` fields. Existing answers require explicit `--replace` (or
`"replace": true`). Question IDs and earlier answers survive restarts.

`edit SESSION --expected-version N --file FILE|-` accepts a JSON object with
optional `title`, `body`, `category_ids` and `context_category_ids`. Title, body
and category edits remain authoritative on later continuations. The context
filter narrows what the assistant receives; it does not change category IDs by
name. `list`, `show` and `questions` inspect saved work; `show --version N`
reads an earlier version. Add `--json` for full structured state, including
retained context, questions, diagnostics, attempts and completion references.

Run `validate SESSION --expected-version N`, inspect its resulting version, then
explicitly `complete SESSION --expected-version N`. Completion atomically links
one exact validated, unapproved proposal. It prints the proposal ID, version and
digest for the existing proposal review process. Repeating completion for that
same version recovers the same proposal. Authoring never approves or publishes.

If publication moved the book, completion refuses the stale base. Inspect
`standards book current`, then explicitly use `author rebase SESSION
--expected-version N --revision NEW_HEAD`. Rebase retains draft text and answers,
shows old/new target and category changes, and asks for affected choices to be
resolved. A removed edit target requires a separate new session.

Continuation uses configured response-only providers with verified isolation,
no native or MCP tools, and separate usage attribution. Unsupported providers
remain unavailable. Each continuation has at most two calls and a 120-second
deadline, with 256 KiB prompts and 32 KiB responses. The retained book capture
limit is 16 MiB. Each serialized saved session version is capped at 64 MiB to
retain that base together with the original edit target, draft, questions, answers
and context. Oversized context stays explicit; use `--categories ID,ID` at
start or edit `context_category_ids` to narrow it. There is no silent truncation
or automatic repair loop.

After interruption, inspect the session and explicitly continue once its finite
lease expires. An edit fences the previous continuation; late output cannot
overwrite newer saved work. A retry can charge again and return different text.
SQLite WAL with synchronous NORMAL protects ordinary process interruption, but
power loss can lose recent writes. Disk loss and administrator modification are
outside this guarantee.
