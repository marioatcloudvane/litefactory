# US-005 implementation

Story #270 / issue ID 5440231261; frozen revision
`05c0a9d83aa275880740f7da56895aec235b2c7736f625955c152c12735bc9ad`.
Dependency US-001 was engineering complete before this work.

Implemented pure process-local advisory reduction in `internal/agentresult/progress.go`.
Factory-owned run/attempt/active authority is separate from authored positive sequence
and nonblank UTF-8 message. Dispositions carry attribution, exact retained replay is
a no-op, conflicting replay rejects, unseen older sequences are stale, and fresh
attempts reset ordering. Only one latest message and 32 sequence/hash identities
survive. Limits accept exactly 2,048 message bytes and 1,024 distinct updates.

State fields are private; reducer output and views copy pointer/slice values.
Factory-supplied terminal time expires at exactly 24 hours or earlier explicit
cleanup. Repeated terminal marking cannot extend retention. Factory must revoke
terminated/superseded attempts and call cleanup when no updates arrive. No persistent
tombstones or restart durability are promised. There is no clock read, timer,
goroutine, SQL, result writer, durable receipt or workflow transition.

Assumption recorded: unseen lower sequences in gaps are stale, as are evicted old
identities; only higher new sequences are accepted. This preserves monotonic latest
visibility and keeps replay bookkeeping bounded.

Changed production paths:
- `internal/agentresult/progress.go`
- `docs/agent-result-contract.md` (advisory progress section)

Checks and coverage are retained in US-005-unit-tests.md/.log and US-005-lint.log.
This is engineering evidence only; no live transport or acceptance claim.
