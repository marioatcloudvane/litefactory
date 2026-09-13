# Meter provider source collection

The collectors read usage from the launched process and its associated local session files. They never infer actual models from the configured default or change provider credentials and permissions. Live collection excludes historical consumption; the separate preview-first `meter recover` command can append evidenced missing historical descendants to a selected finalized attempt.

## Claude

Metered launches forward subagent messages. The collector combines those messages with associated root and child JSONL transcripts, retaining message model names and parent tool identities. Before a resumed launch, it records message counters and file identities so existing work does not become new consumption. Repeated messages contribute only newly observed consumption. Provider-inclusive summaries remain reconciliation evidence.

The implementation was based on inspection of the Claude Code 2.1.266 package. The inspected executable SHA256 was `553d1b9e9e7068b275c0a783c7e139ff6503096f286e674c8c919379fb0eca62`. The [Agent SDK TypeScript reference](https://code.claude.com/docs/en/agent-sdk/typescript) describes `forwardSubagentText`; the installed source established the message and associated-transcript shapes used by this collector.

## Codex

The collector reads associated rollouts using session metadata for ancestry and matching turn context for effective model attribution. Exact `token_usage_record` response records supply exclusive token quantities. Lifetime summaries are never added to those quantities. Prelaunch file boundaries and historical response identities prevent resumed or replayed work from being charged again. Copied parent-thread records in forked histories do not become child usage.

The implementation was based on [Codex commit 41e22fee981a63b3698df7ed36bad393cda24715](https://github.com/openai/codex/tree/41e22fee981a63b3698df7ed36bad393cda24715), tagged `rust-v0.153.0`: protocol `TokenUsageRecord`, session and turn metadata, and the history wire encoding. Effective turn model attribution does not establish any downstream server alias resolution. Older formats without exact response records remain visibly unmeasured.

## Limits and validation status

Both collectors use bounded read-only discovery, descriptor type and identity checks, and nonblocking/no-follow opens. Final collection has a two-second bound and can drain observed associated files after subprocess cancellation without changing the subprocess outcome. Missing, malformed, replaced, unresolved or late sources retain partial/unknown coverage rather than becoming measured zero.

Claude limits associated discovery to 4,096 files, 8,192 entries and depth 8, with separate 128 MiB budgets for prelaunch baseline reads and final reads. Codex limits discovery to 100,000 entries and 4,096 source files, individual records to 2 MiB and total reads to 256 MiB; historical response identity tracking is bounded at 100,000 entries.

These are implemented source integrations, not a claim of completed provider qualification. Run 19 intentionally did not execute tests or paid provider probes, following the owner's instruction. Version-pinned real root, parallel, nested, failed-child, model-switch and resume captures remain deferred. Until that evidence is available, reported completeness remains qualified. Source inspection and successful build/static analysis are recorded separately from real-execution evidence.


## Historical recovery and refusal evidence

Recovery shares Codex response parsing and exact model context with live
collection, but bounds attribution by original attempt windows and verifies
exclusive ownership before append. An unchanged ancestor remains available for
ancestry even when it contributes no new usage. Final live discovery drains
persisted delayed records for at most two seconds; an exhausted drain deadline
is not itself request cancellation. Late/missing sources remain incomplete.

The retained Codex 0.153.4 historical facts from 2026-09-09 are exercised in
isolated SQLite/source tests: run 5 gains $6.754276 and reaches an observed
$11.903026 at original recorded rates, without altering later resumed runs.
[Fixture provenance](../internal/store/testdata/meter_recovery_history.md) records
source hashes and extracted response facts; the minimal source envelopes and
nested/delayed/limit topologies are synthetic. This is offline historical
reconciliation, not an authentic current-hierarchy or invoice certification.

The retained Claude resume refusal reports zero turns and zero cost. Its process
version is independently unknown; the successful fallback reported 2.1.267.
Presence-aware refusal classification requires structural refusal and affirmative
terminal zeros with no contradictory activity. It preserves unknown model/token
pricing and separate fallback facts. Missing, interrupted, nonzero or conflicting
cases are never relabelled as proven zero-cost. No paid nested capture or live
runtime repair was performed to develop these changes.
