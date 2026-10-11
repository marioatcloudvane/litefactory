# Story 005 implementation, run 0005

Resolved the chat inbox merge conflict by retaining the diagnosis target together with the incoming arguments, project-question state, resolved repository and retry fields. Updated the notice projection that shares the inbox scanner. No conflict markers remain in the working file; the factory owns staging and the merge commit.

Completed the typed diagnosis, question and answer commands and daemon wiring. Diagnosis uses the single doctor slot, reads only, excludes repair steps, retries busy work on heartbeats and allows one restart retry. Answers require the current published Research question and token, freshly check issue state under the task lock, and use the shared atomic owner-reply claim and parked dispatch. The private reply band does not move the comment watermark. Redacted issue notes use the existing delivery journal and recover uncertain sends without reposting them.

The signed fake Discord journey covers diagnosis, busy capacity, question display, stale and closed-issue refusals, redaction, repeated delivery, one issue note, continuation allocation, unchanged watermark and diagnosis restart with channel fallback. A real workflow fixture verifies the parked chat answer reaches the resumed Research run. Store fixtures cover the GitHub/chat race, atomic refusals, migration defaults and the restart allowance. Handler fixtures cover resolution, volatile inputs, storage errors, uncertain restart acknowledgements, and reply limits. A publication-window test withholds the token until the journal exists.

## Checks actually executed

- Complete normal suites: storage, workflow, daemon and command packages. The first combined run failed on the incoming notice projection (16 columns for the 17-field scanner); the corrected full storage suite passed. Workflow passed in the original complete run. The complete daemon and command suites subsequently passed; the complete daemon suite passed again after the publication-window guard.
- Complete race suites: doctor, messaging, project-question service, Discord, meter view, repositories, result service, rights, storage test helpers, team release and webhook registration. The first run exposed the same notice mismatch in messaging and Discord. Their corrected complete race suites passed, including later reply-length and uncertain-restart cases.
- Complete race suites for reporting, its result-handler importer, and the executable command package passed.
- Repository-pinned scoped lint passed with zero findings. Changed Go files were formatted, and the working diff has no whitespace errors.
- Full Go build passed. No whole test suite or repository full check was run.

The logs retain the initial failures and the successful reruns; they are engineering evidence, not acceptance approval.

## Assumptions and remaining human check

The original research migration is unreleased on this story branch. It was moved after the incoming inbox rebuild migrations, and preserves their pending question data. Legacy waits still default to forge.

Raw answers and optional diagnosis notes are volatile; they are not stored in generic inbox arguments. After restart, missing answer input is never guessed or reapplied. If the earlier outcome cannot be proved from the current wait, the reply says it could not be confirmed. Long observations and questions are explicitly shortened for Discord, preserving each diagnosis section and the complete revision token.

The real Discord walkthrough is documented but was not performed. Acceptance remains with Validate. Built alone because the current session prohibits delegation. No staging, commit, push, merge command or live Discord/GitHub publication was performed.
