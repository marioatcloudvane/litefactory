# Auxiliary caller contract inventory (#264 US-005)

The shared application schema (1), submission contract (2), workflow version and
result revision are independent. Auxiliary callers preserve their existing typed
payloads and owner validation; none receives application writer or receipt authority.
This inventory is a source-level mapping at base
`16634ed33075ea3acfc449bcb6316163bdc0b807` plus #264's uncommitted implementation.
Executed checks must identify the complete changed state, not claim that base contains
this adoption. No live provider invocation is required or established by this table.

| Caller and owner | Existing contract and unsupported input | Shared convention / explicit exclusion |
|---|---|---|
| Mode classifier: agent.Classifier, agent.ParseMode; harness.Runner.Classify and codex.Runner.Classify; daemon mode selection | Tool-free request returns one of research/plan/implement/validate/spike. Both runners refuse other text or failed terminal output; daemon owns persisted selection. | Closed vocabulary and failure preservation apply. No application schema/version envelope: adding one would break the one-token routing contract and grant no useful result revision. Existing `(Mode, error)` remains the boundary; no application receipt is synthesized. A selected `- Mode: X` remains authoritative. |
| Schedule advisor: agent.ParseScheduleProposal, daemon.proposalAdapter, validatedProposalItems and owner controls | Strict JSON items of payload/due_at, unknown fields and trailing JSON rejected. Daemon validates collection, payload commands and due times; authenticated accept/reject controls own scheduling. Direct `/schedule` grammar is owned by schedule.Parse, with ErrInvalidRequest. | Separate validation and execution authority apply. This payload has no version field; application/version fields are unsupported rather than inferred upgrades. Preserve ScheduleProposal and errors with caller context. Do not map a proposal, parser success or result recommendation to scheduled work, receipt acceptance or owner approval. |
| Conflict advisor: agent.ParseRegionConflictResolution / ValidateRegionConflictResolution; git and turn confirmation owners | Region format version 1, exact file/region identities and coverage; unknown fields, unsupported/legacy versions and incomplete coverage refuse. Factory validates reconstructed bytes, bounds and fresh base before apply. | Explicit independent version and fail-closed validation apply. Region version 1 is not application schema 1. Preserve ConflictResolution and `(value, error)`; no application revision/receipt can replace the region/base checks or separate owner confirmation. |

No new wire format, error-code layer or migration is useful for these three
bounded callers: each has a distinct existing parser and authority. Accordingly,
US-005 explicitly excludes them from application-result serialization while retaining
these shared principles. Transport stdout, usage transcripts, diagnostics and process
outcomes remain provider/runner concerns. In particular `agent.RunResult` termination
causes must survive application rejection. Unsupported input tests belong at each
existing parser/owner boundary, including rejected application-shaped input, unknown
mode, schedule version/extra fields and conflict version/identity mismatch. Temporary
fixtures verify local semantics only; #274/#296 retain authentic host obligations.
