# Research brief

## Request

Shape the next work from the saved Spike incident findings, defining goal, scope, data, interfaces, permissions, validation and edge cases while preserving the existing discovery fix.

## Decisions

- **d1** — This pass is Research into the Spike incident. No implementation, daemon restart, commit, push or change of mode was performed.
- **d2** — Proposed goal: an owner can follow up on a Spike and receive a reliable account of preserved work and measured checks.
- **d3** — Proposed scope covers historical-result loading and Spike failure evidence and reporting, with discovery implementation pending q1.
- **d4** — Historical evidence must be validated against its recorded source authority while new submissions remain subject to the current step's permissions.
- **d5** — Historical evidence carries source instance, attempt, run and result identity and cannot grant execution, approval or publication authority.
- **d6** — Missing source authority, malformed evidence or inconsistent identity must produce a specific diagnostic without permission escalation or silently selecting older evidence.
- **d7** — Check evidence is tied to its source change and records phase, command, outcome, available exit status, elapsed time, redacted diagnostic excerpts and explicit omission status.
- **d8** — Retained diagnostic text is bounded to 64 KiB per command, prioritizing the first failure diagnostic and the final output and marking any omission.
- **d9** — When a command format has no recognizable failure marker, retain bounded leading and trailing output and state that no failure detail was identified.
- **d10** — The interface comprises existing continuation context, retained check evidence and the existing Spike report, with no new owner command or approval surface.
- **d11** — The final report uses factory check evidence for check status and labels conflicting earlier agent statements as pre-check claims.
- **d12** — Legacy records remain readable with missing details explicitly unknown and no fabricated reconstruction.
- **d13** — Acceptance covers authorized historical writes read by a read-only successor, forbidden new write claims, missing or malformed provenance, bounded failure capture and failed, interrupted or absent check records.
- **d14** — Elapsed time is reported without changing timeouts, scheduling, live progress or suite performance.
- **d15** — The preserved discovery change must neither be discarded nor treated as accepted on the basis of this shaping result.


## Assumptions

- **a1** — Interpret 'wedged it and destroyed it' as a request to trace the failed Spike workflow and check preservation of its work, based on the supplied two Spike reports and owner follow-up.
- **a2** — Repository claims refer to HEAD 2c10f40baae2b466dffab342b2b047f6952b9131. Historical daemon records identify running version 16e1140, the parent of the preserved discovery fix.
- **a3** — Open: the specific full-suite failure cannot be recovered from the retained 2 KB check tail inspected here. Do not assume it was caused by the discovery patch or dismiss it as a flaky test. No new checks were run.
- **a4** — Open: the original bot-token reset drift remains an issue-reported inference rather than a verified host observation. The patch's treatment of differing future resets still needs engineering review; passing the daemon package alone is not acceptance.
- **a5** — The 64 KiB per-command diagnostic bound is a shaping proposal to retain useful failure evidence without unbounded output; redaction applies before retention and publication.
- **a6** — The existing retained evidence and Spike report are the intended interfaces because the requested repair concerns continuation and reporting, not a new user workflow.


## Open questions

### q1 — Should this scope repair the Spike workflow only, or also finish the discovery fix?

_Asked of: Product Owner Questions_

- **factory** — Recommended: repair continuation and failure reporting, preserve the discovery patch as unverified, and leave its validation and any further discovery changes to a separate scope; this follows the latest investigation.  *(recommended)*
- **combined** — Include both the factory repairs and engineering validation and completion of the discovery fix; success must then prove rollover recovery as well as safe continuation and reliable reporting.
- **discovery** — Focus only on completing the discovery fix; keep the confirmed factory defects documented for separate work.
