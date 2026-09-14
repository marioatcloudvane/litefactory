# US-001 executed I/O checks

Frozen revision c75f0fc994ded9287b03caa2e600cecd911d5d962b043b5f73aa6be4b913a6d0, issue #281, workflow 2, no dependencies.

Five tests passed, zero failed. Normal Go tests use temporary migrated modernc SQLite and actual private Unix sockets/filesystem journals; no external database, production target, environment override, build tag, or authentic host invocation.

Files:
- internal/store/family_requests_test.go
- internal/turn/family_receipts_test.go
- internal/submission/family_journal_transport_test.go

Coverage:
- AC-001: twelve concurrent SQLite admissions elect one invocation owner; changed family/payload/scope cannot reuse correlation; closing/reopening the database retains the confirmed result and immutable reference. Every new tool name gets its own persisted client identity, descriptors isolate attempts, identical retries reopen their original identity, communication helper/MCP submission identities remain compatible. Lost socket reply retains identity persisted before handler entry.
- AC-002: mismatched current run/attempt/repository bindings fail; revoked old authority cannot read or begin; an explicitly authorized current reader observes old history while an ungranted current reader sees missing. Authored secret content is absent from current and historical projections.
- AC-003: missing/rejected/pending/uncertain/confirmed are distinct after reconnect. Repeated lookup leaves outcomes unchanged, missing lookup allocates no ledger entry, receipts_get and communication_receipt allocate no journal, and observation of an existing application result after a lost ledger link preserves separate input/engineering/acceptance/effect dimensions without another application revision.

Executed final plain command:

```text
go test ./internal/store ./internal/turn ./internal/submission -run 'TestFamily(Ledger|ReceiptsDurable|ReceiptsUnderlying|JournalSocket)' -count=1
ok  github.com/cloudvane/litefactory/internal/store       0.756s
ok  github.com/cloudvane/litefactory/internal/turn        1.609s
ok  github.com/cloudvane/litefactory/internal/submission  1.054s
```

Executed race command:

```text
go test -race ./internal/store ./internal/turn ./internal/submission -run 'TestFamily(Ledger|ReceiptsDurable|ReceiptsUnderlying|JournalSocket)' -count=1
ok  github.com/cloudvane/litefactory/internal/store       4.146s
ok  github.com/cloudvane/litefactory/internal/turn        6.104s
ok  github.com/cloudvane/litefactory/internal/submission  2.818s
```

The store test was then strengthened to close the first DB before reopening; its final race check also passed:

```text
go test -race ./internal/store -run TestFamilyLedgerConcurrentReopenAndIsolation -count=1
ok  github.com/cloudvane/litefactory/internal/store 3.156s
```

Limits: socket tests exercise the actual broker/client/journal with a controlled handler, while SQLite service tests exercise actual receipt/result owners. Full bound AttemptGuard/catalog composition and authenticated denial recording remain US-002/US-008 work. Publication-specific identity projections belong to their family stories. No acceptance review or factory contribution was performed by this specialist.
