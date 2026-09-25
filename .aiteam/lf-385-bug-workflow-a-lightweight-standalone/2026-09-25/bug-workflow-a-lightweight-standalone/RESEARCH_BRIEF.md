# Research brief

## Request

Confirm #385's settled standalone Bug workflow against the supplied repository, including the additional requirement that both Bug agent markdowns match Research and Plan's dedicated nine-section specification. Establish reusable mechanisms, missing capabilities and unresolved technical details without implementing or reopening settled scope.

## Decisions

- **d1** — Preserve the supplied settled entry rule: bug selects a standalone workflow only without an explicit Mode label. Bug is not a sixth mode. An admitted task retains its workflow despite later relabelling.
- **d2** — Preserve two owner gates: approval of the exact analysis revision authorizes scoped repair; approval of the tested PR's exact head and target authorizes delivery. Material remedy changes return for renewed analysis approval. Only authenticated owner replies approve.
- **d3** — Preserve eight analysis passes and one shared budget of initial fix plus two repairs, consumed by test failures and PR feedback without restart or reply resets. Testing is one separately executed, durable child at a time, with recovery and cancellation; no general recursive or parallel graphs.
- **d4** — Deliver the seven Bug-only activities lf_bug_record, lf_bug_attempt, lf_bug_test_graph, lf_bug_checks, lf_bug_pr, lf_bug_merge and lf_bug_close. Checks come only from project configuration; missing recipes block. No dependency on #319, feature story lifecycle or mode handoffs.
- **d5** — Both Bug agents must have Role, Voice, Goal, How You Are Invoked, When You Are Done, Must Do, Must Not Do, Writing the Report and Output sections. Analyst investigates and shapes without product repairs; fixer makes the approved repair and regression tests. Analyst responses are ask/done/approved/blocked; fixer responses are done/reshape/blocked. Both use the shared versioned result route.
- **d6** — Preserve failure visibility and retained evidence. Skipped, interrupted, stale or flaky checks cannot pass. Changed source, recipe or target invalidates affected evidence and approval. Closure occurs only after confirmed integration; uncertain closure stays pending.


## Assumptions

- **a1** — Use checkout fb060b9c06ce923c47aa231b85975490200e2712 as this bounded pass's code baseline. Its commit subject is the merge of #383 for #377's shared-result fix. Latest remote-main equivalence was not independently established; source and existing tests were read but no tests or live workflow probes were executed.
- **a2** — Treat #385's supplied settled scope as authoritative over #367 revision 5's illustrative examples. In particular retain the explicit regression/focused/full check phases, shared repair entry paths and prohibition on budget resets. Exact recipe schema/location, child-call persistence and delivery-review continuation remain shaping details, not missing owner facts.
