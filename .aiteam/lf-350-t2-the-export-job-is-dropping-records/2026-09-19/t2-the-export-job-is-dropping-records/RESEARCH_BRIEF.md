# Research brief

## Request

Investigate issue #350: records reportedly go missing intermittently from an export job after an unspecified batch; explain what is actually happening.

## Decisions

- **d1** — This is a diagnostic research request. No implementation or verified root-cause claim is supported by the available evidence.
- **d2** — Goal and primary user story: as the export owner, understand why an identified run contains fewer records than expected and distinguish exclusion, collection delay, incomplete delivery and actual record loss. The deliverable is an evidence-backed explanation, with any unresolved cause explicitly unverified.
- **d3** — Proposed scope: trace the identified job from eligible source records through selection, snapshot/output and any supplied wrapper or consumer. Compare one affected run against an explicit expected record set or count at the same granularity and cutoff. Examine the alleged batch relationship only once the deployed revision and batch are identified. This is a shaping decision, not a finding that any stage is faulty.
- **d4** — Diagnostic data model: one incident names the job, command/configuration reference, environment/deployed revision and reported batch; each run records timestamp/timezone, filters/cutoff, completion status and evidence references; each discrepancy records expected identity or count, actual presence or count, record unit, eligibility basis and supported explanation. Unknown fields stay unknown; counts alone do not establish which records disappeared. No persistent application schema change is proposed.
- **d5** — Interface and permissions: use the existing job interface and authorized read access to supplied code, configuration and sanitized run evidence. Return the explanation, reconciliation and uncertainty in the Research report and its brief/technical artifacts. No new CLI, API or UI is proposed. Do not change production data/configuration, rerun production jobs, backfill records or publish independently of the factory.
- **d6** — Validation and edge cases: use matching project, filters, record unit, time boundaries and source state for comparisons. Distinguish empty results, coverage rows with missing numeric values, multiple measurements per attempt, duplicate identities, records arriving after capture, cancellation, preparation limits, writer failures and incomplete consumer output. Treat historical meter behavior as conditional on identifying that exporter and checking the deployed revision. A cause claim must explain a concrete discrepancy with observed evidence; repository possibilities or synthetic examples alone cannot verify this incident.
- **d7** — Explicitly out of scope: implementing a fix, changing export eligibility or CSV format, building a replacement exporter, adding monitoring/retry/recovery features, changing storage schemas, restoring/backfilling records, unrelated exporter audits and asserting a regression from timing alone. A later fix requires a supported cause and a separately settled change scope.
- **d8** — Completion rule: the shape remains incomplete until q1 and q2 are resolved or the owner explicitly directs proceeding with assumptions. Diagnosis is verified only when an affected-run discrepancy is explained by evidence. If evidence remains unavailable, an owner-selected checklist-only deliverable must state that the incident is unverified.


## Assumptions

- **a1** — Use meter export only as a provisional repository candidate because it is an explicit record export found in current source. The affected job remains unidentified; findings about this command may not apply.
- **a2** — Provisional expected behavior is that a successful export includes every eligible record from its defined snapshot. The owner's eligibility rules and expected record granularity remain open; q2 proposes this expectation for agreement.
- **a3** — Incident diagnosis still requires the actual job command/configuration, deployed revision and meaning of 'last batch', an affected run time with filters and exit status/logs, and at least one expected record ID or count comparison. No regression or source-data loss can be established without that connection.
- **a4** — The owner's request to continue shaping retains the original diagnostic objective; it does not authorize implementing a fix or treating the provisional exporter as confirmed.


## Open questions

### q2 — What should count as a missing record? I recommend all eligible records present when the export snapshot is captured, using the job's documented filters and row meaning. For meter export, that means measurement rows plus applicable coverage rows, not one row per issue. Is that the intended expectation?

_Asked of: Product Owner Questions_

- **snapshot** — Recommended: include every eligible record present at snapshot capture; later arrivals belong to a later export.  *(recommended)*
- **cutoff** — Include all records through a business cutoff, including late arrivals; specify the cutoff, timezone and record unit.
- **other** — Use another expectation; specify the record unit and selection rule, with an example ID or expected count if available.

### q1 — Which export should this diagnosis cover? I recommend identifying the actual command or job configuration before treating meter export as the target. Include the deployed revision and what 'last batch' refers to. If available, also supply one affected run's time, filters, exit status/logs and an expected ID or count; missing output need not block identifying the job.

_Asked of: Technical Questions_

- **identify** — Recommended: identify the actual job and provide its command/configuration or an accessible source reference; include available run evidence.  *(recommended)*
- **meter** — Confirm that litefactory meter export is the affected command; provide its arguments and available run evidence.
- **unidentified** — The job cannot yet be identified; retain an incident evidence checklist and an unverified diagnosis.
