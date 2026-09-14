# Research brief: native host integration and verification

Research run 0002 — 2026-09-14. Status: complete for the requested lightweight implementation overview. Implementation and authentic host verification remain open. The current recommendation below supersedes run 0001's proposal to wait for #264 lifecycle adoption.

## Current recommendation — run 0002

### Settled scope

The owner asks to defer #264's broader work, permits the minimum necessary connection, and explicitly wants MCP tools added alongside existing tools. Proceed on that basis without another scope questionnaire. Deliver a usable additive MCP connection for both native hosts, concise instructions explaining the available operations, and authentic verification. Existing mode permissions still apply: Validate keeps its narrow native read surface, no shell, no fresh checks and no unrestricted delegation. Additive does not mean enabling every native tool or ambient MCP server.

The existing neutral contract already says providers merge MCP with permitted tools/servers (`internal/agent/mcp.go:9`). The initial surface should be the landed handlers with their current mode and grant restrictions; do not invent additional tools or silently grant creation. Describe only handlers actually installed for that attempt. Keep current native tools available for work the MCP catalog does not cover.

### Proposed implementation tasks

These are a Research task outline, not frozen stories or an implementation plan.

1. **Connect the existing services to a real attempt.** Add the smallest shared preparation/cleanup seam needed to construct trusted result context and grants, bind the existing result service and attempt lease, assemble installed family handlers and their permission digest, start the tool-enabled broker, and populate `Request.MCPLaunch`. Reuse the existing actual-launch boundary and descriptor ownership where appropriate. Resolve runtime identity once at factory startup; bind/revoke per actual launch, including resume/fallback, and close transports/files on exit or cancellation. Honor existing auto-start configuration. Carry a copied permitted-tool list from the same server profile in the neutral handoff so providers do not invent catalogs. This limited runtime plumbing is unavoidable if agents are to use tools now; it does not include #264's broader result-driven orchestration.
2. **Register MCP with Claude Code.** Generate private per-attempt structured configuration for the factory executable and adapter arguments, pass `--mcp-config`, retain `--strict-mcp-config`, and authorize exact adapter tool names through a separately validated registration surface. Preserve the existing native tool ceiling. Registration must not replace native `Tools` with an MCP-only list or loosen Validate's native ceiling.
3. **Register MCP with Codex CLI.** Inject the corresponding `mcp_servers` entry with command, args, minimal env and exact `enabled_tools`; use trusted structured/encoded values and no persistent user-config edits. Keep existing sandbox/config controls and Validate's disabled shell/multi-agent features. Provider filtering applies to this server's MCP tools, not the host's entire native tool set.
4. **Tell agents how to use the addition.** Append a short attempt-scoped MCP usage section only when a live registration exists. Name available operations, include valid minimal call shapes where schemas are generic, and explain preflight → repair → submit → receipt for covered operations. Preserve existing helper/final-output compatibility. For Validate, explain the permitted live checkpoint/receipt path alongside retained-evidence limits; reconcile already-recorded contributions before terminal fallback so the same contribution is not applied twice. Do not introduce an MCP-only instruction or make every normal run depend on a mandatory MCP round trip. The designated verification scenarios must exercise it deliberately.
5. **Verify the wiring and compatibility in repository tests.** Cover both runner configurations, unchanged permitted native tools, exact MCP catalogs, disabled/absent registration, quoting, descriptor isolation, shared-runtime concurrent attempts, cancellation and resume/fallback cleanup. Exercise the live preparation seam and Validate's live-checkpoint/terminal-fallback interaction. Reuse landed family semantics tests and run `make check` for the eventual implementation.
6. **Capture real host evidence and document delivery limits.** Perform authorized top-level Claude and Codex runs, with a working-mode and a Validate scenario on each host. Record host version, source/transcript locator, discovered tools, a correctable preflight response, repaired request, recorded correlated decision/receipt observed before exit, and effective Validate no-shell enforcement. Also observe an allowed native tool alongside MCP in a suitable working-mode scenario to demonstrate additive use. Configuration or simulated clients alone do not satisfy #274. Retain missing observations as unverified; keep the issue open for authentic evidence if captures are unavailable.

Tasks 2 and 3 can use the agreed neutral handoff independently once task 1 fixes its data contract. Usable end-to-end delivery needs task 1, task 4 guidance, and verification; flags alone would leave agents with an unusable registration. This is six bounded work areas, not a claim that the bridge is a one-line change.

Suggested agent-facing wording:

> Litefactory MCP tools are available in addition to your existing permitted tools. Use them for the factory operations they support; continue using your other permitted tools for the rest of the task. For a supported submission, preflight the input, correct diagnostics, submit, and inspect the correlated receipt. Report the returned state accurately; pending is not success. Your current mode restrictions still apply.

The actual prompt should name only the installed tools and use the current call schemas. For Validate, add: “Use retained evidence only. MCP availability does not authorize shell execution, edits, or fresh checks.” Avoid contradictory guidance claiming shell helpers are the only contribution path when MCP is present.

### Included and deferred boundaries

**Included:** minimum runtime owner and per-attempt live-service bridge, both native registrations, additive agent guidance, preserving fallback without duplicate effects, focused tests, and authentic cross-host capture. Existing family business logic, scope enforcement and durable receipts are reused.

**Deferred to #264 or later:** making application results drive scheduling/readiness or automatic mode transitions; replacing the existing contribution/completion pipeline; requiring every agent operation or run to use MCP; a broad prompt/role rewrite; new tool families, publication behavior, creation grants, or workflow semantics. Recording and observing an existing service decision does not require adopting its result as a new scheduling/completion authority (`internal/turn/agent_results.go:23`, `internal/turn/agent_results.go:35`). MCP access does not grant new publication permission.

The owner authorized minimal necessary overlap, so no question is needed to include this bridge. If implementation establishes that delivering it requires the deferred workflow migration, stop and report that concrete dependency rather than silently expanding #274. Authentic verification remains part of the supplied issue; the additive preference does not waive it.

### Focused evidence and remaining uncertainty

Run 0002 source revision: `34f4d1a7b4b384250ea649c6adbbc371c3c106c2`. A focused diff against run 0001's revision shows no changes to the examined launch spec, MCP authority/dispatcher, submission runner, or native runners. The prior missing-wiring findings therefore still apply.

- `internal/turn/mcp_launch.go:11` assigns production adoption to #264; this run's explicit owner allowance narrows the proposed overlap to necessary plumbing rather than treating #264 as a whole prerequisite.
- `internal/turn/submission_runner.go:20` is the current bound runner seam; `internal/turn/submissions.go:54` says both adapter paths call it for each actual launch. Existing session creation is gated on submission contract v2 and a managed parent, so it cannot by itself provide MCP to all ordinary Research/Spike runs. A shared optional attempt-preparation seam must accommodate those modes without requiring managed stories or converting their lifecycle.
- `internal/turn/agent_results.go:44` requires a trusted fresh context resolver and store binding. Merely passing the legacy submission descriptor to MCP is insufficient: its broker uses the legacy handler while `internal/turn/mcp_tools.go:67` requires the tool-enabled dispatcher path. Reuse owners and data, not a mismatched handler/descriptor.
- `internal/turn/mcp_attempt.go:36` creates runtime identity; `internal/store/mcp_authority.go:26` explicitly says startup only and replaces a singleton. Calling it per launch would invalidate other attempts. A once-per-runtime owner is part of the minimum correct bridge, not optional broader orchestration.
- `internal/submission/family_tools.go:14` installs known handlers and `:30` applies mode restrictions. The existing catalog supports limited installed families. No expansion of family semantics is needed to explain additive host access.
- `internal/harness/runner.go:415` validates native Validate tools separately; `internal/harness/runner.go:629` retains strict MCP config. `internal/codex/runner.go:398` builds the native args and `:408` retains Validate shell/multi-agent restrictions. Actual host interpretation and combined effective tool availability still require capture.
- `internal/turn/submission_runner.go:73` currently teaches shell-helper access, `:79` directs Validate to terminal JSON, and `:83` consumes that result after exit. Prompt guidance and terminal handling need a small compatibility adjustment for live MCP checkpoints, without removing the legacy path.

No new host invocation, external documentation lookup, tests or implementation occurred in run 0002. Run 0001's versions and test outcomes below are historical observations, not refreshed compatibility evidence. Current evidence is focused source inspection; exact runtime adapter access and provider behavior remain empirical verification work. Fully read source corpus this run: 24,072 bytes across agent/mcp.go, turn/mcp_launch.go, turn/mcp_attempt.go, turn/mcp_tools.go, turn/submission_runner.go and submission/family_tools.go. Additional excerpts and searches are separate from billed tokens, which are unavailable.

## Retained run 0001 assessment

The following assessment is preserved as historical evidence. Its wait-for-#264 recommendation and request to settle ownership are superseded by the owner-directed run 0002 scope above; its landed-code findings and authentic-evidence requirements remain relevant.

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
- 2026-09-14, run 0002: Applied owner direction to keep tools additive and defer #264 except necessary connection. Confirmed unchanged focused source at 34f4d1a, documented six implementation work areas and the startup-owned authority constraint, and preserved authentic capture as a completion prerequisite. Research complete; no new tests, implementation or live verification.
