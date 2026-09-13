# Historical recovery fixture provenance

`meter_recovery_history.json` retains the sanitized run-5 facts extracted in
`RUNTIME_EVIDENCE.json` during issue #211 Research. Its `provenance` object records
the retained evidence document's SHA-256 and repository path. Each child retains
the original rollout SHA-256, original source line numbers, session metadata,
response identity/model, timestamp and exclusive counters. No transcript content,
credentials or prompts are present.

The facts describe Codex 0.153.4 on 2026-09-09: three parallel children, 63
exclusive responses. The test reconstructs minimal JSONL envelopes and synthetic
turn IDs because full original source envelopes are not retained. Temporary
SQLite run/attempt allocation is synthetic; the original attempt windows and
root counters for runs 5–8 are retained. It preserves unknown cache-write for
the reconstructed historical root. Fixture clocks are UTC, not filename-local
times. Synthetic interruption, overlap, conflict, and source-change schedules
are negative tests, not observed incidents.

The recorded rates are $10/input-uncached, $1/cache-read and $50/output per
million. The expected additional observed child usage is $6.754276; the historical
root's $5.148750 becomes a resulting observed subtotal of $11.903026. Runs 6–8
remain $1.746696, $0.326984 and $0.558352. This proves enumerated offline behavior
at the original rates, not a complete bill, fleet accuracy, or authentic current
provider hierarchy capability. Applying recovery to a live ledger is a separate
explicit operator action.

The CLI test's small current-window records are entirely synthetic. It exercises
the production preparation/reconciliation/command path; it does not claim to
reproduce historical provider traffic.
