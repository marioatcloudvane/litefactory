# Research brief

## Request

Continue issue #314 research with the owner's approval of both recommended scope answers; confirm the current defect and hand over the evidence and settled boundaries.

## Decisions

- **d1** — This continuation completes a bounded Research pass. It does not implement, deploy or repair live state.
- **d2** — Carry forward the recommended distinction between confirmed command rejection and retryable failure, using recoverable reply-before-consumption behavior without globally changing the queue retry helper.
- **d3** — The intended outcome is actionable rejection feedback followed by progress of the owner's next valid comment, without manual queue repair.
- **d4** — Owner accepted q1 option a in the supplied reply 'both your recommendations are fine!' and then instructed 'proceed': cover creation approval and preparation end to end, including malformed input, invalid mode, disabled support and confirmed proposal or authority refusal. Other command families stay separate.
- **d6** — Carry forward retries for temporary infrastructure failures, unknown internal failures and uncertain admission or publication effects. Confirmed absent, expired, superseded or mismatched proposals require corrective guidance; a disabled loaded capability differs from a failing configured dependency.
- **d11** — Carry forward one recoverable diagnostic per rejected source comment, retaining source identity, safe reason, corrective text, stable delivery identity and delivery state through consumption. Consume only after confirmed delivery; recover failed consumption without reposting or treating rejected input as fresh approval. Reuse existing records where sufficient.
- **d13** — Carry forward recovery of recorded successful admission when consumption fails, without duplicate intent or false rejection of an already admitted command.
- **d14** — Preserve identity and bot filtering, mode restrictions, proposal binding, freshness and edited-comment restrictions, existing grammar and limits. Rejection grants no authority, starts no work and discloses no secrets. Corrections use a new comment; rejected commands do not execute after later mode or configuration changes.
- **d17** — Carry forward acceptance coverage for malformed approval and preparation followed by a valid instruction; failed diagnostic delivery; restart recovery; successful admission followed by failed consumption; no duplicate diagnostic or approval effect; and unchanged transient-failure ordering. Include invalid mode, disabled support, invalid proposal references, unauthorized or edited controls, and valid approval.
- **d19** — Owner accepted q2 option a: exclude operator recovery tooling, automatic skipping and new alerts. Keep unresolved infrastructure failures ordered and retryable. Proposal-generation changes, global retry changes and live deployment also remain outside the approved boundary.


## Assumptions

- **a1** — Persistence is assessed against source revision 0a0c567e9a78a57766c6f76800743315c6b014ed, not the deployed daemon.
- **a2** — Historical #230 timing, retry count and deletion-based recovery remain owner-supplied incident evidence.
- **a3** — Existing source-keyed reply recovery is the likely reuse surface. Its sufficiency for every rejection and admission recovery case remains an engineering question for shaping.
- **a4** — This continuation uses source inspection and carries previous tests as historical evidence. No new test execution, full command-family audit or live observation is claimed.
