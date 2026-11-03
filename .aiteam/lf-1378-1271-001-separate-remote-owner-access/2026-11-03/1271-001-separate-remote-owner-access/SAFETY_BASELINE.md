# Safety baseline and characterization

Pinned pre-implementation revision: `5a08f9e4e99640ef56592c90094f1dae75babe87`.

This is the story's starting build, distinct from the older Research source. No production boundary had been changed when the focused connector/store baseline below started. Manual criteria remain for Validate.

| Research / invariant | Existing behavior checks and new characterization |
|---|---|
| F-01, K-03 public contracts | Connector `TestOwnerConsentConnection`, `TestOverviewToolContract`, `TestMCPProtocolAndBrowserBoundaries`, consent and browser tests retain registration, consent, OAuth, MCP routes and refusal contracts. |
| F-02, F-05, K-05 shared backend and target binding | `TestConstructionBindsAuthenticationAccessAndLocalControl` accepts independent adapters over one DB and refuses mixed authentication/access/local-control DBs. Store discussion coverage and retained code tests cover core facts independently of coverage. |
| F-03, K-07 retained identities | Store `TestConnectorCodeReviewDerivesTheTasksRetainedCodeAndFailsClosed`, `TestConnectorCodeReviewRequiresBothScopesAndRetainedIdentity`, `TestConnectorCodeReviewMappedParentBase`, `TestConnectorCodeReviewPlanMappingUsesRetainedTicketSource`; code review continuation/release checks. |
| F-04, K-02, K-04 shared execution | Existing `TestActionMCPUsesTrustedGrantAndFrozenFacts`, `TestDurableWorkflowMCPJourney`, `TestTaskAdvisoryPrivateFollowupJourney`, stage decision scope/discovery tests; daemon shared action/recovery tests. These are preserved compatibility obligations. |
| F-05, K-01 token/owner/resource authority | Store code binding/expiry, owner-away-and-back, concurrent rotation/revocation and spent ancestor tests; connector durable revocation/restart, protected expiry/owner/readiness and refresh replay tests. |
| F-05, K-05 authorization before acquisition/release | Existing discussion cursor/release, usage boundary and code release tests; new `TestRemoteRetainedTargetReadAndRevocation` crosses MCP, durable credentials and core retained target resolution, checks provider identity and refuses anonymous/missing retained targets before acquisition, then withholds acquired evidence when grant is revoked during acquisition and refuses the next call. |
| F-06, F-09, K-08 dependency/disabled deployment | Connector/factory dependency tests; daemon `TestFactoryCoreTicketMainPathWithoutOptionalTransports` and remote preparation/listener tests. |
| F-07, K-01, K-05 transactional persistence | Store consent missing/unknown repository rollback, finite action coverage, concurrent local revoke-all/consent/refresh, code binding and replay tests. Old/current/old real-build compatibility remains a separate T-05 obligation. |
| F-08 existing evidence | Research's earlier executed checks are historical evidence only. This document lists new baseline executions separately below. |
| F-10, K-06 startup invalidation | Generic daemon preparation failures and listener lifecycle exist. Missing targeted owner-away/back characterization under failed preparation was reported to lifecycle owner before extraction for completion. |

## Executed before extraction

- `go test ./internal/connector -run 'Test(RemoteRetainedTargetReadAndRevocation|OwnerConsentConnection|ConstructionBindsAuthenticationAccessAndLocalControl|DiscussionCursorAndRelease|UsageBoundaryDeniesBeforeProviderAndWithholdsRevokedOutput|CodeReviewGrantExpiryAndReleaseLifecycle)$' -count=1 -timeout=5m`: passed, package reported 1.211s.
- Focused store baseline is recorded after completion below.

The new E2E uses synthetic evidence and an in-process HTTP protocol handler, real local SQLite state, actual client registration and actual durable consent/code-exchange fixture transitions. Existing browser OAuth journey separately exercises fresh synthetic GitHub authentication. No live provider, production URL or external database is used.

- `go test ./internal/store -run 'TestConnector(CodeBindingAndExpiry|RevocationIsolationAndReplay|OwnerAwayAndBackAndLocalControl|ConcurrentRotationAndRevocationPersists|ConsentRefusesMissingOrUnknownRepositoriesAndRollsBack|RepositoryCoverageLifecycleFailsClosed|CodeReview.*|ActionConsent.*|UsageScopesAreIndependent)$|TestLocalConnector' -count=1 -timeout=8m`: passed, package reported 1.371s.

After the initial baseline passed, the new E2E was expanded to obtain its finite repository grant entirely through public authorize, controlled-identity callback, consent and token endpoints. Its final verification is recorded separately; the initial passing baseline used the existing durable consent/exchange fixture helper. This distinction preserves which version was actually executed before extraction.

## Final main-path characterization after extraction

The expanded `TestRemoteRetainedTargetReadAndRevocation` obtains authorization through public client registration, authorize, synthetic fresh identity callback, finite repository consent and token exchange before MCP reads. It checks successful retained evidence release, anonymous refusal, missing retained-target refusal, refusal for a real retained task outside finite repository coverage, revocation during provider acquisition with no evidence release, and refusal of the next call without provider acquisition. The controlled clock advances one second per MCP call to avoid incidental request throttling while retaining token validity.

- `go test ./internal/connector -run TestRemoteRetainedTargetReadAndRevocation -count=1 -timeout=5m`: passed, package reported 0.557s.

- `go test -race ./internal/connector -run 'Test(RemoteRetainedTargetReadAndRevocation|OwnerConsentConnection|ConstructionBindsAuthenticationAccessAndLocalControl|DiscussionCursorAndRelease|UsageBoundaryDeniesBeforeProviderAndWithholdsRevokedOutput|CodeReviewGrantExpiryAndReleaseLifecycle)$' -count=1 -timeout=8m`: passed, package reported 7.256s.
- `go test -race ./internal/connectorstorage -run TestBindingKeepsCoreTargetsOnCredentialBackend -count=1 -timeout=5m`: passed, package reported 2.548s. This new direct binding test confirms the extracted core enumeration follows the credential backend; independent wrappers over one database handle share identity, while different database handles (including another handle to the same file) do not. Existing connector construction tests retain mixed access/storage/local-control refusal coverage.

Inspection of the target cut found retained code SQL and parent-branch validation preserved in the neutral core query helper, transaction-bound repository resolution retained during consent, and existing scope/coverage/refusal ordering retained in connector authorization. A concern about constructing authorization with unrelated target readers was reported to the implementation owner for resolution; no claim that import guards alone prove binding is made here.

The reported constructor concern was closed by removing backend attestation from the standalone authorization service. It cannot satisfy the remote Access port; the private connectorstorage adapter alone supplies production identity proof while binding both dependencies from its one database handle. The factory dependency guard was extended to reject connectorstate, connectorstorage and connectoraccess imports explicitly, in addition to the existing connector/transport guards.
