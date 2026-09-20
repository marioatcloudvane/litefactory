# Implement notes (run 0011, #313)

## Assumptions
- Store accessors live on `*CreationStore`. Refusals: different proposal in a presented run, rebinding a different key, and marking presented without a key return `ErrCreationConflict`; wrong task / missing run / empty bind key return `ErrCreationAuthority`. Replacing an unpresented proposal clears the delivery key.
- Added `CreationStore.RetainedCreationProposal(ctx, hash)` (no getter existed) so the reporter reads the envelope from the retained row.
- Handler errors reach the agent as the broker's fixed sentence; the 48000-byte limit and split advice are also in the tool description and guidance.
- Projection registered under both keys whenever the authorizer is present (mirrors the batch projection).
- Hook: the daemon keeps the session backlog flag while `creation:` recovery debt exists and resumes it in `work` before draining, so a legacy run whose trigger is consumed still retries. Comments of that session wait until the presentation posts (intentional hold).
- Crash gap not implemented: a daemon death after retention but before the first attempt records debt is not retried (no per-session latest-run lookup in the store). No AC requires it.
- AC-020 models a forge outage as a failed POST followed by the comment appearing on the forge (the delivery journal never re-POSTs after a failure).
- AC-026 daemon test covers admission within/beyond 30 minutes, superseded hash and no run launched; the next-attempt grant/batch part is covered by existing turn tests, not re-run in the daemon.

## Unverified obligations
- Live Claude/Codex host invocation of `issues_create_prepare` was not observed (AC-039).
- A real GitHub outage and its retry path were not observed; only the offline fake forge.
