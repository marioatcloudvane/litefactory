# Usage and costs

Use the local meter after an initialized installation has recorded attempts. It separates observed token quantities, collection coverage and estimates at locally configured prices.

```sh
litefactory meter github:OWNER/REPO 123 --details
litefactory meter github:OWNER/REPO 123 --json
litefactory meter prices list
```

Replace placeholders with a registered repository and its issue number. Summary scope can be global, repository or issue; repository selectors accept a registered path, `OWNER/REPO` or `github:OWNER/REPO`. Filters select mode, coding agent or actual model. Details show attempts, invocation ancestry, actual models, diagnostics and price revisions. The [CLI reference](../cli/command-reference.md) owns complete syntax and accepted filter values.

## Interpret results

| Result | Meaning |
| --- | --- |
| Measured zero | A source explicitly reported zero for that quantity |
| Unknown (`null` in JSON; blank numeric CSV cell) | No trustworthy quantity is available; do not substitute zero |
| Observed subtotal | Known measured work only; incomplete collection leaves the total unknown |
| Complete estimate | Complete collected quantities priced at the attempt's recorded local rates |
| Legacy provider-reported cost | A separate reported amount, not a replacement for token-derived attribution |

Retries are separate subprocess attempts. Exclusive root/child invocation and model measurements contribute once. Inclusive summaries, reasoning tokens already included in output, and cache-duration subsets are not extra consumption. Model filters retain unresolved attribution and incomplete coverage. Daemon helper consumption is excluded.

Claude and Codex collectors combine process events with bounded associated transcript/rollout reads. Missing sources, model identities or descendant evidence leave partial/unknown coverage. Source inspection and synthetic tests do not certify authentic full provider hierarchies or invoice accuracy. A provider-reported zero-cost refusal is shown only with affirmative supporting evidence; it does not establish a known model or complete token pricing.

## Prices and export

Prices are operator-defined USD per million tokens, not automatically fetched billing rates. `meter prices set` replaces one agent/model entry in a new immutable catalog revision; omitted optional rates become unknown for that entry. Each attempt pins its catalog before launch, including models discovered later. Later price edits do not reprice historical work. See [CLI reference](../cli/command-reference.md) for rate fields and [meter arithmetic](../meter.md#exact-pricing-arithmetic) for rounding and cache fallback.

```sh
litefactory meter export github:OWNER/REPO --issue 123 > usage.csv
```

Export requires an explicit registered `github:OWNER/REPO`. Filters combine with AND. RFC3339 time bounds select complete attempts by start time: `--time_from` inclusive, `--time_to` exclusive. Descendants finishing outside that interval remain included. No matches yields a header only. CSV contains measurement rows and non-additive coverage rows; do not sum coverage metadata. Unknown amounts remain blank. The [CSV reference](../meter.md#project-csv-export) explains columns, spreadsheet escaping and limits (1,000,000 rows, 256 MiB encoded CSV, 30 seconds preparation). Failed/cancelled output may leave a partial redirected file; check the command's exit status before using it.

## Recover historical usage

Historical recovery repairs eligible missing Codex descendant observations, not interrupted execution. Inspect `--details` for a finalized attempt ID, then preview:

```sh
litefactory meter recover github:OWNER/REPO 123 --attempt ATTEMPT_ID
```

After inspecting `unresolved`, `additional_observed_usd` and `resulting_observed_subtotal_usd`, repeat with `--apply` only to request the repair. `applied: false` means preview or unresolved, not completed repair. The command requires matching repository/issue/attempt, supported retained root identity and exclusive response evidence; it does not migrate the database or launch a provider. Apply revalidates sources and appends missing observations atomically at original prices. Repetition after success adds nothing. Active attempts, source changes, ambiguous ancestry and unsupported evidence refuse or remain unresolved. The resulting subtotal remains an observed lower bound, with total unknown.

See [historical recovery details](../meter.md#recover-missing-historical-descendants), [troubleshooting](troubleshooting.md) and [Operations](README.md).
