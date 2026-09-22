# Research brief

## Request

Continue issue #314 at shaping: settle the behavior, scope boundaries and acceptance rules for preventing rejected owner commands from wedging the task queue.

## Decisions

- **d1** — The earlier bounded investigation completed at revision 40fa12605a4c5bb5b366a6e2806add039c7cab40, and this shaping pass performs no implementation or live-daemon changes.
- **d2** — Use explicit command rejection versus retryable failure and recoverable post-before-consume reporting without globally changing the queue retry helper.
- **d3** — The proposed primary user story is that an owner receives actionable rejection feedback and the next valid comment runs without manual queue repair.
- **d4** — The proposed creation scope covers both approval and preparation, including malformed input, invalid selected mode, disabled support and confirmed authority or proposal refusal.
- **d5** — Confirmed absent, expired, superseded, mismatched or ineligible proposals are rejected with guidance to obtain the current proposal and submit a fresh command.
- **d6** — Temporary infrastructure errors, unknown internal errors and uncertain admission or publication effects remain pending for retry or reconciliation.
- **d7** — A loaded configuration that disables or lacks creation support rejects the command with a capability explanation, whereas a failing configured dependency remains retryable.
- **d8** — The logical rejection record retains task/session identity, source comment identity, reason category, safe correction text, stable delivery identity and pending or delivered state through consumption.
- **d9** — Reuse existing durable reply and recovery records where sufficient rather than requiring a new public data model or retry-counter store.
- **d10** — The only proposed public interface change is an issue reply naming the rejected command and safe corrective action while preserving existing command syntax and limits.
- **d11** — Consume a rejected comment only after confirmed diagnostic delivery and preserve its stable delivery identity across retries and restarts.
- **d12** — When delivery succeeded but consumption failed, recover consumption without reposting the reply or reevaluating the command as fresh approval.
- **d13** — When admission succeeded but consumption failed, recover the recorded admission without creating duplicate intent or falsely treating the admitted command as a fresh rejection.
- **d14** — Preserve trusted identity filtering, bot exclusion, mode restrictions, proposal binding, freshness checks and edited-comment approval restrictions.
- **d15** — A rejection neither grants approval nor launches work, and diagnostics disclose no secrets or hidden proposal content.
- **d16** — Corrections use a new owner comment, and an already rejected command does not become executable after a mode or configuration change.
- **d17** — Acceptance requires queue progress after malformed approval and preparation input, recoverable delivery across restart, no duplicate diagnostic or approval effect, and unchanged transient-failure ordering.
- **d18** — Additional acceptance cases cover invalid mode, disabled support, invalid proposal references, unauthorized or edited controls, and a valid command remaining successful.
- **d19** — Recommend excluding automatic skipping, new retry alerts, operator recovery tooling, proposal-generation changes and live deployment from this change, subject to the owner's scope answers.


## Assumptions

- **a1** — 'Still persists' is assessed against source in the supplied worktree and does not establish the live daemon revision.
- **a2** — The historical #230 timestamps, retry count and deletion-based recovery are owner-supplied incident evidence.
- **a3** — Existing recoverable replies are the intended persistence surface because earlier findings show source-keyed recovery already supports post-before-consume reporting.
- **a4** — The shaping pass uses source inspection only and carries prior test results as historical evidence rather than claiming they were rerun on the current revision.


## Open questions

### q1 — How broadly should this change fix deterministic command rejection?

_Asked of: Product Owner Questions_

- **a** — Recommended: fix both creation approval and preparation end to end, with regression coverage for queue progress and recovery; leave other command families for separate work.  *(recommended)*
- **b** — Audit and fix every owner-command family in this change, including mode, schedule, delivery, follow-up and exception controls; this expands scope beyond the confirmed creation defect.

### q2 — Should operator recovery tooling be part of this change?

_Asked of: Product Owner Questions_

- **a** — Recommended: keep this change to rejection handling and delivery recovery, with no automatic skip, new alert or operator command; unresolved infrastructure failures continue to hold ordering.  *(recommended)*
- **b** — Include an audited operator recovery command that can resolve a stuck source comment only after pending effects are reconciled; automatic skipping remains excluded, and the command's authorization and interface need further shaping.
