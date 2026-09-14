# US-002 implementation — run 0007

Identity: #276 / 5442842524; revision `ac14c8f3efc72a7b4126389cc58165aef50d57f4567c9f22e13bf4dc0375523a`; dependency US-001. Owner reversal of #264 prerequisite applies: reusable foundation over delivered #263, no production adoption wiring.

Production implementation complete; new story tests remain delegated to the test specialist. No acceptance/review or aggregate completion claim.

Files:
- `internal/submission/tools.go`: immutable server-local catalog/profile, tool envelope, private client List/Call.
- `internal/submission/broker.go`: explicit AllowTools option and authenticated list/call routing.
- `internal/submission/client.go`: existing durable correlation journal reused for tool submissions, exact raw contribution byte preservation.
- `internal/turn/mcp_tools.go`: guard/service binding, full catalog/result-binding/read/edit permission digest, one-call shared service dispatch.
- `internal/turn/agent_results.go`: MCP-only unsafe Plan route refusal after existing shared validation.

Consumer seam: bind existing AgentResultService first; bind AttemptGuard with service.MCPPermissionDigest(); construct NewToolDispatcher(guard, service); start broker with AllowTools:true and Handler:dispatcher.Handler. submission.Client{DescriptorPath:path} provides List(ctx) and Call(ctx,name,arguments), returning submission.Response. Raw arguments are exact shared result create/update input; receipt takes only request_id. Client-generated durable request ID is independent of MCP IDs. Every operation obtains/releases guard.Context, propagating transactional authority into the result service/store.

ToolProfile never touches agent.Request.Permissions or native host inventory. Only communication_preflight, communication_submit and communication_receipt are registered; no future creation, shell, execution, waiver, dispatch, mode or acceptance tools are advertised. Existing shared validator enforces factory-bound mode, targets, frozen revisions and retained assessment provenance. The dispatcher constructor rejects a mismatched permission digest or result binding.

Safety disposition: accepted MCP Plan input is limited to discussion without proposed stories. Completed or story-bearing assembled Plan candidates, including edits against retained proposals, return mcp_creation_refused and are not retained as accepted snapshots. Existing publishable Plan exact replays cannot be submitted through MCP; authorized receipt lookup remains available. This explicit unsafe-route refusal prevents a deferred proposal publication from dropping policy during recovery. Ordinary JSON/helper inputs retain existing behavior. No creation gate state can widen this initial catalog.

Executed compatibility check after final source changes:
`go test ./internal/submission ./internal/turn -run 'TestAgentResultService|TestAgentResultReader|TestBroker' -count=1`
Passed: submission 0.646s; turn 1.791s.

No subprocess host integration, forge effects, daemon/runtime mutations, new module, dist writes, commits or pushes.
