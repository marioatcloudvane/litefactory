
## Implementation

- Research reads code/web directly; source and installed Claude/Codex instructions share the existing evidence checklist. Removed both research specialists (six live files, two fixture files), roster/tier entries and active references.
- Preserved session/output/authority contracts and unrelated worktree edits. Historical releases and benchmark fixtures remain unchanged.
- New default Research profile permits Edit/WebSearch/WebFetch; generated Claude ceiling includes web. Existing explicit profiles/ceilings and omitted-ceiling semantics remain operator-controlled. No daemon config changed.
- Passed: internal/team tests; internal/config tests; targeted teamsource rendering, mode roster, snapshot, remote-install and relevant parity checks; diff whitespace and scoped gofmt checks.
- Full internal/teamsource run also finds pre-existing historical exact-prose parity failures across roles (structured handoff, shared planning paragraphs, reviewer guard). Left those unrelated contracts unchanged; aggregate gate is orchestrator-owned.
