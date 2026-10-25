# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## f1

B1 is a per-command 16 KiB retention cap, not a suite-wide aggregate cap. The current audit checkout is 155c966cad45d98a207607a18c8c41b4d158b60a; the earlier pass used 6dc340a9. suiteCommandOf retains a UTF-8-clean tail and digest without a full-output reference.

Evidence:

- `internal/turn/validate_suite.go:366`

## f2

Repair handoff uses durable failed-command tails, without rendering OutputTruncated; final failure handbacks also carry OutputTail. Thus markers in storage do not guarantee agent-visible completeness information.

Evidence:

- `internal/turn/validate_flow.go:266`

## f3

Executor capture is separately limited to 4 MiB and overflow cancels execution. Output is redacted before CommandResult publication; CaptureComplete distinguishes overflow, redactor truncation and incomplete drain. Preserve this boundary and do not store raw output instead.

Evidence:

- `internal/bugchecks/executor.go:460`

## f4

Prompt body/comment excerpts are 8 KiB, aggregate history 32 KiB, title 1 KiB. capText marks cuts and uses UTF-8 boundaries. Active Instruction is uncapped. Resumed prompts can omit already seen issue/history and render only the trigger, so new artifact pointers must cover resume paths.

Evidence:

- `internal/harness/prompt.go:71`

## f5

History is built from DB ListComments in forge order, excludes the triggering comment and doctor commands, and drops forge IDs when constructing HistoryEntry. A full observed-thread snapshot should be built before that lossy projection or preserve metadata explicitly.

Evidence:

- `internal/turn/prepare.go:1285`

## f6

Ask uses only ReadOpenIssues, requests 50 newest-updated open issues, filters that list for the target and still excerpts its body to 2 KiB. Facts insertion can stop near 64 KiB; gaps explicitly report omissions. Target acquisition is not a direct issue read.

Evidence:

- `internal/messaging/ask/facts.go:89`

## f7

Explicit target extraction allows one issue, clears historical turns, and passes target to collection. Existing GitHub GetIssue can retrieve a specific issue but is absent from ask IssueReader; extend narrowly rather than give the model a write-capable forge client.

Evidence:

- `internal/messaging/ask/service.go:84`

## f8

Response runner deliberately isolates chat in private temporary scratch; ChatRequest has no tools. Budget constant is 60 seconds. A file pointer alone cannot provide missing evidence to this model.

Evidence:

- `internal/agent/chat.go:1`

## f9

Repair store slices Diff at 64 KiB by byte, with no truncation field. Turn computes digest before persistence. Git DiffSince caps read at 2 MiB and can replace errors with statistics; untracked paths have no bodies. Digest can identify the pre-store text but cannot recover discarded content.

Evidence:

- `internal/store/validate_repair.go:127`

## f10

Diff collection and digest assignment occur before repair size/forbidden-kind rejection and possible reset, making this the relevant preservation boundary. Git fallback must be addressed to make a full patch claim.

Evidence:

- `internal/turn/validate_repair.go:235`

## f11

Existing session discovery verifies regular files with rooted symlink-refusing access and distinguishes directory discovery from verified artifact presence. Reuse this safety approach for evidence references; existing brief discovery is not a general output retention service.

Evidence:

- `internal/turn/session_artifacts.go:76`

## f12

Suite reuse copies command rows into another instance. Evidence references added to SuiteCommand must be copied or resolved durably; schema changes must update idempotent finalization and record scans too.

Evidence:

- `internal/store/suite_runs.go:177`

## f13

Local Git history places initial prompt body cap in da85c154 (2026-08-24), history feature in edf82e72 (2026-08-26), suite records in 4f82d713 and repair records in 5f7af59a (both 2026-10-06), and ask facts in f5aea0d0 (2026-10-10). Prompt comments explicitly describe context-budget motivations; this supports independent bounded-feature origins, not one common truncation service.

Evidence:

- `internal/harness/prompt.go:71`

## f14

Existing regression tests assert bounded tails, digests, prompt truncation, history omission and bounded facts; those are reusable baselines but do not establish full-evidence retrieval.

Evidence:

- `internal/turn/validate_suite_test.go:441`

## f15

Git reader explicitly falls back to statistics on non-cancellation errors if the statistics command succeeds, not only on a proven overflow. A revised API should distinguish too-large, unavailable and complete evidence.

Evidence:

- `internal/git/validate_repair.go:36`

## f16

Existing defaults retain worktrees for 14 days and logs and archives for 30 days, so worktree placement alone cannot promise evidence for the lifetime of retained records.

Evidence:

- `internal/config/config.go:573`

## f17

The broad audit used checkout 155c966cad45d98a207607a18c8c41b4d158b60a; the judge-followup pass used 19baea2c945d9fc22c5d5f140b28bf453c3c87c2. These commit identities were observed with git rev-parse, not inferred from a source-file line.

Evidence:

- `internal/turn/bug_graph_fixer.go:152`

## f18

Bug analyst context serializes the candidate, consumed owner replies, reshape reasons and binding, then keeps only the first 64 KiB. The fixer separately shortens approved analysis and owner replies at the same bound; delivery conflict context also calls this helper. Prefix slicing can cut JSON and omit later guidance; these renderers provide no full retained-context pointer.

Evidence:

- `internal/turn/bug_conversation.go:174`

## f19

Spike command records retain only the last 2048 bytes of output, at most 16 arguments of 256 bytes and a clipped reason. The record has no full-output reference. Above the 1 MiB record bound marshalSpikeCheck removes every command output before retrying serialization.

Evidence:

- `internal/turn/spike_check.go:294`

## f20

Spike handoff is separately limited to 64 KiB: it clears all command outputs if needed, then errors if findings still do not fit. This is layered evidence degradation followed by explicit rejection, not a single tail cap.

Evidence:

- `internal/turn/spike_handoff.go:16`

## f21

Validate-to-Implement guidance assembles reviewer evidence, owner verdicts and suite handbacks, then clips the whole instruction at 60000 bytes before storing it in reopened story guidance. Later findings can disappear; retained source records are not accompanied by a complete-guidance reference in this rendering. The 2048-byte owner clip is usually redundant because command parsing already rejects a whole command above 2048 bytes.

Evidence:

- `internal/turn/validate_implement.go:149`

## f22

Run log evidence reads normalize and redact a complete supported record but expose at most a 700-byte prefix, possibly reduced further to fit a page. Citation rereads apply the same cap and requested excerpt size, not a text continuation; nonzero source offsets must be at record boundaries. Records over 256 KiB are omitted by the raw evidence scanner. Paging records therefore cannot retrieve the remainder of a single long record.

Evidence:

- `internal/logevidence/read.go:181`

## f23

Run digests scan at most 64 MiB with a 16 MiB per-record ceiling, condense messages to 1500 bytes, commands to 300 bytes and error tails to 300 bytes, and omit command output by design. Their offsets lead to raw record reads, whose 700-byte and 256 KiB limits can prevent the promised deeper inspection.

Evidence:

- `internal/logevidence/digest.go:15`

## f24

Incident projection first clips detail at 8 KiB and evidence delivery further clips the reported problem at 1200 bytes. Incident pagination moves between incidents, not through one incident's evidence; this is a lossy discovery view whose consumers need a supported original-source read.

Evidence:

- `internal/store/log_incidents.go:99`

## f25

Repository tools redact output to 32 KiB after collection. repo_read first requests the whole pinned file with a 1 MiB bound and only then applies start_line and lines, so reducing a line range cannot recover a file above 1 MiB. Repository grep/log reads separately cap collection at 256 KiB; log has no continuation selector and grep exposes at most 20 matches per file.

Evidence:

- `internal/projectread/projectread.go:105`

## f26

Doctor issue_comments returns 20 identities per page but caps bodies at 8192 bytes, spends a shared 60000-byte budget and may empty bodies to fit encoding. It has no body-range read. Local retention is insufficient recovery by itself: database_query also limits SQLite allocation and output to 64 KiB and rows to 200; read-only shell is a distinct capability, not proof of a completed oversized-value read.

Evidence:

- `internal/doctor/read_tools.go:108`
- `internal/store/doctor_query.go:16`

## f27

Current Doctor diagnosis uses a live path manifest with shell and read tools and the latest 100 run log paths. Its suggestion to query the database for earlier paths remains subject to database_query's 200-row, 64 KiB and 20-second limits; it does not guarantee complete recovery. Older snapshot caps remain largely in repair recheck paths rather than the live diagnosis prompt.

Evidence:

- `internal/doctor/manifest.go:33`
- `internal/store/doctor_query.go:16`

## f28

Doctor memory selects 30 attempts and strips their evidence snapshots with explicit omission text; current diagnosis and proposal are separate. Targeted SQL may help recover retained history, but the database tool can return empty or partial truncated results for large values and removes rows under output pressure; no unconditional memory-recovery claim is supported.

Evidence:

- `internal/doctor/invoke.go:18`
- `internal/store/doctor_query.go:64`
- `internal/doctor/read_tools.go:88`

## f29

Factory conversation input selects eight recent owner sources within 32768 bytes, skips sources that do not fit, and has no omitted-source count in this block. Oversized current input is explicitly refused. Native session continuity can retain earlier turns, so the prepared window is not proof that the provider has forgotten them.

Evidence:

- `internal/messaging/conversation_factory.go:231`

## f30

Prior factory replies are clipped to 4096 bytes with the cut flag discarded, then divided across a shared 4096-byte budget. If the first clip happened, the second clip need not happen, so a reply can lose content without the later reply-excerpt marker. Full answer parts remain in the database.

Evidence:

- `internal/messaging/conversation_context.go:80`

## f31

Conversation lookup and standards results are clipped to 16384 bytes with gaps; reasoner lookup evidence is clipped again to 2048 bytes each and combined consult evidence to 16384 bytes. Repeating an identical lookup can yield the same prefix. The reasoner has six lookups and a four-minute total deadline, which are explicit bounded-work controls rather than stored-source loss.

Evidence:

- `internal/messaging/conversation_reasoner.go:255`

## f32

Ask history retains 20 turns within 32 KiB from 100 local replies, or four 100-message Discord pages, and signals older omissions. The reconstructed question is itself only the delivered 160-rune header, with project text limited to 100 runes; full original question semantics are not preserved by this history format.

Evidence:

- `internal/messaging/ask_history.go:20`

## f33

Active conflict collection refuses the whole proposal on unsafe or oversized siblings: at most 100 files, 1 MiB original blobs and configured aggregate region content (16 KiB default, up to the supported 1 MiB). It reports required bytes and when raising the setting can help. The older 8 KiB-per-file collection helper has no production caller in the searched checkout; it must not be presented as the active path.

Evidence:

- `internal/git/conflict_snapshot.go:53`

## f34

Instruction-package capture rejects files above 1 MiB and total content above 16 MiB instead of silently slicing. Codex main-instruction delivery separately rejects source or encoded argv content above 64 KiB. Chat requests cap prompts at 256 KiB including framing; a proposed 256 KiB target body would not necessarily fit. These are compatibility/admission limits requiring explicit diagnostics, not automatic cap removal.

Evidence:

- `internal/codex/research_delivery.go:15`

## f35

The judge writes complete redacted result and discussion files and points a read-confined shell at them. Its fallback Codex catalog sets a 10000-token truncation policy, while a matched bundled model preserves its native value. Source inspection does not establish whether that setting truncates prompt input or tool responses in a deployed provider; this requires provider contract or live evidence before changing it.

Evidence:

- `internal/codex/judge_arguments.go:39`

## f36

Failure context can shrink at several layers: harness error text is excerpted to 200 bytes for a run reason, the next-turn previous-reason display caps at 1 KiB, and shared result diagnostics used as an error are sliced at 8192 bytes without a marker. Full provider logs and result receipts may still exist, but the error renderings do not themselves provide a recovery contract.

Evidence:

- `internal/turn/result_finalization.go:287`

## f37

Focused existing tests passed for Doctor comment paging and instruction rejection, conversation reply bounds and envelopes, Bug huge-review parking and guidance context, repository tool isolation, log evidence authorization/redaction/citation recovery, and Validate-to-Implement worker and owner guidance. These verify current behavior, not end-to-end recovery of all oversized evidence.

Evidence:

- `internal/doctor/read_tools_io_test.go:112`

## f38

Local history attributes the Bug prompt helper to 7ff0d91b (2026-10-04), Spike tail retention to eb990a49 (2026-09-30), Validate guidance clipping to 4a9d8860 (2026-10-12), the raw-log 700-byte cap to af0c59cb (2026-10-21), and project-read output cap to 947335ec (2026-10-23), supporting independent feature-local origins and continuing reintroduction of the pattern.

Evidence:

- `internal/projectread/projectread.go:20`

## f39

The owner's latest reply explicitly rejects limiting this work to B1–B4 and requests broad research into capped agent input for fault tolerance and agent usability; it answers none of the previous four product-policy questions.

_No source reference was recorded for this finding._

## f40

The judge review at https://github.com/marioatcloudvane/litefactory/issues/1049#issuecomment-6369191393 raises two medium concerns: missing evaluation of standards continuation reuse and unexamined database recovery limits. Both are supported by this pass's source inspection; the judge did not execute tests or certify exhaustive coverage.

_No source reference was recorded for this finding._

## f41

Standards runtime binds retained book content to the root run, consumer and selection, checks cursor identity and digests against retained state, serves exact UTF-8 byte ranges, and returns source digest, total size and next cursor. Its fit check budgets both raw structured JSON and the escaped JSON text copy with a 1024-byte reserve under a 32 KiB ceiling.

Evidence:

- `internal/standards/runtime.go:100`
- `internal/standards/runtime.go:449`
- `internal/standards/runtime.go:512`

## f42

Standards end-to-end tests reassemble quoted multibyte content through broker tools, pin the original book after newer publication and replacement attempt, and reject forged scope and changed content on replay. The retained-across-attempts test reads from the beginning again, so saved noninitial-cursor restart is an additional regression requirement rather than an independently demonstrated result.

Evidence:

- `internal/turn/standards_endtoend_test.go:130`
- `internal/turn/standards_endtoend_test.go:221`

## f43

Doctor database_query opens a separate read-only query-only connection, accepts one SELECT of at most 16384 bytes, has a 20-second timeout, caps rows at 200 and sets SQLite SQLITE_LIMIT_LENGTH to 65536. SQLITE_TOOBIG can become a successful empty or partial result with Truncated=true; no automatic cursor or continuation position is returned.

Evidence:

- `internal/store/doctor_query.go:16`
- `internal/store/doctor_query.go:64`
- `internal/store/doctor_query.go:114`

## f44

Doctor database result redaction can truncate individual fields and a later serialized-size loop removes rows from the end until the result fits 65536 bytes. Therefore the query's requested LIMIT/OFFSET is not evidence that all those rows reached the consumer; continuation must advance only past returned row identities.

Evidence:

- `internal/doctor/read_tools.go:66`
- `internal/doctor/read_tools.go:88`

## f45

Current Doctor diagnosis exposes a read-only shell surface in addition to the bounded database tool. That supports specifying a separate read-only local large-value recovery route without enlarging the SQL tool's allocation ceiling, but no live agent recovery experiment or new consumer-level large-value shell regression was performed in this pass.

Evidence:

- `internal/harness/doctor.go:24`
- `internal/doctor/manifest.go:43`

## f46

Existing focused tests passed in this pass for standards UTF-8 ranges, retained content across attempts/publication, stale selection, missing/corrupt binding, role cursor checks, forged scope and replay integrity, plus Doctor query size/row/time limits and write barriers. No implementation or new tests were written.

Evidence:

- `internal/standards/runtime_test.go:38`
- `internal/turn/standards_endtoend_test.go:130`
- `internal/store/doctor_query_io_test.go:48`
