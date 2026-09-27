# Bug issue closure

`lf_bug_close` consumes `ConfirmedBugIntegration` and the original issue identity.
It adds no owner gate or remote target-tip check. Both exact approvals and a
confirmed integration without an exception remain prerequisites at dispatch and
at confirmation.

The closure intent precedes issue observation. Reads must match the configured
forge API endpoint, repository, numeric issue ID and issue number. Invalid or
missing observations cannot authorize a close. The existing forge close method
performs its own numeric identity check and requests closed/completed. An already
closed issue receives no write or reason change.

A separate issue read must confirm closed state after any request, including a
lost response. Request success alone is insufficient. Transport uncertainty stays
pending; an open issue alone does not authorize another request. A definitive
identified refusal and subsequent open observation may admit an explicit factory
continuation through `PrepareBugClosureRetry`, with current integration guards.
No automatic mutation retry loop exists.

Historical closure confirmation survives a later observed reopen, but that
observation creates a conflict pending operator handling and prevents lifecycle
completion. The workflow does not reclose the issue automatically. It cannot
exclude concurrent external edits or detect unobserved reopen/close cycles.

`ConfirmedBugClosure` returns the retained independent observation and complete
integration chain. It does not publish a lifecycle report or consume the parent
continuation. Report failure cannot erase the fact that closure was observed.
