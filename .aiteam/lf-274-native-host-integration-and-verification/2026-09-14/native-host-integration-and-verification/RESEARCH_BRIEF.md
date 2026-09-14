# Research brief: native host integration and verification

Research run 0001 — 2026-09-14. Status: complete for the requested source assessment and solution overview. Implementation and authentic host verification remain open.

## Request and scope

Inspect the implementations landed from #272 and #273, identify what remains for #274, and explain how to approach the solution. This is Research only: no implementation, story creation, runtime modification, or live agent capture. The supplied issue establishes the intended outcome and five completion criteria clearly enough to proceed without questions. Research completion does not mean issue completion.

Repository claims below refer to revision `ca5e7ac95e2ca92294203544dbfa0f95198e85fd`. Git history identifies `97021d6` as the #272 merge and current HEAD as the #273 merge. Current source takes precedence over historical planning claims.

## What landed

| Area | Observed implementation | Evidence |
| --- | --- | --- |
| Adapter entry point and transport | `litefactory mcp serve --descriptor <absolute-path>` authenticates the descriptor before handshake, then serves MCP over interruptible stdio. The adapter negotiates its sole supported version, 2025-11-25, and implements discovery/calls with bounded transport and cancellation. | `internal/cli/cmd_mcp.go:13`, `internal/mcpadapter/server.go:57`, `internal/mcpadapter/server.go:182`, `internal/mcpadapter/protocol.go:14` |
| Attempt authorization | Runtime-specific authority, immutable attempt lease and permission digest, per-operation checks, cancellation/replacement/revocation. Reconnecting does not create authority. | `internal/turn/mcp_attempt.go:20`, `internal/turn/mcp_attempt.go:36`, `internal/turn/mcp_attempt.go:58`, `internal/turn/mcp_attempt.go:125` |
| Neutral host handoff | Validated immutable executable/descriptor/attempt value supplies fixed serve arguments; optional `agent.Request.MCPLaunch`; strict loaded `mcp_auto_start`, default true. | `internal/agent/mcp.go:12`, `internal/agent/request.go:53`, `internal/turn/mcp_launch.go:14`, `internal/config/config.go:109` |
| Communication | Shared result validation, submit and receipt via `communication_preflight`, `communication_submit`, `communication_receipt`. Submission requires durable application correlation. | `internal/turn/mcp_tools.go:93`, `internal/submission/tools.go:48` |
| Tool families | Shared-validator feedback; repository-local issue list/get; scoped comments; explicitly granted creation; engineering/Validate evidence checkpoints; advisory activity update/get; durable scoped receipt lookup. | `internal/submission/family_tools.go:42`, `internal/turn/mcp_families.go:38`, `internal/turn/feedback.go:57`, `internal/turn/evidence.go:126` |
| Permission and replay controls | Dispatcher requires matching service scopes and digest. Family receipts retain outcome independently of retries; creation replay rechecks authorization. | `internal/turn/mcp_families.go:49`, `internal/turn/mcp_families.go:130`, `internal/turn/mcp_families.go:143` |
| Offline verification | Real local MCP pipe/socket/SQLite paths exercise catalog, permissions, receipts and recovery; forge HTTP is controlled test infrastructure. These are synthetic host clients. | `internal/turn/mcp_family_catalog_test.go:26`, `internal/turn/mcp_family_catalog_test.go:135`, `internal/turn/mcp_recovery_test.go:238`, `internal/cli/cmd_mcp_test.go:235` |

The catalog is deliberately mode scoped. All five modes have the three communication tools. Installed families are exposed in Research, Implement and Validate; creation is Research-only and requires the creation setting plus separate live grants to execute; evidence submission is Implement/Validate-only. Plan and Spike retain communication only. This is the landed behavior to preserve, not a proposal to expand business semantics (`internal/submission/family_tools.go:34`). With all applicable families installed, Research has 10 tools by default or 11 with creation visible, Implement/Validate have 11, Plan/Spike have 3.

Validate's MCP surface is not literally free of all durable writes: permitted checkpoint recording, advisory activity, and explicitly scoped comments exist. Its essential boundary is no shell, no fresh checks or engineering capture, no creation, and no unrelated authority. Native host permissions and server authorization must both hold.

## What remains open

All five #274 completion criteria remain unestablished at this revision:

1. **Claude registration:** `internal/harness/runner.go:586` builds ordinary run arguments and finishes with `--strict-mcp-config` at line 629, but emits no `--mcp-config`. Its comment still explicitly says no MCP servers are passed. Other stateless/advisory invocations are intentionally tool-free and should stay outside per-attempt registration.
2. **Codex registration:** `internal/codex/runner.go:398` builds arguments without consuming `MCPLaunch`. Validate already disables shell and multi-agent at line 410. Delivery and user-config controls also exist and must survive additive registration.
3. **Production lifecycle seam:** focused non-test call-site searches find definitions, but no production calls to `NewAttemptAuthority`, `BindAgentResultService`, `NewFamilyToolDispatcher` or `BuildMCPLaunchSpec`, and no production assignment to `MCPLaunch`. Reading `internal/turn/submission_runner.go:20` confirms the current path still starts the older submission session and teaches shell-helper commands. This is more than missing CLI flags.
4. **Before-exit decision in Validate:** the existing Validate fallback consumes terminal JSON after the child exits (`internal/turn/submission_runner.go:87`); its own message explicitly says it does not demonstrate live helper acknowledgment. MCP must make the permitted checkpoint/receipt round trip possible while the host is alive.
5. **Authentic evidence:** `docs/story-contract.md:166` and `docs/agent-evidence-capture.md` explicitly retain the missing dual-host observation. Inspected tests and binary version probes do not close it.

The source explicitly assigns production adoption to **#264** and host consumption to **#274** (`internal/turn/mcp_launch.go:11`). This establishes an additional integration dependency beyond the supplied issue's #272/#273 description. It does not establish the current remote status of #264. Keep that ownership: host wiring can proceed against the neutral seam, while authentic ordinary-factory verification waits for lifecycle adoption. Do not silently absorb #264's broader application/lifecycle work into #274.

## Recommended solution approach

### 1. Fix the handoff contract and dependency boundary

Agree the integration seam before implementation: the factory supplies a live bound attempt, registered family catalog, immutable allowed tool names, descriptor, resolved executable and cleanup owner. `MCPLaunchSpec` currently contains only executable/descriptor/attempt; it does not carry tool names or env. Codex's required `enabled_tools` and Claude's exact permissions therefore need a trusted catalog projection. Prefer extending the immutable neutral handoff with copied approved names, derived from the same installed server profile, instead of maintaining a second catalog inside each provider.

#264's lifecycle owner should bind once per actual cold/resumed/fallback launch, build the family dispatcher/broker, honor auto-start without minting authority from configuration, supply the request, and revoke/close at the correct boundary. #274 consumes that data. Coordinate prompt and terminal handling at this seam so MCP-enabled Validate is directed to observe its actual receipt before exit rather than relying solely on post-exit terminal JSON.

### 2. Wire each native runner

Claude: generate a private per-attempt JSON MCP configuration from structured trusted values, pointing to the absolute factory binary and fixed serve arguments. Add `--mcp-config` while retaining `--strict-mcp-config`. Preserve native tool ceilings and explicitly permit only approved adapter tools through the host's supported permission mechanism. The current Validate checker rejects arbitrary additions to native `Tools` (`internal/harness/runner.go:415`), so registration must be a separately validated additive surface; a blanket permission wildcard is not a substitute. Clean up generated files on success, failure, cancellation and resume fallback.

Codex: inject a per-attempt `mcp_servers` entry with command, structured args, minimal env and exact `enabled_tools`, using correctly encoded config values. The adapter needs descriptor-based local authority, not forge credentials in env. Preserve `--ignore-user-config`/delivery strictness, sandbox selection, and Validate's shell/multi-agent disabling. Test ambient server collisions and effective configuration precedence; no persistent user-config edits.

Both runners should retain distinct attempt paths during resume/fallback, refuse invalid registration before launch, and preserve existing behavior when the spec is absent. Verify that the real host-managed adapter can read its descriptor, reach its socket and use the correlation journal under the existing boundary, especially in Validate; do not infer this from a shell-helper test.

Current official references confirm the intended configuration vocabulary: [Claude CLI reference](https://code.claude.com/docs/en/cli-reference) documents explicit MCP config plus strict isolation; [official OpenAI configuration reference](https://learn.chatgpt.com/docs/config-file/config-reference) documents command/args/env and enabled-tools filtering. Accessed 2026-09-14. These references describe configuration, not observed compatibility of this repository's implementation.

### 3. Prove mechanics offline, then capture actual host behavior

Add meaningful runner-level tests for generated config, effective allowed names, cold/resume/fallback replacement, absent/disabled policy, cleanup, encoding, credential exclusion and inherited-server isolation. Exercise the production preparation-to-runner seam once lifecycle adoption is available. Reuse existing adapter/family tests instead of redefining family validation. Run `make check` for implementation completion; this Research run does not implement code.

Then use separately authorized operator-controlled top-level factory runs, outside managed engineering sessions. Capture at least Claude and Codex in a working mode and explicit Validate runs on both (a four-scenario minimum recommendation). Validate consumes already-retained source/check evidence; it must not create that evidence or execute tests during its run.

For each host, retain native provider evidence of discovered permitted tools and actual MCP calls, a deliberately correctable preflight diagnostic, corrected input in the same attempt, submission, and receipt observation before exit. A useful checkpoint chain is `feedback_preflight(kind=checkpoint)` -> corrected preflight -> `evidence_submit` -> `receipts_get`; communication's preflight/submit/receipt chain separately proves application-result recording. Do not equate application input accepted, engineering recorded, acceptance and remote publication.

For Validate, retain effective host tool inventory and observable refusal/unavailability of shell/delegation escape paths as well as successful permitted MCP calls. A run that simply chooses not to invoke shell is insufficient evidence of enforcement. Carry request/attempt identities and ordered tool-result/exit events into sanitized evidence; preserve negative and missing observations honestly.

Record actual binary versions, capture date and environment, source locator, exact tools, effective configuration, protocol negotiation, correlated durable decision, exit ordering, sanitizer/reviewer provenance, checksum and record counts. Replay sanitized captures offline and update the documentation's unavailable-evidence statement only to the extent demonstrated. The procedure is governed by `docs/decisions/nested-agent-evidence.md` and `docs/agent-evidence-capture.md`; no nested paid host invocation or permission widening is part of this Research or implementation workflow.

## Risks and planning decisions

- Lifecycle availability is a real prerequisite; no conclusion is drawn about #264's current remote status. Plan should inventory and assign the missing seam before freezing work.
- Host schema tolerance, MCP 2025-11-25 negotiation, tool-name mapping, approval behavior, environment inheritance and strict-config precedence need actual version-specific observations. Several family input schemas are deliberately generic objects; document exact call shapes sufficiently for genuine host use without changing family semantics.
- External operator capture is not currently supplied. It remains an explicit completion prerequisite, not a reason to label synthetic tests authentic. Research can finish without it because the requested deliverable is an assessment and approach.
- The supplied immutable GitHub link to #265's archived brief returned a retrieval error. Local archived sessions were not read. Current code and supplied issue context were sufficient for this bounded assessment; historical run-specific details were not independently revalidated.
- No owner question is needed for this Research report. Before implementation, establish the lifecycle delivery dependency and external capture owner/context through Plan. Changing issue boundaries or waiving authentic evidence would require explicit scope decisions; neither is assumed here.

## Probes and validation

Environment observed: macOS/darwin arm64, Go `go1.27.0`, Claude Code `2.1.270`, Codex CLI `0.154.0`. Version commands only; no model sessions or MCP host invocations. Codex printed a PATH-alias permission warning while returning its version; this establishes neither app-server failure nor unsupported MCP functionality.

Executed successfully:

```text
go test ./internal/mcpadapter ./internal/cli ./internal/turn ./internal/submission ./internal/agent -run 'Test(MCP|.*FamilyToolProfile|.*FamilyCompleteCatalog)' -count=1
```

MCP adapter, CLI, turn and agent selections passed. Submission package reported no matching tests; do not interpret that as new submission-suite coverage. Full CI and `make check` were not run for this source-only research.

Measured selected fully read core corpus: 53,612 source bytes across ten named files (agent/mcp.go; turn/mcp_launch.go, mcp_attempt.go, mcp_tools.go, mcp_families.go, submission_runner.go, mcp_family_catalog_test.go; submission/tools.go, family_tools.go; cli/cmd_mcp.go). Additional focused excerpts, documentation and test output were inspected separately. Source bytes are not billed input tokens; billed token usage is unavailable here.

## History

- 2026-09-14, run 0001: Assessed merged #272/#273 at ca5e7ac, identified missing host consumption plus #264-owned production adoption, passed focused synthetic MCP checks, and recorded implementation/evidence approach. Research complete; no implementation or authentic host verification claimed.
