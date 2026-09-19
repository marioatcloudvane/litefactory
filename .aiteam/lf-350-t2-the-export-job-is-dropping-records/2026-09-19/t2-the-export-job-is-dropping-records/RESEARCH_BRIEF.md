# Research brief

## Request

Investigate issue #350: records reportedly go missing intermittently from an export job after an unspecified batch; explain what is actually happening.

## Decisions

- **d1** — This is a diagnostic research request. No implementation or verified root-cause claim is supported by the available evidence.


## Assumptions

- **a1** — Use meter export only as a provisional repository candidate because it is an explicit record export found in current source. The affected job remains unidentified; findings about this command may not apply.
- **a2** — Provisional expected behavior is that a successful export includes every eligible record from its defined snapshot. The owner's eligibility rules and expected record granularity remain open.
- **a3** — Incident diagnosis still requires the actual job command/configuration, deployed revision and meaning of 'last batch', an affected run time with filters and exit status/logs, and at least one expected record ID or count comparison. No regression or source-data loss can be established without that connection.
