# Recorded usage engineering proof

This records engineering execution for story-002. It does not accept the story or certify a hosted client's behavior. Three subagents built consent and boundary tests, shared accounting and documentation, and the end-to-end/provider tests. The lead reviewed their changes and built the usage boundary, sanitized projection, cache, daemon wiring and integration repairs.

## Executed complete package checks

Each package ran completely with `-count=1`, a finite timeout and background output. Final successful outputs are retained under [engineering-logs](engineering-logs). The SQL-heavy exceptions follow go-stack and litefactory-rules. No whole-suite test or full repository check ran.

| Command | Result | Retained output |
| --- | --- | --- |
| `go test ./internal/store -count=1 -timeout=45m` | PASS, 117.621s | [store.log](engineering-logs/store.log) |
| `go test ./internal/daemon -count=1 -timeout=40m` | PASS, 148.927s | [daemon.log](engineering-logs/daemon.log) |
| `go test ./internal/cli -count=1 -timeout=35m` | PASS, 42.176s | [cli.log](engineering-logs/cli.log) |
| `go test -race ./internal/connector -count=1 -timeout=10m` | PASS, 40.992s | [connector.log](engineering-logs/connector.log) |
| `go test -race ./internal/usageview -count=1 -timeout=10m` | PASS, 4.076s | [usageview.log](engineering-logs/usageview.log) |
| `go test -race ./internal/meterview -count=1 -timeout=10m` | PASS, 7.433s | [meterview.log](engineering-logs/meterview.log) |
| `go test -race ./internal/messaging -count=1 -timeout=20m` | PASS, 281.406s | [messaging.log](engineering-logs/messaging.log) |

The initial complete store and daemon runs failed because the new consent-target table lacked an archive-registry classification. The table now follows existing global consent targets. Focused archive repairs passed, followed by the complete store/daemon repeats above. These were introduced failures, not pre-existing failures. No final test failure remains.

`go build ./...` passed after the final production edits. `gofmt -l .` produced no output and `git diff --check` passed. Pinned `golangci-lint v2.13.1` through `make lint PKGS='./internal/connector ./internal/usageview ./internal/meterview ./internal/store ./internal/daemon ./internal/cli ./internal/messaging'` passed with zero issues; subsequent store and usageview edits each received another passing scoped lint run. See [lint.log](engineering-logs/lint.log), [lint-store.log](engineering-logs/lint-store.log), and [lint-usageview.log](engineering-logs/lint-usageview.log). Markdown fence balance, relative file references, and migration-version uniqueness checks passed.

## Main path and client evidence

`internal/usageview/consent_e2e_test.go:TestOwnerConsentRecordedUsage` is the single automated main-path scenario. It uses a local MCP protocol client and actual browser consent, OAuth exchange, store, connector and recorded-usage projection; only external GitHub identity is replaced. Its controlled records cover:

- Installation-owner consent to factory, repository and task usage independently of other discussion permissions; authorized discovery and revocation.
- Distinct canonical repository attribution, including matching issue/run ordinals in different repositories.
- Exact saved estimates and referenced historical rates after changing the current catalogue; reads leave ledger rows, prices, sessions and runs unchanged.
- Today, rolling 7 days, explicit rolling 30 days, omitted 30-day default, all history and an offset-aware explicit range, with expected UTC bounds.
- A supporting follow-up beyond the first page, stable live-cursor evidence, repeated whole-snapshot totals/bounds/revision, and final-page completion. Randomized encrypted next cursors are excluded from byte equality; their page evidence is equal.
- Three successive valid questions without waiting for views to expire.
- A task with unavailable usage returning its known historical subtotal and visible gaps, with a null full total, plus a nonempty short factual explanation.
- Both serialized representations and summary bounds.

This is executed protocol evidence, not a Claude/ChatGPT hosted UI walkthrough or proof that a model follows the initialization prose. No hosted-client test installation, credentials or client/version capture was supplied or used. Manual acceptance and the external client experience remain for Validate/owner checks; no production service, paid provider, external ledger or live price list was used.

## Focused worker proof and code review

- `internal/store/connector_usage_test.go` covers additive migration compatibility, atomic consent/target creation, independent scope/target checks, refresh immutability, deletion/rebinding, expiry, owner-generation change, revocation and discovery after target loss. Existing discussion authorization remains separate.
- `internal/connector/usage_test.go` covers strict argument parsing, timezone-aware normalization, controlled UTC midnight/default/rolling/explicit windows, pre-provider denial, release-time revocation, immutable deep copies, live replay, grant/filter/tamper isolation, expiry and policy change, final retirement, oldest idle replacement, failed/cancelled construction, per-grant/global reservation bounds, Unicode clipping, escape-expanded final envelopes and nonadvancing-page refusal.
- `internal/store/meter_snapshot_test.go` covers lower source row/data limits before model filtering, failed snapshot cleanup, same-transaction membership including retired/duplicate links and members without usage, later links on fresh reads, child/foreign isolation, and trusted empty intervals without changing explicit interval validation.
- `internal/meterview/recorded_snapshot_test.go` compares captured rows to the shared meter, including retries/exclusive child rows, missing prices and frozen referenced rates; verifies deterministic repository/issue/run/model-presence ordering.
- `internal/usageview/service_test.go` covers shared redaction, raw/percent/HTML/Unicode-encoded host paths, nested strings/keys, raw-source omission, acquisition/cancellation failure, measured zero versus unknown totals and empty history, and a bounded historical-rate preview with complete per-measurement basis in details.
- Existing complete package tests also exercised exact arithmetic, refusal evidence, source reconciliation, model filters, whole-attempt bounds and retained archive usage. The reader calls the existing exclusive-row meter and exact summary; it does not implement another accounting engine.
- Production dependency review: usageview.Reader exposes only NewMeterSnapshot. Historical rates and family membership are captured in that transaction; the snapshot is closed before any initial response. No usage/price/workflow mutation, recovery, transcript import, provider fetch, command dispatch or agent runner is reachable through the usage projection. Only authorization consent/credential state and bounded temporary read materialization use store writes.
- Initialization guidance is shipped Markdown selected by the existing authorized-installed-tool loader; no prose/hash pins were added. Existing behavioral Go text remains in `internal/connector/mcp.go:overviewToolDescription`, including instructions to lead with owner needs. This is the pre-existing agent-guidance violation recorded by the approved plan; this story does not relocate or expand it.

## Implementation choices

- Summaries preview at most 50 referenced historical rates and disclose the complete count and whether more exist. Detail records carry each measurement's complete referenced basis, so ordinary summaries retain no view and a large price catalogue cannot make supporting detail inaccessible behind repeated metadata.
- Added the current path-taint rule identifier to four existing trusted-log-path lint annotations, retaining their existing rationale and behavior. Adding the usage composition surfaced these warnings; a clean archived starting commit linted without them, so they are not reported as pre-existing failures. No path construction or file-reading authority changed.

The source snapshot was not committed, staged, pushed, deployed or published by this worker. Receipt of the engineering result acknowledges input only; acceptance and any later factory effects are separate.
