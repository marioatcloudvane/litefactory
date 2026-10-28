# Owner action implementation evidence

The implementation uses one transport-neutral confirmation service and the existing
natural execution journal. Domain services retain their operation receipts. The
single story main-path test is `TestPlainWordActionJourney`; it covers MCP and
Discord at both tiers with controlled HTTP/provider adapters and real domain
handlers. Owner input remains plain words; the client/factory carries references.
This is engineering evidence, not live Discord/OAuth approval, deployment or acceptance.

## Implementation choice for review

The existing authoring follow-up, log-analysis and diagnosis handlers run under the shared
bounded daemon-owned execution context. Their old background queue discarded the
reviewed authority context. Keeping exact child receipts and running the existing
handler within that context preserves authority fences after slow work. It adds
no job orchestration or stage steering. The context has a finite ten-minute bound;
an interrupted effect is reconciled or retained as uncertain, without blind replay.

## Existing Go-side agent guidance for operator disposition

Published agent-guidance rules require behavioral instructions in Markdown. The
approved plan explicitly retains the existing Go-side prose concern for operator
disposition. Existing instruction-like catalogue descriptions remain in
`internal/messaging/conversation_standards.go` (EnableStandards descriptions),
`internal/daemon/conversation_standards.go` (authorDraftText next-step prose), and
`internal/connector/mcp.go` (overviewToolDescription). This story updates stale
factual confirmation statements and puts new agent behavior in the existing
conversation agent, authoring skill and connector Markdown resources; it does
not migrate unrelated existing Go prose. Validate should assess this existing
concern independently of new runtime policy enforcement.

## Acceptance limits

Owner checks remain pending for AC-002, AC-003, AC-004, AC-005, AC-006 and AC-007.
No live connector, Discord owner pairing, real reaction delivery or deployment
was tested. Read grants remain read-only; action grants require new explicit consent.
Legacy CLI/GitHub commands and worker submission authority are preserved.

## Executed engineering checks

These are scoped checks executed during this implementation, not a repository-wide
suite or acceptance review. The final-code main path passed with race detection
in 3.860s (`go test -race -timeout=90s -run '^TestPlainWordActionJourney$' ./internal/daemon`).
Complete package runs used finite command timeouts
between ten and thirty minutes. Each row below represents its own package run.

| Package | Complete run | Result |
|---|---|---|
| `internal/owneraction` | race | passed, 30.559s |
| `internal/connector` | race | passed, 52.596s |
| `internal/messaging` | race | passed, 280.910s |
| `internal/messaging/discord` | race | passed, 7.309s |
| `internal/store` | ordinary, then selected Makefile race checks | passed, 122.416s; race 148.705s |
| `internal/cli` | ordinary, then selected Makefile race checks | passed, 39.754s; race 90.916s |
| `internal/standards` | race | passed, 52.402s |
| `internal/standardsexceptions` | race | passed, 12.191s |
| `internal/archive` | race | passed, 1.871s |
| `internal/doctor` | race | passed, 51.733s |
| `internal/daemon` | race | three pre-final-fix complete runs passed, 1233.505s, 1173.135s and 1173.729s |

`go build ./...` passed. Scoped pinned `make lint` for connector, daemon, messaging,
Discord, owneraction and store passed with zero findings. `gofmt -l .` and
`git diff --check` returned no findings. Guidance loading/captured-session
compatibility structural checks passed; Markdown wording is not pinned by tests.

The focused safeguard checks cover exact targets and current authority, concurrent
admission/cancellation/reactions, both confirmation tiers, early/expired requests,
client/grant fences, permission checks before target reads or rechecks,
authorized repository inference and ambiguity choices, known
rate-limit versus uncertain delivery, restart and receipt
reconciliation, ninety-day retention and stable authorized pagination. Additional
regressions cover a slow target recheck crossing expiry, delayed availability,
revoked-grant cancellation by the configured Discord owner, retained history after
a repository rename, exact-source diagnosis receipts and MCP operation without
Discord availability. Uncertain effects are inspected rather than replayed.

Three subagents built this story alongside the lead worker. Intermediate failures
were corrected in the story; no known pre-existing failure is being waived.

## Remaining engineering verification

The final repository-selection privacy fix changed store and messaging after the
latest complete daemon race run had started. The other scoped final-code package
runs and required selected race checks passed, as did build, lint and formatting.
A complete daemon race run against the final code is still required. Earlier
daemon passes and a targeted main-path pass do not replace that complete run.
The attempt deadline limits verification; no manual owner criterion is the reason.

The attempt is submitted incomplete solely for that required complete caller run.
Run `go test -race -timeout=30m ./internal/daemon` in the background, read its result,
and resolve any failures before claiming completion. No other test processes
remain active. No known pre-existing failure is waived.
