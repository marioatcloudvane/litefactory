# Offline shared-result exchange costs

Run `python3 scripts/agent-result-bytes.py` from the checkout. The tool verifies
every SHA-256-pinned input/output segment in
`internal/agentresult/testdata/v1/exchanges/corpus.json` and prints raw totals.
`totals.json` retains that output. Ordinary Go tests independently recompute all
counts and apply both approaches to prove identical final semantic results.

The five synthetic Research tasks use one fixed small six-finding baseline.
Cold creation starts without a baseline. Continuation changes the comment;
incomplete preserves partial findings and changes the reason; blocked records an
obstruction. Stale recovery includes the rejected revision-1 request, diagnostic,
new baseline read and corrected revision-2 request. Every path counts early
instructions, task/constraints, late instruction read requests/responses, baseline
read requests/responses, contributions, all retries and the final receipt. Empty
optional preflight is not assumed or charged: these scenarios submit directly.

The full approach receives a pinned copy of the complete current contract plus the
current source workflow skill and selected-mode instructions. It reads the complete
snapshot and serializes every editable result field in an explicit update. The
scoped approach receives the small entrypoint, common and selected-mode guidance,
reads only changed authorized fields and supplies a small update. Cold output is
identical for both. Full and scoped updates both execute under application-v1 Go
validation; full reads and receipts are modeled offline adapter exchanges, not
claims about a currently wired endpoint. No legacy routing or runtime changed.

All content bytes, including JSON formatting and repeated delivery, count once per
listed segment. UTF-8 bytes are not tokens or billed tokens. No tokenizer, model,
cache hit, model late-loading, transport framing or billing assumptions are inferred.
The experiment uses an intentionally disclosed full-guidance versus selected-guidance
policy; it does not establish that production currently sends the full policy or
that a live model would use the same reads/retries. Results depend on corpus size,
instruction policy and retry frequency; no runtime or billed savings are claimed.

| Scenario | Full input | Full output | Full total | Scoped input | Scoped output | Scoped total |
|---|---:|---:|---:|---:|---:|---:|
| cold | 36403 | 1154 | 37557 | 8131 | 1231 | 9362 |
| continuation | 37889 | 1704 | 39593 | 8367 | 340 | 8707 |
| stale-retry | 39545 | 3408 | 42953 | 8773 | 603 | 9376 |
| incomplete | 37892 | 1745 | 39637 | 8482 | 549 | 9031 |
| blocked | 37886 | 1733 | 39619 | 8571 | 689 | 9260 |
| Aggregate | 189615 | 9744 | 199359 | 42324 | 3412 | 45736 |

Pinned full-guidance SHA-256: `bc12e5f627f19c573815e3b0bb130fd99535c92bc5f3824d13baadbe6c61c4b1`.
