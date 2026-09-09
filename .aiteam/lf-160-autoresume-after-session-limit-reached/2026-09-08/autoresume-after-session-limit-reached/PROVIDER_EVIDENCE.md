# Provider evidence — run 0005 log follow-up

Date: 2026-09-07. **Authentic blocked diagnostics found for both providers.** The earlier blanket BLOCKED verdict below is historical and superseded for planning. Claude has structured reset timestamps. Codex has a provider-owned terminal retry time with minute precision; future implementation uses the machine-local contract below and tests conservative interpretation. No transport change is demonstrated necessary.

## Read-only scope and chronology

The host clock reported 2026-09-07 21:52 CEST, matching the session date. `LF_HOME` is unset; `internal/config/paths.go` resolves the default runtime root. Inspected existing `~/.litefactory/logs/runs` JSONL streams and their metadata read-only. No runtime writes, DB connection, account mutation or paid-agent invocation occurred. Inventory contained 202 completed sidecars plus six streams without sidecars; counts below use `started_at` UTC dates, not file modification time. Codex focus was September 5–6, the two days before today's subscription upgrade. The completed Codex runs were 14 on September 5 (one quota failure) and six on September 6 (all successful). The three-day completed Claude inventory comprised 71 runs. No September 6 Codex quota event was found; the September 5 capture is sufficient authentic evidence of the requested case.

Paths in this document are relative to `~/.litefactory/logs/runs/github/marioatcloudvane/litefactory/`. Only diagnostic fields are reproduced; prompts, tool output, conversation IDs and credentials are omitted. Event ordering/line numbers refer to original local streams; snippets below are explicitly selected fields, not complete fabricated streams.

## Codex: authentic terminal limit on September 5

`100/0006.jsonl:85–86`, sidecar start `2026-09-05T15:24:49.594876Z`, finish `2026-09-05T16:18:38.854242Z`, outcome failed, exit 1, resumed true:

```json
{"type":"error","message":"You've hit your usage limit. Upgrade to Pro (https://chatgpt.com/explore/pro), visit https://chatgpt.com/codex/settings/usage to purchase more credits or try again at 10:24 PM."}
{"type":"turn.failed","error":{"message":"You've hit your usage limit. Upgrade to Pro (https://chatgpt.com/explore/pro), visit https://chatgpt.com/codex/settings/usage to purchase more credits or try again at 10:24 PM."}}
```

Line 1 is `thread.started` with a nonempty thread handle; the sidecar also retains a usable handle. The diagnostic contains no seconds, date, timezone or individual quota windows. It is an actual terminal provider error, not model-generated final prose. The historical binary version is **not recorded in this capture or sidecar**; do not label it 0.153.4 merely because that version was installed during run 0003. The matching 0.153.4 upstream source inspected in run 0003 corroborates this message-only schema and local-time formatting, but is separate evidence.

Current host `/etc/localtime` resolves to Europe/Vienna. This proves the current host zone, not the zone used historically by this capture. `codexEnv` currently excludes `TZ`. The run 0006 owner decision adopts the machine-local assumption below; the old stream must not be relabeled as carrying timezone metadata. Under that assumption with Europe/Vienna same-day context this historical example would mean 22:24 CEST, with conservative due time 22:26 CEST / 20:26 UTC; that conversion is conditional, not metadata recovered from the event.

## Claude Code: structured resets and terminal error envelope

Seven blocked runs on September 5–7 contained eight rejected events (one duplicate). Representative provenance:

| Stream | Version from init | Rejected line | Provider rate-limit assistant line | Terminal result line |
|---|---|---|---|---|
| `89/0004.jsonl` | 2.1.261 | 718 | 725 | 726 |
| `68/0025.jsonl` | 2.1.263 | 727 | 733 | 734 |
| `68/0031.jsonl` | 2.1.263 | 132 | 139 | 140 |
| `70/0014.jsonl` | 2.1.263 | 157 | 165 | 166 |
| `126/0003.jsonl` | 2.1.263 | 304, 312 | 316 | 317 |
| `70/0018.jsonl` | 2.1.263 | 311 | 315 | 316 |
| `70/0022.jsonl` | 2.1.263 | 367 | 374 | 375 |

The last example began `2026-09-07T16:28:40.207897Z`, finished `17:12:41.389633Z`, resumed true, failed, exit 1. Selected fields:

```json
{"type":"rate_limit_event","rate_limit_info":{"status":"rejected","resetsAt":1788806400,"rateLimitType":"five_hour","overageStatus":"rejected","overageDisabledReason":"org_level_disabled","isUsingOverage":false,"unifiedWindows":{"five_hour":{"utilization":1,"resetsAt":1788806400},"seven_day":{"utilization":0.69,"resetsAt":1789102800}}}}
{"type":"assistant","error":"rate_limit","message":{"content":[{"type":"text","text":"You've hit your session limit · resets 8:40pm (Europe/Vienna)"}]}}
{"type":"result","subtype":"success","is_error":true,"result":"You've hit your session limit · resets 8:40pm (Europe/Vienna)"}
```

`resetsAt=1788806400` is Unix seconds: `2026-09-07T18:40:00Z`, matching 20:40 Europe/Vienna in provider text. Due time is `18:41:00Z`. The seven-day window is only 69% utilized; waiting until its later reset would be wrong. `overageStatus=rejected` alone also occurs in allowed events and does not establish an interrupted run. Use top-level rejection plus terminal error semantics. The terminal result says subtype `success` **and** `is_error=true`; subtype alone cannot determine success. Original init and final records have nonempty session IDs; sidecars preserve the handle. Background-task notifications may intervene between rejection and terminal result; keep draining and classify the completed outcome, not an isolated warning.

`68/0025` supplies a midnight case: `resetsAt=1788734400` is September 6 22:40 UTC / September 7 00:40 Vienna, and provider prose says `12:40am (Europe/Vienna)`. Epoch seconds eliminate date and timezone ambiguity entirely.

## Negative evidence and limits

Across September 5–7, Claude streams contained 1,133 `allowed`, 91 `allowed_warning`, and eight `rejected` rate-limit events. A warning/reset timestamp alone must not schedule. Four sidecars recorded `timed_out`: `68/0018`, `89/0006`, `89/0007` around 5,400 seconds and `68/0021` around 10,800 seconds. These are local wall-time deadlines and carry no subscription-reset entitlement; never treat a generic timeout as a quota interruption.

All observed rejected Claude cases involve `five_hour`; simultaneous exhausted windows, weekly-only rejection, quota-without-reset, DST ambiguity, and exit-zero quota are not claimed as authentic captures here. Test conservative behavior for those as synthetic edge cases explicitly labeled as such. Repeated observed rejection records need deduplication. Exact historical Codex binary version remains unknown; EXT-2 versioned verification and fixture-backed adapter tests remain implementation work, not a reason to claim existing CLI support is technically impossible.

### Codex time contract (run 0006 owner decision)

Treat timezone-less provider-owned Codex retry clocks as machine-local time, as explicitly directed by the owner in run 0006. This is the supported operating assumption, not timezone metadata recovered from historical logs, and requires no further timezone approval or proof. Leave both child environments unchanged: do not force `TZ=UTC`, inherit additional variables, or change the existing allowlists (SEC-4). Supply the machine-local location and invocation-start/terminal-observation instants to adapter parsing; use explicit inputs in deterministic tests, without adding a clock to turn or retries below daemon (CONC-6).

Date-bearing supported retry text uses its stated date in that location. Time-only same-day text is eligible only when invocation-start and terminal-observation bounds share one local calendar date; use that date, never blindly roll a past clock to tomorrow. A midnight-spanning time-only diagnostic, unavailable local location, contradictory explicit zone, or a local clock in a DST fold (two possible instants) or gap (no instant) remains report-only with a reason. Do not rely on date-construction normalization to pick an occurrence or repair a gap. If a timezone change during the invocation makes the context inconsistent, keep it report-only. These exceptional cases do not block ordinary machine-local recovery.

Resolve the supported displayed minute to a unique absolute interval, take its next-minute boundary, then add 60 seconds of elapsed time: 15:00 schedules for 15:02 in an ordinary local minute. Normalize the result to UTC once; stored deadlines and visible absolute-UTC `/schedule` actions must not be reinterpreted after a restart or host-zone change. Claude Unix-seconds `resetsAt` remains an absolute instant plus 60 seconds, independent of local timezone.

T-2.1 retains supported diagnostic/version documentation under EXT-1/EXT-2, with unknown historical Codex version labeled honestly. T-2.3 covers the owner-chosen local assumption using injected locations and observation bounds: non-UTC conversion, explicit dates, noon/midnight, date-less midnight spanning, stale clocks, DST folds/gaps, contradictory zones, and minute rounding. Verify existing child environment behavior remains unchanged. Authentic captures and synthetic edge cases stay distinguished; no paid-agent tests or transport migration are needed.

Historical decision: run 0005 proposed forcing the Codex child to `TZ=UTC` and validating that override. Run 0006 explicitly supersedes that proposal with the machine-local assumption; neither the override nor its verification is an implementation requirement.

## Revised implementation contract

Accept narrow provider-owned Codex terminal error text with the owner-chosen machine-local timezone and supported date context; preserve precision evidence. A displayed minute denotes a possible reset anywhere in that minute: use the next minute boundary plus 60 seconds (15:00 → 15:02). Exact Claude epoch seconds use reset plus 60 seconds. For known exhausted windows take the latest required reset; absent enumeration in a provider's terminal retry recommendation is not itself an unknown additional blocker. An explicitly known blocker with missing reset remains ineligible. Do not claim certainty about invisible provider windows; another rejection is handled by the already-planned bounded retry policy.

Re-verification: read new completed sidecar dates and provider terminal envelopes, record stream init version where supplied and current read-only version output separately, sanitize only minimal envelopes, preserve warning→terminal ordering, compare epoch to provider displayed zone, and replay through fake CLI tests. Never induce quota exhaustion or copy raw prompts/IDs into repository fixtures. This run changes planning documents only; feature completion is not claimed.

---

## Historical run 0003 investigation (superseded by the findings above)

### Original provider evidence gate — US-2

Date: 2026-09-07. Status: **BLOCKED**. No adapter implementation or authentic quota fixture is claimed.

## Method and installed versions

Read-only version commands reported `codex-cli 0.153.4` and `2.1.263 (Claude Code)`. Codex also printed a nonfatal PATH-alias permission warning. No model request, account mutation, paid agent invocation, runtime-state inspection, or transport substitution was performed. Upstream source was downloaded to `/private/tmp` for inspection. These are installed versions plus source inspection, **not** versions tested against an actual blocked subscription.

The Codex `rust-v0.153.4` Git tree resolved to immutable commit `3d2ee51ca2d5db578f328aa75e20aa22c0197c9a`. This matches the reported version tag; the locally installed executable was not reproducibly rebuilt or attested against that source.

## Codex: concrete transport limitation

The existing litefactory adapter consumes `codex exec --json`. In that version's upstream source:

- [exec_events.rs, lines 55–57 and 90–94](https://github.com/openai/codex/blob/3d2ee51ca2d5db578f328aa75e20aa22c0197c9a/codex-rs/exec/src/exec_events.rs#L55): terminal `turn.failed` carries an `error` whose only field is a message string. There is no typed quota reason or reset timestamp in this JSONL event.
- [event_processor_with_jsonl_output.rs, lines 440–450](https://github.com/openai/codex/blob/3d2ee51ca2d5db578f328aa75e20aa22c0197c9a/codex-rs/exec/src/event_processor_with_jsonl_output.rs#L440): error notification conversion retains the message and optional textual details, losing structured error classification.
- [Same processor, lines 530–550](https://github.com/openai/codex/blob/3d2ee51ca2d5db578f328aa75e20aa22c0197c9a/codex-rs/exec/src/event_processor_with_jsonl_output.rs#L530): failed turns use the same message-only representation.
- [Same processor, lines 587–595](https://github.com/openai/codex/blob/3d2ee51ca2d5db578f328aa75e20aa22c0197c9a/codex-rs/exec/src/event_processor_with_jsonl_output.rs#L587): unmatched notifications are ignored; no structured rate-limit-window event is emitted.
- [protocol/src/error.rs, lines 643–649 and 753–780](https://github.com/openai/codex/blob/3d2ee51ca2d5db578f328aa75e20aa22c0197c9a/codex-rs/protocol/src/error.rs#L753): the internal quota error has an optional UTC reset, but its displayed retry timestamp converts to local time, drops seconds and timezone, and also omits the date when the reset is today. No-reset errors produce a nonspecific retry-later instruction.

Consequently the adapter cannot recover a precise, unambiguous reset instant from the exposed diagnostic, including around date changes and ambiguous local times. It also cannot prove every blocking window from that event. Parsing this text into an assumed future time would violate the approved plan's no-guessed-reset requirement. The app-server has richer structured information, but switching to or querying another transport is explicitly outside this plan.

This does not claim all past/future Codex versions have identical limitations. It establishes a limitation in the installed version's matching upstream source and prevents claiming that this version fulfills US-2 acceptance.

## Claude Code: capture not established

The installed Claude Code package has a native executable; static inspection found rate-limit event and reset-related symbol strings. Strings alone do not prove terminal-blocking semantics, precedence among windows, or actual serialized output. No sanitized authentic blocked-quota stream from version 2.1.263 was supplied or found in the inspected repository adapter fixtures. The research brief's earlier upstream example was an allowed event from a different version and cannot stand in for a blocked-run fixture.

No eligible Claude adapter is implemented from these incomplete observations. Codex's independently verified limitation already fails the requirement to ship both providers together.

## Implementation consequence

T2.1 and US-2 AC1 remain blocked. T2.2 cannot responsibly introduce an operational interruption contract/adapters backed by the required evidence. Dependent stories must not be represented as completed. No synthetic JSON is labeled authentic and no production source was changed for US-2.

To re-verify after an upgrade: record exact `--version` output; resolve the corresponding immutable upstream source; inspect the actual exec event schema, error conversion and retry formatting; obtain a sanitized complete blocked-quota capture from the existing runner transport without deliberately consuming paid quota. For Claude Code, similarly obtain versioned actual stream-json blocked and allowed/success captures. Verify terminal identity, explicit timestamp units/timezone/precision, all blocking windows, and conversation handle before enabling automatic continuation. If the existing transport still discards that information, revising the transport or timestamp requirements requires a revised plan rather than an invented fixture.

## Choices for a revised plan

1. Keep the strict reset-evidence requirement and existing transports: wait for usable provider output or supported versioned captures. This preserves the current contract but leaves automatic support unavailable for the inspected Codex version.
2. Permit narrowly matched provider-owned error prose and conservative timing assumptions: explicitly relax exact-reset precision, define how local timezone/date and DST ambiguities are handled, and test genuine versioned captures. Dropped seconds could require an extra conservative minute in addition to the requested buffer. This still cannot establish all blocking windows from absent data and broad generated-message matching remains inappropriate.
3. Adopt a structured provider transport or companion query: investigate Codex app-server quota/error information and conversation compatibility, then revise the architecture and test plan. This entails additional provider integration and lifecycle work and is not justified as a silent implementation detail. Claude's actual blocking stream evidence is still required independently.
