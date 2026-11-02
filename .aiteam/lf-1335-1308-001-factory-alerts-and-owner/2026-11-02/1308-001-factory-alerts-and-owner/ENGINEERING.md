# Factory alerts and owner controls: engineering evidence

Engineering for story-001 only. Main CI monitoring, production deploy monitoring, new webhook subscriptions, repairs and work control remain outside this change. No live Discord send, installed mention, remote permission or device push was tested. Manual acceptance remains with Validate and the owner.

## Work and delegation

Three subagents built this story in the shared prepared worktree. One established the compatibility baseline before shared edits, then owned Discord transport and typed provider evidence. One owned additive storage/configuration, fixed controls and status projections. One owned the offline journey, policy regressions and owner guidance. The lead owned policy, observation, daemon integration and review, read every contribution, and ran the final package checks. File ownership was separated; dependent shared edits followed the passing baseline. No commit, stage, push, branch switch or live service test was performed.

## Delivered tasks

| Task | Delivered evidence |
| --- | --- |
| T-01 | BASELINE.md records passing ordinary routing/new conversation, notice retry/deduplication/restart, interaction fallback, actual ordinary payload suppression, lease fencing/retention and original report identity/uncertainty/recovery checks before shared changes. |
| T-02 | Additive migrations preserve old rows and create no historical notifications. Transactional SQLite alert ledger retains evidence, episodes, controls, receipts, immutable attempted messages/digest membership and fenced attempts separately from ordinary purge. Concurrent mutation/rollback/restart tests cover persistence. Old configurations still load with enabled factory defaults. |
| T-03 | Joined independent daemon monitor observes disk, authoritative work/waits/progress, saturated pool, captured stopped queues/dead dispatches and original report uncertainty. Provider facts are typed, operation-deduplicated and source-local; retries remain owned by the existing paths. |
| T-04 | Authenticated owner/allowed-channel commands execute before conversation binding, using an independent bounded history scan, exact durable receipts, composed controls and deterministic time/DST parsing. Malformed reserved forms never reach a model. |
| T-05 | Durable claims precede HTTP. Ambiguous sends reconcile exact bot/channel/content identities without blind replay. Full advertised 429 delays share transport backoff. Quiet warning digests retain fixed attempted membership. The adapter alone constructs trusted critical owner mentions; actual HTTP regression payloads cover injected user/role/everyone/owner tokens and suppression on warnings/clears/digests/ordinary replies. |
| T-06 | One sanitized bounded local projection supplies alerts, overview/schema and doctor, with grant filtering, unavailable/stale coverage, controls and delivery states. Reads neither poll nor repair. |
| T-07 | Clock-driven policy/store/source/command tests cover thresholds, affirmative newer recovery, errors, cooldown/escalation, mutes/disablement, DST, quiet resolved members, multipart UTF-16 bounds, source-local provider windows, immutable uncertainty and concurrency. |
| T-08 | TestFactoryAlertJourney uses real temporary SQLite, production policy/source helpers and captured HTTP. Disk and no-progress openings, unchanged deduplication, store restart retention, two affirmative recovery observations and one all-clear per confirmed opening pass offline. |
| T-09 | Final complete affected-package runs include the retained compatibility baseline and alert integration cases. Intended transport difference: preserve the full Discord retry delay beyond one hour. Ordinary report identity/recovery, routing, new conversation, notice purge/fences and scheduler invariants remain covered. |
| T-10 | Discord, configuration, doctor and overview guidance covers controls, expiry/zone, recovery, quiet hours, uncertain/unavailable delivery, disable-without-delete fallback and safe disposable installed checks. Configuration edits explicitly require restarting only the test daemon. |

## Rule and boundary evidence for Validate

- Disk: TestFactoryRuleBoundaries covers below/equal/above threshold and probe errors; the journey covers opening/restart/two-clean recovery. Equality never opens or clears an alert, while scheduler behavior is unchanged.
- Pool/progress: TestFactoryAlertObservationContinuesWithFullPoolAndDiskHold, TestFactoryAlertBackoffExcludesPendingButStillObservesRunningWork, TestSavedResultRevisionSuppliesDurableProgressEvidence, TestFactoryAlertNoProgressNeedsTerminalSourceEvidence and source tests cover actual due work, ignored discovery-only flags, unpublished Plan/graph waits, future schedules, approved Doctor holds including parent writers, future quota continuations, current continuation identity and progress/settlement. Monitor observations do not reserve slots.
- Provider: TestProviderThresholdsAndSourceRecovery, TestProvider5xxUsesRollingConsecutiveWindow, TestProviderOlderOutcomeDoesNotDefeatLaterSuccess and TestProviderUnknownOutcomeBreaksRecoveryStreak cover immediate typed authentication/quota, two consecutive source-local refusals, three consecutive 5xx in fifteen minutes, deduplication, later affected-source success and unknown/error streak interruption. Adapter observer tests preserve cancellation and returned retry outcomes.
- Stopped queues/dead dispatches: TestFactoryAlertQueueCaptureRequiresConsumedSourceToRecover, TestFactoryAlertDeadDispatchKeepsEvidenceAfterCleanup and source settlement tests require the original input consumption/explicit settlement or terminal dispatch settlement; cleanup never manufactures missing evidence.
- Report delivery: TestAlertReportOriginalUncertaintyAndSettlement, TestAlertMigrationPreservesUncertainReportsWithoutHistoricalSend and TestAlertReportRecoveryOnlyUncertaintyRetainsOriginalSource preserve the original timer/source, approximate provenance for old records, and explicit confirmation/settlement. No report replay is exposed.
- Controls: duration/alias/replay, DST/next-clock/tomorrow, malformed reserved grammar, no-provider admission and incomplete-history ordering tests cover authenticated fixed control behavior. Independent scan capacity rolls back without losing reserved commands; control scan failure still permits ordinary conversation intake.
- Delivery: uncertainty/fence/backoff, destination/owner changes, quiet digest reconciliation and actual serialized mention tests cover crash/send boundaries, permission errors, malformed IDs, history limits, full retry delay, immutable attempted bytes and confirmed-opening-only all-clears.
- Lifetime/readiness: TestFactoryAlertMonitorContinuesWithoutSlotsAndJoinsCanceledSend covers cancellation and joined shutdown independently of a full pool. Projection tests cover stale/unavailable observation, hidden subjects and mixed-grant digest counts.

## Checks and limits

All package runs use a finite 35-minute Go timeout and local temporary stores/captured transports. Race instrumentation follows go-stack and the Makefile: full runs use race except SQL-heavy store and cli; store and cli also run their named Makefile concurrent race selections. Complete affected packages were started in parallel, with no overlapping run of the same package. The whole repository test suite and full check were not run.

The initial complete store/daemon checks caught new-table archive classification and exclusive scheduler-clock violations. Both were fixed, then their complete runs passed. A later test-only variable rename caused a build error in the alerts test package; it was corrected and the entire package passed again. These were caused by this change, not pre-existing failures. Complete daemon runs without race also passed during repair; the final daemon run adds the full race instrumentation required by the Makefile. No remaining pre-existing test or lint failure is known.

The approved architecture identified existing instruction prose in the conversation recovery path as an agent-guidance standard violation. It remains untouched as directed by that architecture; this story adds no agent behavior guidance in Go. No existing feature is retired. Manual AC-002 through AC-007 remain pending; deterministic proof is separate from acceptance and installed behavior.

## Implementation decisions for review

- The observation clock is injected independently from the scheduler clock, preserving the repository rule that only Tick reads its scheduler clock.
- Saved application results have revisions but no event timestamp. A changed revision supplies progress at its first successful observation, retained across restart; this can lag a save by one monitor cadence.
- Configured alert intervals are validated from one minute through seven days, keeping arithmetic bounded and matching the command horizon; command durations retain their separately required seven-day maximum.
- The transactional alert journal has a 16 MiB fail-closed cap. Unresolved state is preserved rather than purged to fit; reaching the cap makes monitoring unavailable and prevents unjournaled sends. This limit is documented.

## Executed complete-package results

| Package | Complete command | Actual result |
| --- | --- | --- |
| agent | `go test -race -timeout=35m ./internal/agent` | ok   github.com/cloudvane/litefactory/internal/agent 4.305s |
| alerts | `go test -race -timeout=35m ./internal/alerts` | ok   github.com/cloudvane/litefactory/internal/alerts 22.867s |
| cli | `go test -timeout=35m ./internal/cli` | ok   github.com/cloudvane/litefactory/internal/cli 36.378s |
| codex | `go test -race -timeout=35m ./internal/codex` | ok   github.com/cloudvane/litefactory/internal/codex 152.360s |
| config | `go test -race -timeout=35m ./internal/config` | ok   github.com/cloudvane/litefactory/internal/config 26.121s |
| connector | `go test -race -timeout=35m ./internal/connector` | ok   github.com/cloudvane/litefactory/internal/connector 68.070s |
| doctor | `go test -race -timeout=35m ./internal/doctor` | ok   github.com/cloudvane/litefactory/internal/doctor 94.107s |
| harness | `go test -race -timeout=35m ./internal/harness` | ok   github.com/cloudvane/litefactory/internal/harness 92.337s |
| messaging | `go test -race -timeout=35m ./internal/messaging` | ok   github.com/cloudvane/litefactory/internal/messaging 316.133s |
| messaging/discord | `go test -race -timeout=35m ./internal/messaging/discord` | ok   github.com/cloudvane/litefactory/internal/messaging/discord 5.579s |
| overview | `go test -race -timeout=35m ./internal/overview` | ok   github.com/cloudvane/litefactory/internal/overview 7.538s |
| store | `go test -timeout=35m ./internal/store` | ok   github.com/cloudvane/litefactory/internal/store 138.517s |
| daemon | `go test -race -timeout=35m ./internal/daemon` | ok github.com/cloudvane/litefactory/internal/daemon 1292.595s |

Additional executed checks:

- Store named Makefile concurrency selection: `go test -race -timeout=35m -run "^(<current RACE_TESTS_store joined by |>)$" ./internal/store` — PASS (182.153s), including the new concurrent alert transaction test. The exact selected test names are retained in the Makefile.
- CLI named Makefile concurrency selection with the same anchored selection form and finite timeout — PASS (100.040s).
- `go build ./...` — PASS (no output).
- `make lint PKGS="./internal/agent ./internal/alerts ./internal/cli ./internal/codex ./internal/config ./internal/connector ./internal/daemon ./internal/doctor ./internal/harness ./internal/messaging ./internal/messaging/discord ./internal/overview ./internal/store"` (using a writable temporary lint cache) — PASS, pinned golangci-lint v2.13.1 reports zero issues.
- `gofmt -l` on affected Go package directories — PASS, no output.
- `git diff --check` — PASS, no output.
- Relative-link existence check on the four changed owner documents — PASS, 51 links resolve. No Markdown wording assertions were added.

Final complete daemon race run passed in 1292.595 seconds (exit 0). Every affected package has a successful complete run and required race coverage. Engineering is complete; acceptance and installed Discord behavior remain pending.
