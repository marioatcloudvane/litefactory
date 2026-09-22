# Research brief

## Request

Check whether issue #314 still persists; if so, explain root cause and preconditions, impact and severity, and a solution. Research only.

## Decisions

- **d1** — Bounded research completed against worktree revision 40fa12605a4c5bb5b366a6e2806add039c7cab40. No implementation, issue publication, daemon-state changes or commits were performed.
- **d2** — Recommend explicit rejection versus retryable outcomes at command boundaries and reuse recoverable post-before-consume reporting. Do not globally change leave to consume failures. Safety-net and operator-recovery design remain advisory.


## Assumptions

- **a1** — 'Still persists' is assessed against the supplied current worktree; this does not establish which revision the live daemon runs.
- **a2** — The historical #230 timestamps, retry count and deletion-based recovery are owner-supplied incident evidence, not independently observed in this pass.
