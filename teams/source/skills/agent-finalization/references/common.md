# Shared finalization

Application schema is integer 1; unknown versions reject. Result revision starts at
1 and increments per accepted update. Submission contract version 2 and workflow
version 1/2 are independent. The factory supplies meta and trusted mode/scope;
create input is only {"result": ...}. Never author meta, owner answers, approvals,
acceptance, publication, capabilities or execution telemetry.

Require outcome completed/incomplete/blocked and nonblank comment. Completed omits
reason; incomplete and blocked require a meaningful reason and only actual work.
Keep exactly the selected mode under payload. Omit unused optional fields; null
rejects. Recommendation is {text, action?}; the only optional action is
"action.moveto.planmode" and remains a proposal. Artifacts require stable id, kind
and rooted relative path; a path alone cannot prove immutable evidence.

Questions require current factory permission, Research or Plan, and incomplete or
blocked outcome. Use stable question/option IDs and a valid recommended_option if
supplied. Never invent owner answers. Without question permission, explain the
material missing input in reason and stop within the selected mode.

For continuation, obtain only authorized fields/items plus current full-result
revision. Projections are not complete snapshots. Update contains expected_revision
and ordered operations: set, remove, add_item, update_item, remove_item, reorder.
Targets use allowlisted field names, stable item id and parent story for nested
criteria/tasks; no numeric indexes or JSON Pointer. set recursively merges supplied
objects and replaces supplied arrays; omission preserves. remove applies only to
existing optional editable fields. add_item appends an unused identity; update_item
merges without changing identity; remove_item retires the identity; reorder lists
all current IDs exactly once. Retired keys cannot be reused, including by replacing
arrays. Frozen requirements and retained evidence/assessments are never patchable.
The complete final merge must validate. Limits: 4 MiB input, depth 32, 256 operations.

Preflight the candidate if available; it performs evidence reads but records no
request, proof or revision. Correct discoverable diagnostics and preflight again;
submit revalidates. On stale revision, read authorized current targets and rebase
intent before a fresh submission. Keep exact request bytes and returned correlation
for lost replies: receipt lookup or exact replay returns the original application.
Changed bytes require fresh correlation. Do not repeat execution to replace a lost
reply. Input received/accepted, engineering, acceptance and publication/remote effects
are separate receipt observations; pending is not completed acceptance or publication.
Current adapters may have different transport envelopes; do not invent a new route.
