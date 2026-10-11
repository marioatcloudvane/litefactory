# Issue command reference

These controls belong in comments by the configured numeric owner. They are not terminal commands. Replace angle-bracket placeholders with values from the relevant factory report. Mode commands and factory controls start the comment; embedded/quoted commands are not a substitute for a direct request.

## Work and mode selection

| Syntax | Effect and restrictions |
| --- | --- |
| `/research <instruction>` | Select Research and investigate. |
| `/plan <instruction>` | Select Plan; on a locked managed parent, explicitly request replan. |
| `/implement <instruction>` | Select Implement; managed work requires approved frozen scope. |
| `/validate <instruction>` | Explicitly request read-only assessment; separated managed workflow gates apply. |
| `/spike <instruction>` | Select Spike for an experiment/ad-hoc change. |
| Ordinary nonempty text | Continue the selected mode; with no selection, classification can select one. Managed locked scope remains pinned. |
| `replan` | Entire trimmed parent comment, case-insensitive; reopen Plan only when quiescent. No `/replan` alias. |
| `lf: ask` | Exact standalone line, optional trailing whitespace. Legacy infer opt-in; Research/Plan already allow asynchronous questions. Never enables questions in other modes. A tag alone starts no run. |

Mode command names are case-insensitive and require nonempty instruction text. Unknown slash commands and empty mode commands receive feedback and start no work. An explicit current no-question instruction overrides question defaults.

## Scheduling

| Syntax after `/schedule` | Effect |
| --- | --- |
| `-by HH:MM:SS <instruction>` | Due after a positive delay from receipt time. |
| `-to MM/DD/YYYY HH:MM:SS [timezone] <instruction>` | Absolute civil time; optional `UTC`/IANA zone, otherwise daemon local time. |
| `--after <id[,id...]> -by HH:MM:SS <instruction>` | Delay after successful prerequisite schedules. Positive, unique schedule IDs. |
| `--propose <instruction>` | Request a reviewed scheduling proposal. |
| `--accept <proposal-id>` | Accept exactly one positive proposal ID. |
| `--reject <proposal-id>` | Reject exactly one positive proposal ID. |

Controls do not directly launch an agent work run. Delays use two digits per component, positive duration and minutes/seconds 00–59. Nested `/schedule` payloads, ambiguous DST times, invalid timezones and extra decision arguments are refused. Due time means eligibility. Proposal generation requires an installed generator (absent in current CLI daemon wiring); proposals allow 1–16 items and pending advice expires after seven days. New advice supersedes older pending advice for that issue/owner. Rejection does not cancel accepted work. See [scheduling](scheduling.md).

## Conflict, acceptance and delivery controls

| Exact syntax | Effect and restrictions |
| --- | --- |
| `/confirm <proposal-id> apply this resolution` | Apply the reviewed stored conflict proposal after fresh checks. Positive ID; phrase case-insensitive, no extra words or punctuation. |
| `/waive <generation> <selector[,selector...]> -- <reason>` | Separated workflow parent only; explicit reasoned exception for the current frozen generation. No run or mode transition. |
| `/followup <generation> <proposal-SHA256> linked` | Approve exact proposed follow-up content as separate linked work. |
| `/followup <generation> <proposal-SHA256> attach` | Also approve attaching the follow-up as a child; its open state can block readiness. |
| `/deliver <generation>` | Evaluate readiness and prepare a concrete delivery proposal. |
| `/merge <generation> <proposal-digest> <PR-number> <head-SHA> main` | Approve the exact proposed PR/current head/main target. Integration must be confirmed before parent closure. |

Generations/IDs must match current factory records. Proposal hashes/digests are 64 lowercase hexadecimal characters; the merge head is a 40-character lowercase SHA. Follow-up approval never waives the source requirement. A stale head requires another `/deliver` proposal. See [validation and delivery](validation-and-delivery.md).

Waiver selectors are `criterion:<story>:<criterion-key>`, `review:<story>:code`, `review:<story>:architecture`, `review:<story>:plan`, `story:<story>`, `tests:<story>`, `child:<numeric-GitHub-issue-ID>`, `acceptance`, `readiness` or `all`. Child IDs are database identities, not issue numbers. Selectors expand once against current scope; later children are not covered. Commands allow at most 8192 bytes, reasons 4096 bytes and 128 unique selectors of at most 256 bytes each. Non-AC criterion keys use the report's URL-encoded exact parser key. [Owner-exception details](../workflow/owner-exceptions.md) define the expanded obligations and retained original findings.

## Factory MCP creation

| Exact syntax | Effect and restrictions |
| --- | --- |
| `/mcp-create prepare <CreationEnvelope JSON>` | Prepare and report exact redacted content and SHA256. Does not approve or publish. |
| `/mcp-create <SHA256> linked` | Approve the prepared linked batch. |
| `/mcp-create <SHA256> attach` | Approve the prepared batch and reviewed parent attachment. |

Requires selected Research mode, loaded creation setting, authenticated owner and matching repository/source/content. Approval expires after 30 minutes, is bounded by attempt lifetime and consumed by the next eligible native Research launch. Edited controls, stale or changed content and copied third-party commands confer no authority. See [MCP](mcp.md) and the [envelope schema](../mcp-tool-families.md#exact-creation-preparation-and-publication).

## Canonical labels

| Label | Meaning |
| --- | --- |
| `Mode: Research` | Persistent Research selection. |
| `Mode: Plan` | Persistent Plan selection. |
| `Mode: Implement` | Persistent Implement selection. |
| `Mode: Validate` | Persistent Validate selection; managed admission still requires the explicit control. |
| `Mode: Spike` | Persistent Spike selection. |
| `Coding Agent: Claude` | Select configured Claude runner. |
| `Coding Agent: Codex` | Select configured Codex runner. |
| `Sync: On` | Enable per-issue base sync, subject to other gates/limits. |
| `Sync: Off` | Skip per-issue automatic base sync. |

Use exact capitalization and one label per selector family. Multiple or malformed matching labels are refused, including miscased prefixes. No agent label uses the first configured agent; no sync label uses configuration. Switching provider in a retained conversation can require an explicit conversation reset through [CLI](../cli/command-reference.md).

Deprecated `lf:tools=<value>` labels are refused, not permission selectors. Permissions come from the configured mode profile; unrelated labels cannot grant capabilities. Issue checkboxes, manual closure and reactions do not grant acceptance, scope changes or merge approval.

[Using the factory](README.md) · [CLI reference](../cli/command-reference.md) · [Configuration](../configuration/README.md)
