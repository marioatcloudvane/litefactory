# Approved validation follow-ups

For separated workflow, the factory derives an immutable proposal from each
retained unmet/unverified acceptance or review finding. The proposal contains its
complete title and body, source story URL, criterion/review key, assessment run,
approved revision and snapshot. These are proposed separate work, not waivers.
Reports refuse truncation. At most 16 pending proposals are delivered per poll;
remaining proposals remain durable debt. No agent or mode request creates issues.

The numeric owner approves the displayed concrete proposal:

```
/followup 1 <64-character proposal digest> linked
```

`linked` creates a standalone follow-up with source links. To explicitly attach
it as a child instead, use `attach`. The same `attach` control may also be used
later for an already-created linked issue. Its separate comment is retained.
Every attached child participates in normal parent readiness until closed.
Approvals cannot remove a previously approved attachment.

Only the authenticated numeric owner can approve. Generation and proposal
identity must match; changed content is a new proposal requiring new approval.
Manual boxes, labels, closure and free prose are never approvals. Source
exceptions require their own `/waive` control and reason. Follow-up creation
never changes acceptance, executes checks, approves merging or switches modes.

The factory records approval and create intent before network calls. It verifies
numeric issue and creator identity, exact content and marker before retaining a
created issue; attachment is independently confirmed. Lost responses reconcile
against the marker and verified factory creator before any retry. If no issue
can be confidently found after an attempted create, the operation stays pending
with a diagnostic rather than blindly creating another issue. Proposal, pending
and completion comments each have independently deduplicated factory markers.
