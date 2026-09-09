# Terminal quota diagnostic fixture

`quota-authentic.jsonl` contains the selected authentic diagnostic fields from
September 5, 2026, stream `100/0006.jsonl` lines 85–86, preserved in the supplied
session's PROVIDER_EVIDENCE.md. Historical Codex CLI version is **unknown**.
No conversation handle, prompt, generated work or credential was copied.
Invocation bounds were 15:24:49.594876Z–16:18:38.854242Z. The clock text has no
zone; Europe/Vienna is the adopted machine-local assumption, not recovered
historical metadata. Under that assumption the due instant is 20:26Z.

Supported classification is the provider-owned `error.message` or
`turn.failed.error.message` beginning `You've hit your usage limit.` with
`try again at` followed by a local 12-hour minute clock, optionally dated.
Generated `agent_message` text never supplies quota evidence. Missing or
unsupported reset text remains a verified report-only interruption. Both
observations must have consistent machine-local timezone rules; time-only
observations must share a local date. Gaps, folds and nonfuture minutes fail
closed. Existing child environment and CLI transport remain unchanged.

All additional constructed cases in tests are synthetic. After a CLI upgrade,
record the binary version separately, collect an already-occurring refusal
without provoking quota exhaustion, sanitize only diagnostic fields, and replay
through the existing fake CLI runner. No current binary version is claimed as
verified by this fixture. The prior research separately inspected upstream
rust-v0.153.4; that does not identify the captured executable.
