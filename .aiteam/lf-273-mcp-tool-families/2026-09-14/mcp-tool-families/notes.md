# Implement notes — run 0007

Frozen workflow version 2 governs this implementation. Requirements are only the eight factory-supplied immutable revisions. Engineering and executed checks will be recorded independently of acceptance; no Validate review or issue closure is performed.

Assumptions: existing SQLite, local broker/socket and controlled HTTP fixtures are the authorized real-boundary test environment. Production host launch, approval intake and lifecycle/recovery scheduling remain #274. Creation reuses server-held grants/private permits from #272; public prose and tool arguments cannot authorize creation.

US-001 is in progress; later stories follow its dependency graph.

US-001: scoped unit and SQLite/socket race checks passed. Engineering recorded under request d77222eafecfecdfb3e8cfb9d3c1a17e; input valid, engineering recorded, acceptance not applicable, remote effects pending. This releases engineering dependencies without claiming remote publication. US-006 implementation started.

US-006, US-002, US-007, US-004 and US-003 engineering recorded after specialist checks. US-002 was invalidated and recaptured after strict raw UTF-8 rejection fixed a comment regression. US-003 top-level null rejection was fixed before its first checkpoint. SQLite concurrent writer BUSY/BUSY_SNAPSHOT remains a fail-closed transient response; tests retry identical identity and assert no additional dispatch ownership. All retained remote-effect receipt states are pending; no forge completion is claimed.

Final: make check passed after one aggregate lint correction cycle. The full suite deliberately uses the repository timeout policy and took over 24 minutes in turn; recent temporary-fixture metadata established ongoing progress during quiet output. All eight final engineering snapshots were recorded and their source bytes remain current. Native host integration and authentic evidence remain #274. No Validate or forge publication was performed.
