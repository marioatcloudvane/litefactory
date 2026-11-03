# Target, authorization and lifecycle retirement audit

Read-only review of the extracted implementation. Acceptance remains with Validate. Final package checks and rollback rehearsal are separate evidence; they are not inferred from source inspection.

## One route per owner

| Previous route | Delivered replacement and retained compatibility |
|---|---|
| `store.ResolveConnectorDiscussionTarget` / internal connector target query | `store.ReadTaskTarget` and transaction-bound `resolveReadTaskTarget` in `read_targets.go`, exposed to connector composition through `factory/readtargets.Service.Task`. Canonical task/session/repository identity comes from existing rows without creating a session. |
| `store.ResolveConnectorCodeTarget` and retained target SQL in connector source | `store.ReadCodeTarget` / `resolveReadCodeTarget`, exposed by `readtargets.Service.Code`. Retained execution snapshots, ticket bindings, connection versions and parent/base branch consistency SQL are preserved. No configured clone/path/branch fallback added. |
| `store.ListConnectorRepositories` and connector-specific repository resolver | `store.ReadRepositories` / `ReadRepository` and transaction-bound `resolveReadRepository`, exposed by core readtargets. Same canonical reference ordering and 1000-entry bound. Consent and action coverage keep repository resolution in their original transaction. |
| `store.CheckConnectorDiscussionAccess`, `CheckConnectorCodeAccess`, `CheckConnectorUsageAccess` | `connectoraccess.Service` owns live credential/scope/coverage decisions over core targets. Private connectorstorage adapter delegates to it; no old policy body or fallback remains in store. Credential transitions and grant-checked admissions remain in physical persistence to preserve serialized transactions. |
| Combined connectorstorage persistence and target policy implementation | `connectorstorage.New(one DB)` privately constructs storage plus core target reader and authorization policy. Its externally exposed ports have no DB handle or general store callback. The adapter contains delegation, not duplicate target queries or coverage decisions. |
| Direct daemon owner reconciliation and listener ownership | `connector.Prepare` reconciles before local readiness, secret resolution and listen; connector Runtime owns serve/close. Daemon supplies dependencies, one shared core runtime, cancellation and serving join. Standalone `connector.New` retains reconciliation for direct construction; Prepare uses its internal already-reconciled constructor, so startup does not run both routes. |

## Binding closure

`connectoraccess.Service` does not expose `SameStore` and cannot satisfy the remote `connectorstate.Access` port. Constructing policy with an arbitrary target reader cannot itself attest backend identity to Server. The private immutable connectorstorage adapter is the production binding: its constructor accepts one DB and creates both credential persistence and the core reader from that handle. Identity checks accept independent adapters over the same handle and reject other handles, including a separate handle to the same SQLite file. Existing server/local-control construction checks reject mixed backends.

Neutral target aliases in connectorstate/store remain source compatibility aliases only. They carry values; they do not query persistence or authorize requests. Core readtargets/contracts import no connector policy. Existing bounded overview, task evidence, usage and code providers retain their private core readers and are not relocated.

## Searches reviewed

Production-source searches across internal packages found:

- No `ResolveConnectorDiscussionTarget`, `ResolveConnectorCodeTarget`, `resolveConnectorDiscussionTarget`, `resolveConnectorCodeTarget`, `resolveConnectorRepository`, `connectorCoveredTask` or `connectorCoveredRepository` routes.
- No production daemon direct `ReconcileConnectorOwner` call. Daemon test fixture setup still seeds authority directly and is not a production lifecycle route.
- Protected read access calls remain at connector acquisition/release boundaries and delegate to the bound policy service.
- Core admission transactions reuse neutral target helpers for consent, action coverage, discussion and stage decisions; no SQL callback or credential is added to core target contracts.
- No changed schema/migration file in the story diff at review time.

## Evidence and limits

Focused pre-extraction checks and the public remote retained-target main path, including mid-acquisition revocation, are recorded in SAFETY_BASELINE.md. The direct binding test exercises separate adapters and bound target enumeration with real temporary SQLite databases. Complete package runs, lint and final rollback results are owned by the story lead and must be read from their executed-check evidence.

Real-build old/current/old authorization rehearsal evidence is retained in [the rehearsal report](rehearsal/README.md), with source/executable identities, a complete source manifest, eight sequential process results and an identical schema fingerprint in every phase. Inspection of its retained observed log finds the final generation restart passed with all invalidated access/refresh credentials refused and handles closed. This audit does not replace the executed rehearsal or broaden what it proves. No claim is made here for live provider certification, power-loss durability, retraction of bytes already released, restoring obsolete authorization snapshots, independent process versions or multi-host operation. SQLite remains the same physical shared database with unchanged encodings. The encountered pre-existing Go response-guidance standard issue remains for operator disposition as approved; it is not moved by this refactor.

Executed audit checks: `go test -race ./internal/connector ./internal/factory -run 'Test(RemoteConsumerCannotAcquireFactoryStoreOrExecution|FactoryDependencyDirection|RemoteRetainedTargetReadAndRevocation|ConstructionBindsAuthenticationAccessAndLocalControl)$' -count=1 -timeout=8m` passed (connector 2.903s; factory 1.418s). The factory guard now explicitly refuses connectorstate, connectorstorage and connectoraccess dependencies as well as the existing transport dependencies.
