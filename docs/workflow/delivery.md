# Factory delivery

`lf_deliver` is one factory activity. Every node declares exactly one
`lf_operation`: `push`, `merge` or `close`. An operation limits effects; it does
not grant permission. Team versions stay at 1.0.0. The shipped Spike and Validate
graphs use `lf_deliver`; Bug still uses its `lf_bug_finalize` compatibility entry.

| Workflow binding | push | merge | close | Authority |
|---|---|---|---|---|
| Retained checked result | `ready,unchanged,revalidate,blocked` | `merged,revalidate,blocked` | `completed,revalidate,blocked` | Current authenticated review reply, bound to the displayed candidate |
| Bug | `ready,revalidate,stale,blocked` | `merged,revalidate,stale,blocked` | `completed,revalidate,stale,blocked` | Existing approved diagnosis and accepted repair |
| Validate | `done` | `done` | `closed,open` | Existing owner-command intents and exact commit/PR/issue set |

For retained results, all delivery nodes name the same agent through
`lf_source`. They declare a positive `lf_limit`, `lf_park="owner"` and
`lf_stop="blocked,failed,invalid,exhausted"`. `push` saves, synchronizes, pushes
and finds/opens the PR. `ready` reaches its review ask; `unchanged` reaches its
final comment. `revalidate` returns to its source for fresh checks.

The review ask uses `lf_kind="review"`, `lf_source` and
`lf_approve_reply="ship"`, with outcomes `published,approved`. `published` ends at
a waiting terminal. Only a claimed authenticated exact owner reply releases its
`approved` edge to `merge`. The review displays the saved commit, base and PR.
An agent's words cannot approve it. `merge` returns `merged` to its `close`
operation, and `close` returns `completed` to a terminal. Both return
`revalidate` to their source when candidate identity changes. Remote failures
park for retry. These are graph capability constraints, independent of node names.

Bug keeps its existing owner conversation and repair-check boundaries. Its
`lf_deliver` nodes use `lf_owner_continuation="required"`, owner parking and the
existing stop set including `stale`. The successful repair reaches `push`;
`ready` reaches `merge`; `merged` reaches `close`; `completed` ends the graph.
Every `revalidate` route returns to the declared repair entry (including its
base-sync prerequisite, when present). A fresh accepted repair is required after
source changes. The old `lf_bug_finalize` remains an all-in-one compatibility
entry using the same reconciliation implementation.

Validate replaces only the action declarations of its remote nodes, keeping
its existing outcomes, resolve edges and owner commands. For example:

```dot
owner_push [lf_action="lf_deliver", lf_operation="push", lf_outcomes="done", lf_limit="16"];
```

`/validate-push` still authorizes only push/PR, `/validate-merge` only the displayed
PR and head, and `/validate-close` only the displayed issue set after merge.
`/validate-ship` records the existing authorization chain. Push and merge return
to owner resolution or reporting; they cannot silently advance permissions.
Existing `lf_validate_push`, `lf_validate_merge` and `lf_validate_close` wrappers
use the same operation handlers and durable intent/uncertainty records.

All bindings use factory-owned Git and forge gateways. The shared exact-head
merge helper observes remote state after a lost reply. Bug and retained-result
reconciliation park and retry; Validate retains its owner-command retry policy.
Closure requires confirmed integration. Agent decisions about edits, checks and
reporting remain in Markdown.
