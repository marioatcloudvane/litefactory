# US-001 implementation report

Frozen revision: c75f0fc994ded9287b03caa2e600cecd911d5d962b043b5f73aa6be4b913a6d0; issue #281; no dependencies.

Implementation task complete; criterion test specialists and aggregate engineering remain pending. No acceptance assessment or engineering submission performed.

Files:
- internal/submission/client.go: durable family/tool/descriptor namespaced journal before transmission, preserving communication identities; receipts_get uses the looked-up identity and allocates no journal.
- internal/store/migrations/0044_family_requests.sql: bounded metadata ledger with separate authoritative reference fields.
- internal/store/family_requests.go: current-binding admission, exact replay conflict checks, first invocation ownership, 4,096-per-run record budget, immutable outcome references, explicit historical read scopes.
- internal/turn/family_receipts.go: copied immutable factory reader scope, permission material, nonrecursive receipt projection, separate workflow dimensions, bounded confirmed publication identities and incremental observation-only projection registration.

API handoff: NewReceiptService(db, binding, historicalScopes, projections); PermissionMaterial() ([]byte,error) must enter US-002's composite permission digest. Begin(ctx, requestID, tool, payload) returns ledger record and first-invocation bool. Finish records confirmed/rejected/uncertain with store.FamilyReference. Get and HandleGet observe only. FamilyProjection maps reference kind to separate states/publication identities. Optional tool:<tool-name> projection recovers pending lost links by stable underlying request identity; it must observe only. Built-in SubmissionReceiptProjection, ResultReceiptProjection and CreationReceiptProjection reference existing owners. More detailed publication projection remains in publication stories.

US-002 integration must call through live AttemptGuard.Context, check immutable permissions, record authenticated rejected family calls, exclude receipts_get from Begin, and retain ordinary communication behavior. Construction receives trusted factory scopes; no caller may supply those scopes. Historical scopes in this feature remain repository/task-local and can name prior runs/attempts.

Executed checks:
- go test ./internal/submission ./internal/store ./internal/turn -run TestNonexistent -count=1: all packages compiled; explicitly no tests ran.
- go test ./internal/submission -run 'TestBroker(PersistsCorrelation|ConcurrentSubmissions|LostReply)' -count=1: PASS, package output ok (0.403s).
- go vet ./internal/submission ./internal/store ./internal/turn: PASS, exit 0, no diagnostics.

No test files written; new acceptance-derived unit and actual SQLite/socket fixtures are the specialists' next tasks. No authentic host or remote publication evidence is claimed. No blockers to that handoff.
