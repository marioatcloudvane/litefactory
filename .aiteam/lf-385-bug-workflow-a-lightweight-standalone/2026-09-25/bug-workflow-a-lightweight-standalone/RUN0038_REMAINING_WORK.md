# Run 0038: what remains for stories 008 and 009

Diagnostic only, based on supplied factory state and saved session reports. No production inspection, implementation, tests, checkpoint submissions, acceptance review or forge actions were performed.

## Current authoritative state

Stories 001–007 are engineering-complete read-only dependencies. Story 008 (#397), revision c594f6821abb1984c42257eccaa6f3e8fa61ead85514cb952a395c761964a3da, and story 009 (#398), revision 73ce8275d3e7c25e58be8a37601ff5b257f6adf36b4a99baa368cf5b4630ae8a, are blocked with no retained snapshot or checks in the supplied factory evidence. Story 008 depends on 007; 009 depends on 001–008.

## Work already reported

RUN0037_ENGINEERING.md reports production implementations for stories 003–009, including runtime/daemon integration, separate child execution, delivery/closure and recovery reporting. notes.md explicitly reports story 008 activity and controlled SQLite/forge tests delivered. STORY_009_ENGINEERING.md describes the parent package, outcome routing, shared repair step, distinct waits, continuation lineage, durable reporting and lifecycle completion guards; it records passing scoped race and package/runtime tests. These reports are engineering handoffs, not a fresh verification of code or proof that every frozen criterion is complete.

The actual composed fixture used SQLite, disposable repositories and controlled runner/forge seams. Its child stopped when isolation was unavailable; separate controlled domain tests cover merge/closure. No authentic provider permission proof or live delivery is established. Existing reports record owner-authorized story-002 test and unavailable-host-probe exclusions; exclusions must remain visible and cannot be described as passed.

## Submission and aggregate evidence gaps

RUN0037_STORY008_RECEIPT.json records request f820194b4bbc0b07ae0647bdc10b8806 as input rejected and engineering rejected. Its diagnostic says the snapshot did not match current stable worktree bytes and no evidence was retained. notes.md attributes this to concurrent integration edits after preflight. It is not an uncertain accepted completion.

The supplied last run-0037 comment separately describes a later story-008 submission as processing-incomplete. That later request ID is not retained in the inspected session reports/receipt index. Recover that original request through the factory before retrying; do not mistake the initial rejected request above for it or invent a replacement to bypass uncertainty.

The last run-0037 comment says both aggregate gates passed, but RUN0037_ENGINEERING.md still says they are in progress. MAKE_CHECK_FINAL.log and MAKE_TEST_FINAL.log contain older failed aggregate runs matching the run-0035 report, not final run-0037 passing evidence. The inspected artifacts therefore do not provide final passing aggregate output bound to final bytes. This is an evidence gap, not a finding that run-0037 tests failed.

The supplied factory diagnostic for run 0037 also identifies a separate final-result failure: a positive revision and 1–256 nonempty operations were required. Checkpoint recovery alone cannot fix missing final application output.

## Necessary finish sequence

1. Recover the later uncertain story-008 receipt using its original request identity. If definitively rejected, correct its diagnosed problem and use a new request; if pending, retain pending status and recover through the factory without duplicating it.
2. Establish story-008 scoped checks against stable final files and capture the exact immutable bytes. Reuse valid retained proof where available; otherwise execute the required checks with the recorded authorized exclusions. Preflight, submit the engineering checkpoint, and read its receipt until its factual state is known.
3. Once 008 engineering is recorded, establish and submit 009 evidence for the existing parent composition and required journeys/recovery paths against the same stable final implementation. Complete any engineering/test gaps actually found; saved reports alone identify no specific remaining product-code defect or missing feature requiring redesign.
4. Recover final run-0037 passing aggregate evidence if it can be tied to unchanged final bytes; otherwise execute make check and race-enabled make test with exclusions stated precisely. Submit a separate aggregate capture and reference its retained snapshot/check IDs.
5. Submit the complete shared application update using the current authorized revision and nonempty typed operations, replacing obsolete blocker prose and referencing 008/009 and aggregate evidence. Preflight and read the matching receipt. Keep engineering, acceptance and publication states separate.

No new scope approval is indicated. Evidence capture/submission and verification are engineering/factory work. An operator is needed only if the factory cannot recover the unresolved request or if actual supported-host proof is required beyond the recorded exclusions. Unsupported production isolation must continue to block runtime execution. Separate Validate remains responsible for acceptance; this diagnosis does not close either story.
