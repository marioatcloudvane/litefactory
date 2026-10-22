# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## f1

Current HEAD is 6dc340a9. All five reported rule families remain in current code. These are code-confirmed failure paths, not evidence of their production frequency. Historical file introductions: shared result validator ef78c2d5 (2026-09-13), Plan graph bc314391 (2026-09-25), Doctor result 89130d40 (2026-09-29), Validate repair 5f7af59a (2026-10-06), conversation protocol c6bf8cba (2026-10-13). Introduction commits locate provenance but their broad commit subjects do not establish the authors' reasoning.

Evidence:

- `internal/agentresult/validate.go:53`
- `internal/turn/validate_repair.go:143`
- `internal/doctor/result.go:69`
- `internal/messaging/conversation_protocol.go:178`

## f2

C1 confirmed with a correction: it does not reject every test-file edit or every net line removal. The diff detector counts removed versus added test/benchmark/fuzz declarations, deleted test files, and added skips. Test body edits and added tests can pass unless another signal forbids them. Declared change kinds are unioned with measured kinds. Any basename containing acceptance is classified as criteria. The default graph allows three files and forbids package,dependency,schema,api,tests,criteria. too_big resets to the prior commit and routes to review.

Evidence:

- `internal/turn/validate_repair.go:252`
- `internal/turn/validate_repair.go:349`
- `internal/turn/validate_repair.go:395`
- `internal/turn/validate_repair.go:409`
- `internal/turn/spike_check.go:123`
- `internal/teamsource/validate/validate.dot:61`
- `internal/teamsource/validate/validate.dot:108`

## f3

Existing C1 retention is evidence, not a recoverable full proposal: DiffSince falls back to statistics above its read limit and lists untracked paths without contents; storage truncates the diff to 64 KiB. ResetHard removes untracked files outside keep prefixes. The repair store permits ResultCommit only for kept state, so a flagged proposal needs separate retained commit/ref metadata or a changed state invariant. Interrupted rounds also reset before measurement. Existing rounds, diff digest, diagnosis, report routing and replay are reusable.

Evidence:

- `internal/git/validate_repair.go:16`
- `internal/git/validate_repair.go:77`
- `internal/store/validate_repair.go:36`
- `internal/store/validate_repair.go:133`
- `internal/turn/validate_repair.go:127`
- `internal/turn/validate_repair.go:307`

## f4

C2 confirmed. Completed reason is forbidden, including explicitly present empty reason in Create; completed questions are disallowed; Spike status/check/issue are factory-owned and forbidden; Implement deviations over twenty, multiline, blank or above 500 bytes fail. Every diagnostic currently makes validation invalid. Create returns no candidate unless validation completes. The agent activity emits invalid when no retained submission can be accepted. Questions have workflow consequences and cannot all be classified harmless; dropping a real owner question can silently turn unfinished work into completion.

Evidence:

- `internal/agentresult/validate.go:62`
- `internal/agentresult/validate.go:96`
- `internal/agentresult/validate.go:160`
- `internal/agentresult/validate.go:194`
- `internal/agentresult/decode.go:20`
- `internal/agentresult/decode.go:180`
- `internal/turn/research_agent.go:284`

## f5

C3 confirmed at two admission layers: graph validation rejects above twelve before checking dependencies; state proposal separately rejects above twelve. Names and intents are limited to 80 and 280 Unicode runes and one line. Names must also be case-insensitively unique. Content revision hashes full trimmed entries and sorted dependency lists; rendering already flattens whitespace and escapes table pipes. Display truncation must not overwrite canonical names/intents or change the content owner approval binds.

Evidence:

- `internal/agentresult/plan_graph.go:319`
- `internal/agentresult/plan_graph.go:356`
- `internal/planbrief/plan_set_proposal.go:36`
- `internal/planbrief/plan_set_proposal.go:62`
- `internal/planbrief/plan_set_proposal.go:94`
- `internal/planbrief/plan_set_proposal.go:136`

## f6

C4 confirmed: Doctor requires 1–30 findings and exact inclusion of every collector gap, with max 100 diagnosis gaps and 64 KiB serialized result. Result preparation validates before adoption; retained reads validate again. The collector already owns the source gaps, so deterministic merge can remove the copying burden. Invented evidence handles, unobserved repair preconditions, unsupported targets and unverified repair completion are separate checks that should remain strict.

Evidence:

- `internal/doctor/result.go:69`
- `internal/doctor/result.go:111`
- `internal/doctor/result.go:128`
- `internal/doctor/results.go:71`
- `internal/doctor/results.go:248`
- `internal/doctor/result_unit_test.go:41`

## f7

C5 confirmed with nuance: label and github reference regexes reject answer/clarify text; clarify limit is 1000 bytes. Propose accepts exact owner byte spans and factory references; standards action is a narrow literal exception. quotedSpan rejects an entire turn containing fences, blockquoted/forwarded selections, and an entirely quoted turn; it does not reject every quotation within ordinary owner prose. The current decoder already extracts a single JSON object from surrounding harmless noise. Quote matching must preserve trusted owner-source provenance rather than allow arbitrary model-supplied values.

Evidence:

- `internal/messaging/presentation/conversation.go:102`
- `internal/messaging/conversation_protocol.go:48`
- `internal/messaging/conversation_protocol.go:234`
- `internal/messaging/conversation_protocol.go:278`
- `internal/messaging/conversation_protocol.go:326`
- `internal/messaging/conversation_protocol.go:426`

## f8

One correction is enforced in both the main conversation factory and consult reasoner. The factory reconstructs a boolean corrected state from persisted exchanges and shares the provider call budget with lookups. A second correction therefore touches live looping, restart reconstruction, journaling, reasoner behavior and budget tests; changing the prompt alone cannot implement it.

Evidence:

- `internal/messaging/conversation_factory.go:312`
- `internal/messaging/conversation_factory.go:739`
- `internal/messaging/conversation_reasoner.go:100`
- `internal/messaging/conversation_factory_test.go:94`

## f9

Inference from code and history: the common origin is strict machine contracts combining presentation preferences, workflow semantics, trusted factory facts and automatic edit limits into all-or-nothing admission. Repair comments explicitly keep only small changes and reset every other outcome; Plan comments prescribe splitting large parents; Doctor enforces collector completeness; conversation guards hide internal records and prevent quoted evidence becoming instructions. These purposes support selective relaxation rather than removing all validation.

Evidence:

- `internal/turn/validate_repair.go:141`
- `internal/planbrief/plan_set.go:10`
- `internal/agentresult/decode.go:180`
- `internal/doctor/result.go:56`
- `internal/messaging/presentation/conversation.go:102`
- `internal/messaging/conversation_protocol.go:426`

## f10

Verification: go test ./internal/agentresult ./internal/planbrief ./internal/doctor ./internal/messaging/presentation passed in this worktree. Existing tests explicitly assert field ceilings, omitted-gap rejection, graph dependency integrity, revision freshness and correction budgets. No source changes or live provider/Discord checks were performed; full turn/messaging suites were not run.

Evidence:

- `internal/agentresult/plan_graph_contract_test.go:136`
- `internal/agentresult/plan_graph_contract_test.go:191`
- `internal/doctor/result_unit_test.go:54`
- `internal/messaging/conversation_factory_test.go:94`
