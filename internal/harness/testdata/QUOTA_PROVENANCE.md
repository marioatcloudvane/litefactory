# Terminal quota diagnostic fixture

`quota-authentic.jsonl` contains selected authentic fields from September 7,
2026, stream `70/0022.jsonl` lines 367, 374 and 375, captured with Claude Code
2.1.263. Invocation bounds were 16:28:40.207897Z–17:12:41.389633Z. The supplied
PROVIDER_EVIDENCE.md also records authentic rejected events from 2.1.261.
Prompts, tools, conversation handles and credentials are omitted. This is a
selected diagnostic excerpt, not a complete fabricated session stream.

The five-hour reset is 18:40Z; due is 18:41Z. Seven-day utilization is below
exhaustion and does not postpone recovery. Classification requires top-level
`rate_limit_info.status=rejected` and a terminal `result.is_error=true`.
`subtype=success` does not override the error bit. Overage-only rejection,
allowed/warning and warning-then-success are not interruptions. Every explicitly
exhausted window must carry usable future Unix-seconds reset evidence; malformed,
missing or stale blockers remain report-only. Repeated diagnostics are
idempotent. Both output pipes retain their existing drain behavior.

All additional constructed cases are synthetic. After upgrading, record the
binary version, collect an already-occurring rejected/terminal-error sequence
without deliberately spending quota, sanitize its diagnostic fields, and replay
it through the existing fake CLI runner alongside allowed/success cases. No
new transport or paid provider test is required.
