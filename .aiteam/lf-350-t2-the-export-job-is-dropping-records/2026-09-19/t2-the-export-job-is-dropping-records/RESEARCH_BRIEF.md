# Research brief

## Request

Investigate issue #350: records reportedly go missing intermittently from an export job after an unspecified batch; explain what is actually happening.

## Decisions

- **d1** — The agreed deliverable is a diagnostic evidence checklist with an explicitly unverified diagnosis, as selected by the owner in q1.
- **d2** — As the export owner, I need a clear account of what is known and what evidence would distinguish expected exclusion, collection delay, incomplete delivery and actual record loss.
- **d3** — The current scope preserves existing findings and missing evidence without requiring job identification or a run reconciliation to complete this owner-selected deliverable.
- **d4** — The diagnostic record contains incident-level job, command/configuration, environment, deployed revision and batch references; run-level time/timezone, snapshot capture, filters, completion status and evidence references; and discrepancy-level record unit, expected identity/count, actual identity/count, eligibility basis and explanation, with absent evidence recorded as unknown.
- **d5** — The interface is the Research report and its brief and technical artifacts, using only authorized read access to supplied source, configuration and sanitized run evidence.
- **d6** — Any future discrepancy comparison must use the same project, filters, record unit, time boundaries and snapshot source state, with counts alone insufficient to identify lost records.
- **d7** — Product fixes, eligibility or format changes, new interfaces or storage schemas, monitoring, retries, recovery, production reruns, backfills and unrelated exporter audits are outside this scope.
- **d8** — Shaping is complete once the owner-selected checklist, snapshot expectation and uncertainty are recorded, while the incident remains unverified until observed evidence explains an affected-run discrepancy.
- **d9** — The owner settled q2 by requiring every eligible record present at snapshot capture in a successful export, with later arrivals belonging to the next export.
- **d10** — The retained evidence checklist is the actual job command/configuration, deployed revision, meaning of the last batch, one affected run's time/timezone and snapshot evidence, filters, exit status/logs, and an expected record ID or count compared with output at the same record unit.
- **d11** — Validation must distinguish empty selections, duplicate identities, multiple rows per source entity, blank values or coverage rows where applicable, late arrivals, cancellation, preparation limits, writer failure and consumer truncation.
- **d12** — No production data or configuration changes or independent publication are authorized, and the factory owns publication of the report.
- **d13** — A regression claim requires evidence connecting the identified batch and deployed version to the discrepancy, rather than timing alone.
- **d14** — The possible explanations retained from research are filter exclusion, post-snapshot arrival, row-meaning confusion and partial failed output, with no explanation ranked or confirmed for this incident.


## Assumptions

- **a1** — Meter export remains only a provisional repository candidate because no supplied evidence identifies it as the affected job.
- **a2** — The unknown job's documented filters and row meaning define eligibility and record granularity under the owner's settled snapshot rule; meter-specific row meaning is not assumed.
- **a3** — Run reconciliation cannot be performed from the supplied evidence because the job, snapshot source state and affected output are unavailable.
- **a4** — The owner's reply authorizes completing the checklist deliverable without further questions and does not authorize a product fix.
