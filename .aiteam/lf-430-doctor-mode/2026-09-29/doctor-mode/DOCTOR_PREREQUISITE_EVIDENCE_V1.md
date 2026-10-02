# Doctor prerequisite evidence — version 1

Recorded: 2026-09-29T17:00:47.441751+00:00
Source checkout HEAD: `a330625f6ae45b6d2b8a261132d9a7b290c7b450`.
Selected requirement: story-001 / #432 / issue ID 5631757897, frozen revision `b70a2450e6b2ffc758447e6f191dfadc5e7e5308acea4fdfdac29b1b5d3d7c2d`, T-001 / AC-011. No story dependencies.

## Verdict

**Blocked prerequisite; no engineering completion claimed.** The current session supplies a historical narrative of authentic inspection, but not a retained authentic redacted issue-scoped capture with sufficient provenance for the required before-implementation reconciliation. The separate reviewed field-guide resource is also absent; the companion v0.1 draft records only supported source statements and is not a completed review. T-002 depends on T-001. Dependent engineering must not claim this prerequisite satisfied.

## What was inspected

The supplied plan brief, current-session artifact inventory, RESEARCH_BRIEF.md, RESEARCH_TECHNICAL.md and PUBLICATION_FAILURE_DIAGNOSIS.md; current source for the Plan renderer, redaction budget, publication truncation refusal, normal comment drain and flagship resolution. No archived issue sessions, forge APIs, live database/logs or provider processes were inspected. No runtime edits, restart, implementation or tests occurred. Tests: 0 executed, 0 passes claimed, 0 failures claimed.

The retained publication diagnosis reports live database effects 162 and 163, requests plan-request-142-1343 and plan-request-143-1352, failing in render with `complete draft cannot be truncated`; it reports daemon log times 17:30:26 and 17:46:33 on 29 September. These are reported historical observations, not observations made in this run. The report supplies neither raw scoped rows/log excerpts nor an explicit capture timestamp/timezone and daemon binary/source version. Its claim of zero secret redactions does not establish capture redaction provenance. The current run's source revision and recording time cannot be substituted for the missing historical provenance.

## Source reconciliation and corrections

- Supported: renderer reserves 512 bytes; Redactor.Budget reserves 2,048 bytes; MaxStoryBodyBytes is 65,536; publication rejects truncation before remote mutation. References: internal/report/plan_draft.go:13, internal/redact/truncate.go:19, internal/redact/redact.go:222, internal/planbrief/proposal.go:25, internal/turn/plan_story_activity.go:288. This supports the explanation's mechanism, not independent verification of the historical row/log claims or current incident state.
- Supported: ordinary drain stops after the first unconsumed comment, preserving ordered processing. Reference: internal/daemon/tick.go:1471. Historical #314/#366 summaries remain historical; this record makes no claim that those defects are currently unfixed.
- Supported: per-provider flagship resolution returns absence rather than proving provider availability. Reference: internal/config/tier.go:132. This does not prove actual model selection or isolation.
- Correction to evidence status: retain the newer #430 publication narrative alongside earlier research limitations; do not simply claim nobody ever inspected live state. The supplied current session contains a report that someone did, but lacks its inspectable capture.

## Missing prerequisite and safe continuation

Supply one authentic redacted capture of a specifically identified stuck issue, with exact issue/source identity, capture time including timezone, runtime binary/source version, relevant bounded row/log observations and redaction/coverage limitations. Reconcile and version the actual separate guide against that capture and current source before production implementation. A capture of the previously reported #430 publication failure would be suitable only if its provenance and observations are retained; selecting a presently running issue does not manufacture a stuck case. No owner exception is supplied. No fresh broad runtime scan is justified by this missing artifact.

Provider isolation is a separate unverified prerequisite: no authentic doctor-specific observation is retained in the current session with provider binary version, configured and observed model, exact permitted tools, working directory/environment and forbidden-operation probes. General source or synthetic tests cannot certify it. Keep adapters disabled until observations exist; no paid/nested provider was launched.

## Artifact integrity

- `PUBLICATION_FAILURE_DIAGNOSIS.md` SHA-256 `e7eb5a3aab54c8b5efd55bbd768093f4b2cb2b03cf34d7a50b02a788c01a53ed`
- `RESEARCH_BRIEF.md` SHA-256 `8f120db6c170a48c4483893cee087b0b28eb601ea4956382823650329c354893`
- `RESEARCH_TECHNICAL.md` SHA-256 `5ff3909576322f1324e8d01551852d00e286863c8d0de892d316d4b47f12b9e9`
- `DOCTOR_FIELD_GUIDE_PREREQUISITE_V0_1.md` SHA-256 `b6cf644a6872b6518df400e9b5753e50ae51270d5c4e9d87d3e64317ea9ca58d`

This is engineering prerequisite evidence, not a Validate acceptance/code/architecture assessment. The orchestrator owns checkpoint submission, shared result, manifest and other completion artifacts.
