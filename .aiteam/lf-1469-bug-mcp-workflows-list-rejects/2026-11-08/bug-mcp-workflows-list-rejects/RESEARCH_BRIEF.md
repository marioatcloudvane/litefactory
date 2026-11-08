# Research brief: MCP workflow call envelope

## Goal and state

Recheck #1469 and settle the bounded repair scope for authorized ChatGPT workflow discovery. Research is complete; implementation, regression execution, deployment and live acceptance are not. Inspected commit: `dd8520545072756669ad2ec52ed2b1539199ce09`.

No earlier brief or saved result for #1469 was found in this worktree. The supplied comments and the issue's fetched comments agree. The owner's substantive answer already settles scope; the previous request for another explicit go was unnecessary. Research does not itself authorize a mode transition.

## Outcome ledger

### S-01 mode=bug, size=small — one public transport envelope defect with shared callers and bounded regression coverage (agent's call)

### O-01 Accept an optional object-valued MCP params._meta for workflow calls and the other families sharing the strict public connector check (owner said: A-01)

### O-02 Authorized workflows_list calls with empty or project-filtered arguments must return normal successful catalogue results with and without metadata through the actual HTTP MCP route (owner said: A-01)

### O-03 Verify that workflow_describe, workflow_propose, workflow_continue_propose and workflow_job_get follow the same envelope rules without bypassing their normal argument and authorization checks (owner said: A-01)

### O-04 After implementation, retry the original ChatGPT calls and retain sanitized wire-level evidence if the call still fails or metadata is absent; stop and report the mismatch instead of claiming resolution (owner said: A-01)

### O-05 Refusal messages must accurately describe permitted envelope fields without echoing supplied values (agent's call)

### K-01 Preserve rejection of other unknown params keys, unknown outer request keys, duplicate keys and malformed envelopes (owner said: A-01; agent's call for existing duplicate rejection)

### K-02 Keep public tool schemas, argument validation including empty-optional normalization, grants, repository restrictions and authorization unchanged (owner said: A-01)

### K-03 Metadata carries no authorization or tool inputs and must not enter persisted workflow state or secret-bearing logs (agent's call)

### N-01 Exclude private agent transport redesign, new protocol versions, progress notification support, workflow business changes and permanent request-body logging (agent's call)

### N-02 Exclude deployment and code changes from this Research run (agent's call)

### A-01 Earlier scope question — owner replied verbatim (owner said: supplied earlier owner comment)

> q1=b
>
> I agree with the proposed change to accept the optional MCP `params._meta` object while preserving strict rejection of other unknown fields and keeping authorization and tool argument validation unchanged. Please extend the plan with an end-to-end regression test that sends a complete JSON-RPC `tools/call` request through the actual HTTP MCP endpoint, not just a unit test of the parser. Cover `workflows_list` with empty and project-filtered arguments, both with and without `_meta`, and verify that the other workflow tools follow the same envelope rules. After implementation, retry the original ChatGPT `workflows_list` call to confirm the reported failure is resolved. Since `_meta` is still a hypothesis, if the real client request does not contain it or the original call still fails, report the observed wire-level mismatch and stop rather than declaring the bug fixed.

### A-02 Current recheck request — owner: "recheck this" (owner said: comment 6721201189)

## Findings and evidence

**Observed in code:** `internal/connector/mcp.go:278–315` rejects every params member except name and arguments for action, workflow, advisory and brainstorm tools, and strict decoding repeats that restriction. All five workflow tools use this branch. The matching error at line 800 is the reported error text. Therefore an otherwise valid call carrying `_meta` deterministically reaches this refusal according to source inspection; no runtime reproduction was executed here.

**Observed contract:** `internal/connector/durable_workflows.go:38–57` allows project, cursor and limit with no required fields for workflows_list. Its schema and parser share these field sets. Metadata belongs beside arguments, not inside them. The reported client rejection of nested name/arguments is consistent with the schema and is not a repair target.

**Protocol evidence:** The supported MCP revision 2025-06-18 defines request params metadata as an optional object, and progress notifications are not mandatory: [official schema](https://github.com/modelcontextprotocol/modelcontextprotocol/blob/main/docs/specification/2025-06-18/schema.mdx). The connector advertises 2025-06-18 and 2025-11-25 (`internal/connector/mcp.go:23–25`); no newer protocol support is part of this work.

**Transport distinction:** `internal/connector/transport.go:119–122` mounts the remote owner's `/litefactory/mcp` route directly on the connector handler. The separate private agent server in `internal/mcpadapter/server.go:218` calls `submission.DecodeToolCall`; `internal/submission/tools.go:121–144` already accepts metadata, and `internal/submission/mcp_metadata.go` validates it independently. Repairing that private path would not repair the public connector. The exact deployed endpoint/build used by the failing ChatGPT call remains unobserved.

**History:** Commit `97c2b6e4` introduced the inspected strict envelope block. Commit `9789c853` normalized empty optional tool arguments and added catalogue handler coverage; it did not add metadata acceptance. Closed issue #1421 concerns that separate empty-argument defect, while closed #1410 concerns refusal messages. Both are related context, not proof of this failure's cause. No duplicate repair issue was found in the repository issue search for MCP envelope. Recent changes `98c94108`, `ce9e1cb3` and `323ba7a9` concern Plan/guidance and do not remove the current rejection.

**Prior research:** The #1108 brief documents the public HTTP connector and secret-free logs; the #1271 brief preserves the public/private boundary, tool behavior and grants. Their paths are `.aiteam/lf-1108-mcp-1-remote-mcp-endpoint-with-owner/2026-10-25/mcp-1-remote-mcp-endpoint-with-owner/RESEARCH_BRIEF.md` and `.aiteam/lf-1271-separate-remote-owner-mcp-access-from/2026-11-03/separate-remote-owner-mcp-access-from/RESEARCH_BRIEF.md`.

**Authentic retry limitation:** This run discovered `mcp__codex_apps__litefactory_workflows_list` and attempted both `{}` and `{"project":"marioatcloudvane/litefactory"}`. Both returned `MCP tool call requires approval, but approval policy is never`. Neither reached a confirmed server execution. This is a session permission limitation, not a reproduction of the reported server error. No raw ChatGPT request or server-side capture was available. The earlier claim that no live tool exists is now superseded; successful execution remains unavailable.

**Inference:** Standard metadata explains the reported refusal, but a different unknown envelope member, wrong wrapping or a differing deployed build could also explain it. Do not state that ChatGPT sends metadata without a capture. Local compatibility success and reported-bug resolution are distinct acceptance claims.

## Behavior and acceptance scope

Use the real public HTTP route with complete JSON-RPC requests, appropriate content/accept headers and a fixture-issued authorized bearer token. Existing `TestDurableWorkflowMCPJourney`, `TestWorkflowsListTreatsEmptyOptionalFieldsAsOmitted` and `internal/connector/http_test_helpers_test.go` show the authenticated handler setup. These helpers currently call ServeHTTP with a recorder; a loopback HTTP server around the full Handler is the clearest interpretation of the requested endpoint regression (agent's call). Parser-only checks are insufficient.

For workflows_list, exercise empty and project-filtered arguments with absent metadata, an empty metadata object and representative object metadata. Require a successful catalogue response with equivalent applicable results, not merely absence of the old error. The original project string must remain in live retry evidence; local fixtures may use a registered test repository.

For each of the other four workflow tools, use valid fixture inputs with and without metadata and establish equivalent normal dispatch/results; arbitrary invalid arguments that produce a different error are insufficient proof of a working call. Include representative action, advisory and brainstorm cases because they share the repaired condition. Use local providers and fixtures for proposal calls; no live mutation is required.

Reject metadata values null, array, string, number and boolean; retain unknown-field, duplicate-key and malformed JSON refusals and prove rejected calls do not reach providers. Metadata extensions stay opaque. Existing whole-request size limits remain in force. Valid progress tokens (string or number) must be accepted; existing private metadata validation is useful evidence, but importing private broker policy or its independent limits is not required by this scope. Do not expand this into transport unification.

Keep authorization and invalid tool-argument regression coverage active. Post-change checks must include the connector regressions, applicable repository Go build/vet/format and lint requirements; these are future checks, not executed evidence. No tests or builds were run during Research.

Live acceptance remains O-04: after the changed server is available, retry both original calls from ChatGPT. Record build/endpoint identity and sanitized envelope keys/types when obtainable, never credentials or private input values. If approval restrictions prevent execution, report live acceptance pending; a simulated request cannot substitute. If the real request lacks metadata or still fails, report the actual mismatch and stop under the owner's explicit condition. This does not prevent handing over the settled compatibility repair scope now.

## Impact analysis

- **Touched:** public connector envelope validation and its error text; connector HTTP workflow regression coverage and representative shared-family coverage; no team or skill change. Applicable standards inspected: `.golangci.yml`, existing connector HTTP tests, public transport bounds and supplied plan-go-guidance.
- **Depends on it:** ChatGPT and other remote MCP clients, workflow discovery/proposals/job reads, action/advisory/brainstorm callers through the strict branch; factory authorization and workflow providers retain their existing ownership.
- **Superseded:** the two-field-only restriction is replaced by acceptance of optional metadata; no service, endpoint or persisted format is retired.
- **Data and compatibility:** no migration, stored grant rewrite, new scope or team release is needed based on inspected connector and submission contracts; existing requests without metadata remain equivalent, and metadata cannot confer authority.

## Handoff

The scope is settled with the assumptions above. No new owner question is needed. The owner retains the `/plan` gate. The small repair could also fit a Bug workflow, but this Research result does not select or start another mode.
