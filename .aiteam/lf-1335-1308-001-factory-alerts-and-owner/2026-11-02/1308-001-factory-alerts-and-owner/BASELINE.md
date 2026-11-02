# Compatibility baseline (T-01)

Established before shared intake, notice, Discord transport or report-delivery production changes. All checks use offline local SQLite and captured HTTP servers; no live services or credentials. Existing tests retained. Added `TestOrdinaryPayloadMentionSuppressionBaseline` because ordinary actual payload coverage previously checked parse suppression on notices but did not explicitly reject user/role/reply mention allowlists across ordinary channel, interaction and rich-reply delivery.

## Executed commands

- `go test -race -timeout=10m ./internal/messaging/discord -run 'Test(OrdinaryPayloadMentionSuppressionBaseline|TaskNoticeJourney|OwnerCommands|DeferredEdit|NaturalPromptDeliveryIdentity|GatewayIdentifiesHeartbeatsAndReportsOwnerMessages)$'` — PASS (4.861s). Covers signed owner intake, ordinary gateway intake, notice deduplication/retry/restart/purge, channel fallback, interaction retries and remote identity handling.
- `go test -race -timeout=10m ./internal/messaging/discord -run 'TestOrdinaryPayloadMentionSuppressionBaseline$'` — PASS (1.493s), after strengthening the interaction case to establish a signed live interaction handle. Checks actual serialized `allowed_mentions` for no parse targets, user/role allowlists or reply mention across all three routes, with injected owner/user/role/everyone/here text.
- `go test -race -timeout=10m ./internal/messaging -run 'Test(NaturalIntake.*|ConversationRestartJourney|NoticeCollectionEnableStaleAndRetry|NoticeMappingAndQuestionBudget|ConversationFactory.*)$'` — PASS (12.393s). Covers ordered ordinary routing and retained cursors, restart/failure, history activation, future timestamps, large intervals, notice enablement/retry and real conversation restart.
- `go test -race -timeout=10m ./internal/messaging -run 'TestConversationFreshCommandStartsNewNativeSession$'` — PASS (5.205s). Covers fresh conversation before provider calls, both factory/conversation paths and history/gateway origins.
- `go test -race -timeout=10m ./internal/store -run 'Test(NoticeAdmissionRollbackAndReplay|NoticeLeaseFenceAndRetention|ReportDelivery.*|ReportRecovery.*|DeleteReportRecovery.*)$'` — PASS (98.614s). Covers notice transaction rollback/replay, fenced leases/retention; report original identity, uncertainty, confirmation metadata, migration, HTTP/SQLite failure windows, atomic completion and retained recovery sources/restart.

The Makefile exempts full SQL-heavy store runs from race instrumentation; this focused baseline intentionally uses race coverage. Final full-package checks must follow that exemption plus its named concurrent race selection.

No live Discord delivery, installed permissions or push behavior is established by this baseline. T-09 must rerun this selection after shared-path changes and compare actual results.

## Shared-path comparison and transport verification

After the alert-specific Discord transport and shared full-delay backoff were added:

- `go test -race -timeout=10m ./internal/messaging/discord -run 'Test(Alert|OrdinaryPayloadMentionSuppressionBaseline|DeferredEdit|TaskNoticeJourney|OwnerCommands)'` — PASS (3.654s). Ordinary routing/fallback, notice retries and actual mention suppression remain intact. Intended difference: Discord's advertised retry delay is preserved beyond one hour and shared across ordinary and alert sends; no short retry overrides it.
- `GOLANGCI_LINT_CACHE=/tmp/lf1335-golangci-cache make lint PKGS=./internal/messaging/discord` — PASS, 0 issues. The explicit cache path keeps analysis cache inside the permitted writable area.
- Transport regressions exercise bounded safe actual payloads, trusted owner construction, absent/invalid message IDs, permission/authentication/destination failures, full 7500-second global rate delay, exact own-bot/channel/content reconciliation, absent/incomplete/forbidden history and the four-page read bound. Uncertainty never grants resend permission.

Provider normalization verification:

- `go test -race -timeout=10m ./internal/agent ./internal/harness ./internal/codex -run 'Test(ProviderOutcomeUsesTypedEvidence|ProviderEventClassIgnoresGeneratedAndUnknownText|ProviderStructuredTerminalEvidence|ProviderCapacityEvidence|RunnerQuotaDiagnostics|ClassifyOutcomeContextPrecedence|Recorded.*IsolationRefusals)$'` — PASS (agent 1.389s, harness 6.587s, codex 7.544s).
- `GOLANGCI_LINT_CACHE=/tmp/lf1335-golangci-cache make lint PKGS='./internal/agent ./internal/harness ./internal/codex'` — PASS, 0 issues.

Provider classification adds safe terminal metadata and typed error evidence only; existing retry decisions and run outcomes are preserved. Generated prose, generic raw errors, unknown codes, cancellations and timeouts do not establish provider failure classes. Verified quota and existing typed conversation refusal/authentication remain affirmative evidence.

Provider terminal callback wiring:

- `go test -race -timeout=10m ./internal/agent ./internal/harness ./internal/codex ./internal/messaging -run 'Test(ProviderObserverTerminalCopyAndCancellation|ConversationProviderObserverTerminalClasses|ProviderStructuredTerminalEvidence|RunnerQuotaDiagnostics|ConversationFreshCommandStartsNewNativeSession|ConversationProviderRefusalAndLoginReplies)$'` — PASS (agent 1.337s, harness 2.230s, codex 2.907s, messaging 6.362s).
- `GOLANGCI_LINT_CACHE=/tmp/lf1335-golangci-cache make lint PKGS='./internal/agent ./internal/harness ./internal/codex ./internal/messaging'` — PASS, 0 issues.

The observer callback is synchronized, receives a copied terminal run result, and runs outside its lock. It cannot alter the adapter's returned outcome. Unstarted unknown work and cancellation are excluded; conversation source identity is carried separately from safe failure class. No retry or provider-execution controls are exposed to the alert callback.
