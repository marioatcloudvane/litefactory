# Implementation notes

The complete frozen revisions are requirements. Older parent notes differ on migration and config retirement; the frozen stories govern. No acceptance or review is commissioned. Existing session reused. Session convention read from .claude/modes/session.md because docs/workflow/session.md is absent; docs/story-contract-instruction-manifest.md is also absent.

Repository command surface has add and list, no detail command. Tests cover current add/list surface. SQLite failure is tested through Migrate, because Open deliberately does not migrate. No external test service is needed for temporary SQLite fixtures.

Factory snapshot validator checks parent directory existence even for deleted files. Empty parent directories were recreated only for capture validation, with all retired files still absent; git records no empty directories. These must stay until post-exit revalidation. No production validator was changed.

The factory recorded story-002 blocked rather than engineering_complete after the aggregate test failure. Aggregate capture truthfully records passed:false and unfinished suite execution. Sandbox denied signalling the task-owned test process group, so no stop was made and the failure remains the original cause.

Application preflight refuses failed check IDs in aggregate_checks, even for incomplete results. Failed aggregate execution remains retained under capture request d76aa0e596946bc27e368be66cce30dd and session reports; final incomplete result omits a successful aggregate association rather than disguising failure.

## Run 0007 continuation

Replaced deleted distribution instruction inputs in the exchange-cost corpus with four small local static protocol fixture texts; regenerated hashes and byte totals without changing its result-equivalence and stale-retry assertions. These are synthetic offline test inputs, not authentic runtime observations. No shipped definitions changed. Final aggregate proof and submission remain pending.

## Aggregate log provenance correction

Concurrent retained executions wrote to the shared make-check.log; NUL gaps and multiple timings make attribution unsafe. Preserved its observed bytes in make-check-mixed-preserved-20261005T193718Z.log. No success or failure from that mixed log is attributed to the current execution. A fresh uniquely named final aggregate runner writes make-check-run1055-final.log and make-check-run1055-final.exit with exclusive creation; it has not been executed yet. Wait for the existing task-owned execution to finish before running it.

## Run 0007 engineering completion

Run 0007 engineering complete: uniquely logged final make check exits 0 (format, vet, pinned lint and full race suite). Story-002 snapshot lf489-story002-run1055 retains five executed scoped check references; aggregate snapshot lf489-aggregate-run1055/check aggregate-check-run1055 is prepared for factory submission. Net Go reduction is 14240 lines (565 added, 14805 removed). Acceptance and reviews not performed; no commit/push or issue closure. Capture/submission receipts remain separate and are pending root handoff. Mixed earlier log is preserved without current-run outcome attribution.

## Run 0007 submission receipts
Story checkpoint dc125fb22ffac694728ff007ef2fcad6: valid input, engineering recorded, acceptance not applicable, remote effect pending. Aggregate capture 0679a1c267581f93fe9c77e08a6891f3: input accepted; engineering/acceptance/publication pending. Final application bde14cbbf9ff0cc60fc5bde543eaa409: preflight valid and complete; input accepted at result revision 2; engineering/acceptance/publication pending. Matching receipts observed. No non-session files changed after final submission.
