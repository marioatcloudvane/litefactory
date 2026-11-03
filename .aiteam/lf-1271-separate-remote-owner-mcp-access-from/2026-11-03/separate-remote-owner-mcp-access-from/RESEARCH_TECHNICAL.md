# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## F-01

Current baseline already implements much of the requested cut: connector.Server owns HTTP/OAuth/browser/consent, connectorstate owns authorization values and Storage/Access/LocalGrants ports, connectorstorage hides store.DB, and read results alias factory/contracts. This is an incremental ownership extraction, not a missing remote server.

Evidence:

- `internal/connector/server.go:105`
- `internal/connectorstate/ports.go:8`
- `internal/connectorstorage/persistence.go:12`
- `internal/connector/provider.go:15`

## F-02

The remaining mixed boundary is explicit: connectorstorage implements authentication persistence and factory target access in one adapter. Access resolves repository lists and discussion/code/usage targets through store methods; those methods query sessions, repositories, execution snapshots and retained connection/ticket identities. Core ownership should be made explicit without losing the current same-backend binding, retained-target checks or existing compound persistence fences. This is a source ownership concern; no unauthorized disclosure was reproduced.

Evidence:

- `internal/connectorstorage/persistence.go:12`
- `internal/connectorstate/ports.go:29`
- `internal/store/connector_discussion.go:16`
- `internal/store/connector_code.go:18`
- `internal/store/connector_usage.go:18`

## F-03

Reuse existing projection owners: overview, taskevidence, usageview and codeevidence already supply bounded/redacted views; daemon setup composes them only when remote access is enabled. Their private readers legitimately retain store access. Connector-facing results use shared contracts, while evidence targets still use connectorstate values through store aliases. Moving every read implementation or database table is not established as necessary.

Evidence:

- `internal/daemon/setup.go:412`
- `internal/overview/service.go:24`
- `internal/taskevidence/service.go:22`
- `internal/usageview/service.go:22`
- `internal/codeevidence/provider.go:39`
- `internal/connector/code_provider.go:11`

## F-04

Current behavior exceeds the issue's historical read-only wording: the server already offers reviewed owner actions, durable workflows, task discussion/publication and stage decisions through bounded service interfaces. These do not expose coordinator execution/recovery. #1270 established a single core owner-action runtime shared with Discord. Existing tools must continue through that owner; no new write authority is implied by #1271.

Evidence:

- `internal/connector/services.go:14`
- `internal/connector/server.go:62`
- `internal/daemon/owner_actions.go:17`
- `docs/factory-core.md:101`

## F-05

Authorization is tied to current configured/stored owner identity and generation, token resource and scope, repository coverage and retained target identity. New rejects absent dependencies or mismatched authentication/access backends; local control must share the backend. MCP checks access before processing and rechecks protected results before release. Startup pin reconciliation happens in daemon composition and again during server construction; any retirement must preserve pin-away-and-back invalidation.

Evidence:

- `internal/connector/server.go:150`
- `internal/connector/control.go:44`
- `internal/connector/mcp.go:48`
- `internal/connector/mcp.go:409`
- `internal/store/connector.go:385`
- `internal/store/connector.go:539`
- `internal/daemon/remote_connector.go:28`

## F-06

Impact: daemon Setup and remote listener lifecycle, connector/connectorstate/connectorstorage, target authorization persistence, read providers, CLI local grant administration, boundary/security/transport/recovery tests, docs/factory-core.md and remote connector/grant operator guides. Consumers include existing remote clients, daemon composition, stopped-daemon grant commands and the shared approval runtime. No team or skill changes were established by the affected source inspection. Replaced direct resolution/composition paths need retirement; ordinary workflows, agent result brokers and publication authority remain core-owned.

Evidence:

- `internal/daemon/remote_connector.go:17`
- `internal/daemon/setup.go:65`
- `internal/connectorstate/ports.go:39`
- `docs/cli/connector-grants.md:1`
- `docs/guides/remote-connector.md:1`
- `docs/factory-core.md:1`

## F-07

Compatibility: current ports preserve consent/token transactions and store aliases preserve saved encodings. Existing store tests cover owner generation invalidation, refresh replay/revocation across restart, finite repository coverage and legacy grant migrations. Factory pending-state fixtures include grants and protected proposals, but are not exhaustive connector authorization upgrade/rollback proof. No schema migration or physical database split is shown necessary; a connector-specific old/current/old authorization-state rehearsal remains unverified in this pass.

Evidence:

- `internal/connectorstate/ports.go:8`
- `internal/store/connector_test.go:168`
- `internal/store/connector_test.go:202`
- `internal/store/connector_discussion_test.go:231`
- `internal/store/factory_pending_state_test.go:116`
- `internal/store/testdata/factory-pending-state/README.md:1`

## F-08

Executed successfully on bca2495749c3b7204246b185b44f9de446b31fa4: go test ./internal/connector ./internal/overview ./internal/taskevidence ./internal/usageview ./internal/codeevidence -count=1; selected connector store and factory boundary tests; targeted daemon tests for preparation refusal, listener lifecycle, shipped local control, optional-transports-disabled GitHub/Linear main path and Discord shared approvals. Connectorstorage has no tests of its own. These controlled tests do not establish live-provider operation, historical rollback, full repository checks or acceptance.

Evidence:

- `internal/connector/connector_security_test.go:98`
- `internal/connector/boundary_test.go:14`
- `internal/factory/boundary_test.go:17`
- `internal/daemon/factory_core_e2e_internal_test.go:44`
- `internal/daemon/remote_connector_internal_test.go:73`

## F-09

Context checked: #1269 core ownership and #1270 Discord extraction are merged as PRs #1365 and #1375; the current HEAD includes #1375. Their retained briefs and factory-core documentation establish one process/module, shared physical SQLite and bounded consumers. Open related MCP/rights issues #1354, #1297 and #1267 are adjacent work, not authority to absorb new capabilities. Repository open PR listing returned empty. Existing dependency guards constrain connector imports and forbid factory transport dependencies, but import tests alone cannot prove target-policy ownership.

Evidence:

- `.aiteam/lf-1269-establish-factory-core-contracts-and/2026-11-02/establish-factory-core-contracts-and/RESEARCH_BRIEF.md:1`
- `.aiteam/lf-1270-separate-discord-conversations-and/2026-11-03/separate-discord-conversations-and/RESEARCH_BRIEF.md:1`
- `docs/factory-core.md:91`
- `internal/connector/boundary_test.go:14`
- `internal/factory/boundary_test.go:17`
