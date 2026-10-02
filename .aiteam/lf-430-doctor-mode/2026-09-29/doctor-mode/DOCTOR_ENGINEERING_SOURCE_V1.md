# Doctor engineering source reconciliation v1

Story: story-001 / #432, frozen revision b70a2450e6b2ffc758447e6f191dfadc5e7e5308acea4fdfdac29b1b5d3d7c2d.
Guide: doctor-field-guide-v1; prompt: doctor-diagnosis-v1.

The owner identified #406 as a stuck case and explicitly instructed this run to continue implementation while recording missing acceptance evidence. No authentic #406 capture, daemon log extraction, live database mutation, paid provider invocation, or daemon restart was performed. No claim is made that a historical incident remains an unfixed defect. Authentic case reconciliation (AC-011) and provider isolation observations (AC-005/007) remain unverified.

Source-backed statements used by the guide:

- `internal/store/sessions.go`, `runs.go`, `segments.go`: a workflow session, its runs and provider selections are distinct from a doctor conversation and its attempts.
- `internal/store/watermarks.go`, `internal/daemon/tick.go`: normal ordered consumption owns its watermark. Doctor collection does not advance it.
- `internal/store/report_deliveries.go`, migration0033, migration0034: pending, possibly-sent and confirmed delivery are distinct; a local absence does not prove a remote post never happened.
- `internal/store/graph*` migrations0048/0051/0053: graph attempts, waits and dispatches are independent persisted facts; a failed run alone cannot explain all pending work.
- `internal/daemon/lock.go`: the singleton is a kernel-held file lock, not a PID file. Its observed state is not proof that a workflow is healthy.
- `internal/config/tier.go`: current provider flagship mappings are resolved independently of ordinary workflow settings.

Implemented isolation posture: production providers refuse diagnosis until authentic versioned isolation is available. The mode-free provider contract, private broker and controlled-provider test seam exist; the actual Claude and Codex adapter methods explicitly refuse and do not fall back to workflow Run. There is currently no supported production provider launch. A passing simulated provider test does not establish host isolation or enablement.

Evidence collection is bounded and issue scoped. Database projections omit raw parent aggregate blobs; comment bodies exceeding 8192 bytes are omitted, bounded excerpts are redacted, and total disclosed evidence has a 96 KiB limit. Local logs require canonical task attribution, use at most a 1 MiB tail and retain at most 200 matching lines. Worktree git observation performs no fetch or optional index locking. Session inventory reads metadata under the recorded issue branch directory, not file contents; unsupported paths and file repair targets remain unavailable. Optional remote issue/comment reads are not wired and appear as coverage gaps.

Remaining engineering limitations are not passes: production provider execution remains unsupported; native provider usage collection is unknown; full session-file content/digest repair targets are not enabled; a full isolated CLI subprocess matrix and authentic #406/provider observations are not claimed. The implementation has no GitHub command adapter or repair effect path in this story.

Lifecycle hardening includes recovered accepted results before a new turn, immutable retained input/evidence, active-generation result fencing, doctor-only orphan metering closure, in-process cancellation on stop, strict duplicate-key result decoding and preflight revision diagnostics. Shared result submission mechanics now serve both doctor and the existing workflow façade; workflow authority checks remain in their existing owner. A controlled provider test exercises private scratch, flagged model recording, shared meter ownership, durable preflight/submit/receipt/adoption and duplicate-source refusal. This test is simulated and does not enable native providers.
