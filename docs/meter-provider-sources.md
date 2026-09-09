# Meter provider source collection

The collectors read usage from the launched process and its associated local session files. They never infer actual models from the configured default, import historical logs into old runs, or change provider credentials and permissions.

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
