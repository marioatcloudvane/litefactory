# Token metering

Metering records each subprocess attempt and observed root/child invocation.
Retries are separate attempts. Exclusive invocation/model measurements are added
once; provider-inclusive summaries and cache-duration/reasoning subsets are not
additional consumption. Unknown values differ from measured zero.

## Commands

```text
litefactory meter [repository [task-id]] [--mode MODE] [--ca AGENT] [--model MODEL] [--json] [--details]
litefactory meter prices list [--json]
litefactory meter prices set MODEL --coding_agent AGENT --input RATE --output RATE [--cache_read RATE] [--cache_write RATE] [--cache_write_5m RATE] [--cache_write_1h RATE]
litefactory meter export github:OWNER/REPO [--coding_agent AGENT] [--issue NUMBER] [--mode MODE] [--time_from RFC3339] [--time_to RFC3339]
```

`meter prices` is a help grouping. Summary repository selectors accept a registered
path, owner/name or github:owner/name; task-id is the repository-local issue number.
MODE is research, plan, implement or spike; AGENT is claude or codex. Model filters
retain unresolved attribution and incomplete coverage rather than hiding it.
`--details` includes attempts, invocation ancestry and actual models. JSON preserves
null unknown quantities. Text includes complete totals and observed known subtotals.
Separate daemon helper consumption is excluded everywhere.

Prices are user-defined USD per million tokens. Each edit creates a complete
immutable catalog revision, pinned before an attempt launches. Omitted optional
rates become unknown for the replaced entry. Existing attempts, including models
first discovered later in those attempts, retain their pinned catalog. Estimates
may grow as more usage arrives but price edits never recalculate historical work.
No import or repricing command exists. Legacy provider-reported cost is separate.

## Project CSV export

An explicit registered `github:OWNER/REPO` is mandatory; no current-directory or
global fallback exists. All five filters combine with AND. Issue numbers are
positive and local to that repository. No matching measurements produces a header
alone. Unknown/invalid selectors fail before CSV output.

Timezone-aware RFC3339 bounds select **attempt start time** with an inclusive
`time_from` and exclusive `time_to`. Either bound may be omitted; equal/reversed
bounds are errors. Full selected attempts are exported, including children that
finish outside the interval. Bounds are compared as instants, including fractions;
they are not rounded outward to the database's millisecond timestamp precision.

```sh
litefactory meter export github:acme/api --coding_agent codex --issue 73 \
  --mode implement --time_from 2026-09-01T00:00:00Z \
  --time_to 2026-10-01T00:00:00Z > usage.csv
```

CSV v1 has this fixed column order:

```text
project,issue,run,attempt_id,attempt_started_at,attempt_finished_at,coding_agent,mode,invocation_id,parent_invocation_id,actual_model,model_provenance,input_uncached_tokens,cache_read_tokens,cache_write_tokens,cache_write_5m_tokens,cache_write_1h_tokens,output_tokens,reasoning_tokens,price_revision_id,estimated_usd,priced_subtotal_usd,pricing_status,collection_status,diagnostic_codes,row_kind
```

| Columns | Meaning |
|---|---|
| project, issue, run | Registered repository, GitHub issue number, run ordinal |
| attempt_id, attempt_started_at, attempt_finished_at | Subprocess attempt and UTC timestamps; unfinished time is blank |
| coding_agent, mode | Executing agent and work mode |
| invocation_id, parent_invocation_id | Invocation identity and parent; root parent is blank |
| actual_model, model_provenance | Source-proven model, or blank unknown; origin of attribution |
| input_uncached_tokens, cache_read_tokens, cache_write_tokens, output_tokens | Disjoint primary token dimensions |
| cache_write_5m_tokens, cache_write_1h_tokens, reasoning_tokens | Subsets of cache writes/output; never add again |
| price_revision_id | Attempt-pinned catalog revision |
| estimated_usd | Frozen all-in estimate, blank unless collection and pricing complete |
| priced_subtotal_usd | Frozen priced amount for observed work only |
| pricing_status, collection_status | Pricing and collection completeness independently |
| diagnostic_codes | Sorted semicolon-separated controlled codes |
| row_kind | measurement or non-additive coverage |

There is one measurement row per exclusive invocation/model aggregate. Model
changes produce distinct rows. Unknown models remain visible. Attempts and known
invocations without measurements have coverage rows with blank token/amount/revision
cells. Numeric zero is `0`; unknown is blank. Repeated ancestry/coverage metadata
is not a counter. No prompts, transcripts, raw provider diagnostics or helper rows
are exported. Only current measurement-matching frozen valuations are selected.

UTF-8, standard CSV quoting, UTC timestamps and exact locale-independent decimals
are used. Every nonempty text cell is prefixed with one ASCII apostrophe if it
starts with an apostrophe, has leading tab/CR/LF whitespace, or its first
non-Unicode-whitespace character is `=`, `+`, `-` or `@`. This protects spreadsheet
imports. To reverse, remove exactly one leading apostrophe when present; original
leading apostrophes were doubled. Typed numeric/timestamp cells use canonical
serialization. Standard CSV quoting follows text protection.

Rows are ordered by project, numeric issue/run, attempt start/id, invocation and
model (unknown first). Output uses a coherent snapshot capped at 1,000,000 rows,
256 MiB of encoded CSV including header/escaping, and 30 seconds preparation (or
an earlier caller deadline). Limits fail before the header; narrow filters to
retry. Draining uses bounded pages and releases SQLite before writing stdout.
Errors/cancellation return nonzero and may leave a partially redirected file.
Shell redirection owns filenames; no output-file option exists.

## Provider validation pending

Manual authentic captures for supported Claude/Codex versions remain deferred to
the operator. Synthetic parser tests do not certify complete subagent collection.
Collectors combine process streams with bounded associated source reads. Claude
uses message/model resume baselines and parent Agent/Task links; Codex uses rollout
response records and turn-context models, with prelaunch boundaries excluding
historical work. Inclusive summaries and evidence lacking trustworthy baselines
remain non-additive. Missing source/model/descendant evidence remains diagnosed as
partial or unknown. See [provider sources](meter-provider-sources.md) for inspected
source versions; authentic execution certification remains pending. No
historical-log recovery script is part of this repository.

## Exact pricing arithmetic

Rates accept at most 18 integral and 9 fractional decimal digits in USD per
million tokens; excess precision is rejected. Valuation rounds half-even to 12
USD decimal places per measurement and billable dimension, then sums those stored
rounded amounts exactly. Reports and exports add frozen values without repricing
or further rounding.

Cache-write 5-minute and 1-hour quantities are disjoint subsets. Each uses its
specific rate, falling back to the same pinned catalog's generic cache-write rate
when absent. Remaining writes use that generic rate. If duration decomposition is
unknown, a generic rate can price the total writes once; the total and its subsets
are never both charged. Without sufficient duration information or a generic rate,
the affected writes remain unpriced. Reasoning is included in output and has no
additional rate.

## Verification and manual capture handoff

Automated tests use synthetic shell executables and real temporary SQLite
stores. They never launch a paid provider. `go test ./internal/harness
./internal/codex ./internal/meter ./internal/store ./internal/turn ./internal/report
./internal/cli` exercises parsing, persistence, frozen prices and presentation;
`make check` additionally runs formatting, vet, pinned lint and race tests.
The end-to-end CLI cases compare reports and CSV and edit prices/add measurements
while output is blocked to check that an export retains its original snapshot.

The manual handoff must record the exact provider version, source format, capture
date and reproduction steps. Capture a fresh root, parallel and nested children,
a failed child, model switches, resume and cancellation. Preserve source-proven
invocation/parent IDs, actual models, token/cache dimensions and finalization
relationships while removing prompts, credentials and unrelated session data.
Commit sanitized fixture bytes with provenance and document any dimensions the
provider does not expose. Replay them through the same automated boundaries and
reconcile exclusive measurements against available inclusive totals. Repeat this
process whenever the supported version or source format changes. Until those
captures establish complete attribution, no provider version is certified by this
feature and the complete-descendant release checks remain open.
