# MCP story handoff from #263, run 0007

MCP is outside #263. This is a local transfer note, not a new story or an external update; the destination issue number is not supplied. Transfer these findings when that story is prepared.

- Build a thin adapter over the shared Go communication library; do not duplicate decoding, validation, patch, retry, receipt or workflow logic.
- Keep initial context retrieval distinct from preflight: retrieval supplies authorized scope/current revision/evidence/instruction references; preflight validates a candidate and grants no acceptance or revision reservation.
- Prefer typed semantic arguments; let shared code serialize and bind factory metadata/correlation and construct evidence bytes/hashes. Never generate passing proof or promote agent-reported execution to independently observed evidence.
- Support scoped reads and small updates, stale-revision diagnostics and exact-retry receipt lookup. Submission authority remains attempt-bound; read access alone grants no mutation authority.
- Provide early discovery/finalization guidance while keeping requirements, constraints and evidence obligations available before work. Detailed mode serialization may load near submission, including incomplete/blocked exits.
- Compare embedded context against tool declarations, calls/results, total input/output, cache context and retries before claiming token savings. No MCP transport or token savings have been demonstrated here.

Evidence: #263 owner discussion and RESEARCH_BRIEF.md run 0005; current context retrieval/tool names remain proposals. Production cutover belongs to its separately authorized work.
