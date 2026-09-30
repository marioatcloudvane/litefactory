# Research brief

## Request

Investigate what went wrong in issue #435's Spike, distinguish the long check run from the subsequent continuation failure, and establish whether the discovery fix survived.

## Decisions

- **d1** — This pass is Research into the Spike incident. No implementation, daemon restart, commit, push or change of mode was performed.


## Assumptions

- **a1** — Interpret 'wedged it and destroyed it' as a request to trace the failed Spike workflow and check preservation of its work, based on the supplied two Spike reports and owner follow-up.
- **a2** — Repository claims refer to HEAD 2c10f40baae2b466dffab342b2b047f6952b9131. Historical daemon records identify running version 16e1140, the parent of the preserved discovery fix.
- **a3** — Open: the specific full-suite failure cannot be recovered from the retained 2 KB check tail inspected here. Do not assume it was caused by the discovery patch or dismiss it as a flaky test. No new checks were run.
- **a4** — Open: the original bot-token reset drift remains an issue-reported inference rather than a verified host observation. The patch's treatment of differing future resets still needs engineering review; passing the daemon package alone is not acceptance.
