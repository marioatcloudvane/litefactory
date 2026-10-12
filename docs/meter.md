# Usage and costs

Litefactory counts the tokens each agent run uses and shows what they would cost at prices that you set. This page explains the numbers and the CSV export. The exact command syntax is in the [command reference](cli/command-reference.md#usage-and-costs). Back to the [documentation index](README.md).

The numbers come from local records only. They are an estimate at your own price list, not an invoice from Anthropic or OpenAI. Work that the daemon's own helpers do is not counted.

## Quick start

```sh
litefactory meter github:OWNER/REPO 123 --details
litefactory meter github:OWNER/REPO 123 --json
litefactory meter prices list
litefactory meter export github:OWNER/REPO --issue 123 > usage.csv
```

Replace `OWNER/REPO` and `123` with a registered repository and one of its issue numbers. For `meter` (not `meter export`), the repository can also be a registered path or `owner/name`. You can leave out the issue to see the whole repository, or leave out both to see everything. Filters pick a mode (`research`, `plan`, `implement`, `spike`), a coding agent (`claude`, `codex`) or an actual model.

## How to read the numbers

- Every agent process start is one **attempt**. A retry is a new attempt. An attempt can contain several **invocations**: the main agent and the sub-agents it started.
- Each token amount is counted once. Totals that a provider reports for a whole group, and the subsets described below, are not added a second time.
- **Unknown is not zero.** A measured zero means the source said zero. An unknown value (`null` in JSON, an empty cell in CSV) means there is no trustworthy number. Never treat it as zero.
- Retries and resumed runs are separate attempts, so their usage adds up.

| What you see | What it means |
| --- | --- |
| Measured zero | A source reported zero for that amount. |
| Unknown | No trustworthy amount is available. |
| Local estimated USD | All the work was collected and every part has a price. It is priced at the rates saved when the attempt started. |
| Observed lower bound USD, "total unknown" | Some work is missing or has no price. The amount covers only what is known, so the true total is higher. |
| Provider-reported zero-cost refusal | A resumed run that the provider refused. It is shown only when the provider reported zero cost with no sign of any work. It does not give a model or token prices. |

The token amounts are split into separate kinds: uncached input, cache reads, cache writes and output. Two further numbers are subsets, never extra: cache writes are also split into 5-minute and 1-hour writes, and reasoning tokens are already part of the output. Where a model cannot be proven from the source, it is shown as unknown, and `--model` filters keep that work visible instead of hiding it.

The Claude and Codex readers use both the live output of the agent process and its transcript or rollout files. If a source, a model name or a sub-agent record is missing, the coverage shows as partial or unknown. The numbers are not checked against real provider invoices.

Archiving a closed task keeps its usage and cost records, so a project's totals stay the same; the archived work just loses its link to the workflow step. Records removed by archives made before this change are not restored.

## Prices

Prices are USD per million tokens, and you set them yourself; nothing is fetched. Use `meter prices list` to see the current list and `meter prices set` to change one agent and model.

- Each change creates a new, complete price revision. Leaving out an optional rate makes it unknown for that entry.
- A run saves the price list in force when it starts and uses it to the end, even for models first seen during the run. Later price changes never change old work.
- There is no command to import prices or to re-price old work.

### Exact arithmetic

- A rate has at most 18 digits before and 9 digits after the decimal point. More precision is refused.
- Each amount (one measurement and one price kind) is rounded to 12 decimal places, half to even. The rounded amounts are then added exactly. Reports and exports add the saved values and do not round again.
- The 5-minute and 1-hour cache writes use their own rates. When one of them has no rate, it falls back to the general cache-write rate from the same price list. The writes not covered by either use the general rate. If the split is unknown and a general rate exists, all writes are priced once at that rate, never both as a total and as parts. Without a usable rate those writes stay unpriced.
- Reasoning tokens have no price of their own because they are part of output.

## CSV export

`litefactory meter export github:OWNER/REPO` writes CSV to standard output. Use shell redirection to save it; there is no output-file flag.

```sh
litefactory meter export github:acme/api --coding_agent codex --issue 73 \
  --mode implement --time_from 2026-09-01T00:00:00Z \
  --time_to 2026-10-01T00:00:00Z > usage.csv
```

- The project must be written `github:OWNER/REPO` and be registered. There is no fallback to the current folder.
- All filters combine with AND. An issue number is positive and belongs to that repository.
- `--time_from` and `--time_to` need a time zone. They select whole attempts by the time the attempt **started**: `time_from` is included and `time_to` is not. Either may be left out; `time_from` must be before `time_to`. Children of an attempt that finish after the end are still included.
- If nothing matches, you get only the header row. A bad selector fails before any CSV is written.
- The output is UTF-8 with standard CSV quoting. Timestamps are UTC. Numbers are plain decimals that do not depend on locale.
- Rows are sorted by project, issue, run, attempt start, attempt, invocation and model (unknown model first).
- It reads one consistent snapshot. Limits: 1,000,000 rows, 256 MiB of CSV including the header, and 30 seconds to prepare. If a limit is hit, it fails before printing the header, so narrow the filters. If it fails or is cancelled part-way, a redirected file can be incomplete, so check the exit code.
- Prompts, transcripts, raw provider diagnostics and daemon helper usage are never exported.

### Columns

The columns are always in this order:

```text
project,issue,run,attempt_id,attempt_started_at,attempt_finished_at,coding_agent,mode,invocation_id,parent_invocation_id,actual_model,model_provenance,input_uncached_tokens,cache_read_tokens,cache_write_tokens,cache_write_5m_tokens,cache_write_1h_tokens,output_tokens,reasoning_tokens,price_revision_id,estimated_usd,priced_subtotal_usd,pricing_status,collection_status,diagnostic_codes,row_kind,execution_path,run_id,graph_instance_id,graph_attempt_id,graph_node,outcome,agent_version,source_format,source_invocation_id
```

| Column | Meaning |
| --- | --- |
| `project` | The registered repository, as `github:owner/name`. |
| `issue`, `run` | The issue number and the run number inside that issue. |
| `attempt_id` | The attempt this row belongs to. |
| `attempt_started_at`, `attempt_finished_at` | UTC times. The finish time is empty while the attempt is unfinished. |
| `coding_agent`, `mode` | The agent (`claude` or `codex`) and the work mode. |
| `invocation_id`, `parent_invocation_id` | The invocation and its parent. A main (root) invocation has no parent. |
| `actual_model` | The model, if the source proves it. Empty when unknown. |
| `model_provenance` | Where the model name came from. |
| `input_uncached_tokens`, `cache_read_tokens`, `cache_write_tokens`, `output_tokens` | The four separate token amounts. |
| `cache_write_5m_tokens`, `cache_write_1h_tokens` | Parts of `cache_write_tokens`. Do not add them again. |
| `reasoning_tokens` | Part of `output_tokens`. Do not add it again. |
| `price_revision_id` | The price revision saved for this attempt. |
| `estimated_usd` | The full estimate. Empty unless collection is complete and everything is priced. |
| `priced_subtotal_usd` | The saved price of the known work only. |
| `pricing_status` | `priced`, `partial`, `unpriced` or `unknown`. |
| `collection_status` | `complete`, `partial` or `unknown`. Pricing and collection are judged separately. |
| `diagnostic_codes` | Sorted, semicolon-separated codes that explain gaps. They include `provider_reported_zero_cost_refusal` and `filtered_incomplete_invocation` where they apply. |
| `row_kind` | `measurement` (counts) or `coverage` (does not count, see below). |
| `execution_path` | What ran the attempt: `graph` (a workflow step), `legacy`, `doctor`, `chat_answer` or `chat_presentation_<phase>`. |
| `run_id` | The internal run identifier (0 when there is no run). |
| `graph_instance_id`, `graph_attempt_id`, `graph_node` | The workflow instance, workflow attempt and step name. Empty when the attempt was not a workflow step. |
| `outcome` | How the attempt ended. |
| `agent_version` | The version the agent reported, if known. |
| `source_format` | The format of the usage source that was read. |
| `source_invocation_id` | The invocation identity as the provider itself names it. |

A **measurement** row is one invocation and model. A model change gives separate rows, and unknown models stay visible. A **coverage** row marks an attempt or a known invocation that has no measurement. Its token, amount and price-revision cells are empty. Coverage rows and the repeated attempt details are not counters, so do not sum them. A real zero is `0`; unknown is empty. `estimated_usd` is also empty on rows whose collection or pricing is not complete.

### Text escaping for spreadsheets

A spreadsheet can read a cell that starts with `=`, `+`, `-` or `@` as a formula. To prevent that, the export adds one apostrophe (`'`) in front of a text cell when any of these is true:

- the cell starts with an apostrophe;
- it starts with a tab, carriage return or line feed (before any other text);
- its first character that is not white space is `=`, `+`, `-` or `@`.

Only the text columns are treated this way: `project`, `attempt_id`, `coding_agent`, `mode`, `invocation_id`, `parent_invocation_id`, `actual_model`, `model_provenance`, `pricing_status`, `collection_status`, `diagnostic_codes`, `row_kind`, `execution_path`, `graph_node`, `outcome`, `agent_version`, `source_format` and `source_invocation_id`. Numbers and timestamps are written as they are. Normal CSV quoting is applied after the apostrophe.

To undo it, remove exactly one leading apostrophe from a text cell when there is one. A real leading apostrophe was doubled, so it comes back correctly.

## Recover missing historical sub-agents

Older Codex attempts can be missing the usage of sub-agents. `meter recover` finds it again from the Codex session files on this machine. It repairs the usage records only; it does not re-run anything or contact a provider.

```sh
litefactory meter recover github:acme/api 195 --attempt ATTEMPT_ID
litefactory meter recover github:acme/api 195 --attempt ATTEMPT_ID --apply
```

1. Find the attempt ID of a finished attempt with `litefactory meter github:acme/api 195 --details`.
2. Run without `--apply` first. It changes nothing and prints JSON. Check `unresolved`, `additional_observed_usd` and `resulting_observed_subtotal_usd`. `"applied": false` means a preview or an unresolved result, not a finished repair.
3. Run again with `--apply` to record what the evidence supports. It runs only when nothing is unresolved. It checks the sources again and adds the missing records in one step, at the prices the attempt originally used. Existing records, outcomes and times are kept. Repeating it after success adds nothing, and an interrupted run rolls back, so you can retry.

The repository, issue and attempt must match. Only Codex attempts with a supported saved root identity and clear evidence can be repaired. Active attempts, changed sources, overlapping or unclear parent links, and unsupported files are refused or left unresolved. Fix the cause and preview again. After recovery the total is still a lower bound at recorded rates, and the full total stays unknown.

## Related pages

- [Command reference: usage and costs](cli/command-reference.md#usage-and-costs)
- [Operations](operations/README.md) and [troubleshooting](operations/troubleshooting.md)
- [Documentation index](README.md)
