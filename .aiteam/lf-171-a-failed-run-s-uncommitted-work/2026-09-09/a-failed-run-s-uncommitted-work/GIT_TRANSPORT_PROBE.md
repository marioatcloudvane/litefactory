# Reserved transport synchronization probe

Verified on 2026-09-09 with `git version 2.50.1 (Apple Git-155)` using isolated temporary local bare origins, clones and task worktrees; no external service or daemon state.

Reproduce with:

```
go test ./internal/git -run TestReservedTransportSynchronization -race -cover -count=1 -v
```

The table exercises both `.litefactory-plan-proposal.json` and `.litefactory-story-events/244/000001.json`: untracked, staged, incoming untracked collision, incoming staged collision, tracked dirty, tracked incoming collision, clean tracked incoming collision, and unchanged clean legacy tracking. Each refusal checks task HEAD, raw index bytes and retained transport bytes; successful synchronization is followed by a task save proving transport is absent from that commit's changes and remains on disk.

Before the correction, staged transport (even with unrelated incoming changes), staged incoming collisions and tracked dirty incoming collisions failed Git merge but changed raw index bytes. Git reported local changes would be overwritten and its subsequent abort reported no MERGE_HEAD. Adding `--no-optional-locks` to the merge invocation did not prevent these index changes (six failures across proposal and event paths).

The correction performs reserved-path checks before mutating Git merge: tracked/staged transport changes and branch/base differences in reserved entries produce explicit refusal. Ordinary untracked transport remains compatible with synchronization. Underlying inspection errors remain wrapped. The guard neither clears transport nor changes its lifecycle, and identical legacy tracked entries are permitted.
