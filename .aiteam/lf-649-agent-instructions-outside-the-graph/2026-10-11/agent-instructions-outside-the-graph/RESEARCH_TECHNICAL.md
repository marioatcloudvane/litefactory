# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## f1

Doctor diagnosis is the compiled FixedPrompt, then the embedded field guide (field_guide_v2.txt, 4.7 KB) and serialized context. Identity, memory count and byte limits are checked before redacted delivery; attempts record prompt and guide versions.

Evidence:

- `internal/doctor/prompt.go:13`
- `internal/doctor/prompt.go:35`
- `internal/doctor/field_guide_v2.txt:1`

## f2

Doctor repair joins FixedRepairPrompt, FieldGuide and an inline REPAIR AUTHORITY sentence, plus a JSON manifest. Typed approved repair execution and independent effect checks stay factory authority.

Evidence:

- `internal/doctor/repair.go:271`
- `internal/doctor/repair.go:544`

## f3

Ask builds its instruction and correction string inline; the current text distinguishes delivery evidence, owner waits and active work. Answers are validated separately for JSON shape and citations.

Evidence:

- `internal/messaging/ask/service.go:122`
- `internal/messaging/ask/service.go:137`

## f4

Draft uses an inline role and correction. JSON shape, sanitization and size bounds are enforced in code after extraction.

Evidence:

- `internal/messaging/feature/draft.go:213`
- `internal/messaging/feature/draft.go:247`

## f5

Conflict and schedule advisors build compiled strings in their prompt builders, called by both the Claude (harness) and Codex runners.

Evidence:

- `internal/agent/conflict.go:134`
- `internal/agent/proposal.go:58`
- `internal/harness/runner.go:79`
- `internal/harness/runner.go:133`
- `internal/codex/runner.go:153`
- `internal/codex/runner.go:203`

## f6

StoryEventInstructions and EngineeringEventInstructions are compiled protocol guidance with schema and byte limits. They sit inside the owner's graph/protocol boundary and stay in code.

Evidence:

- `internal/agent/story_progress.go:190`
- `internal/agent/story_progress.go:243`

## f7

Graph extras stay compiled: node route and result-shape text per mode, headless/asking/worktree rules, plus the Implement story-scope text and the Validate read-only contract. Excluded from this migration by the owner's Book of Standards decision (#728).

Evidence:

- `internal/turn/research_agent.go:20`
- `internal/turn/implement_policy.go:17`
- `internal/turn/validate_flow.go:18`
- `internal/turn/spike_workflow.go:124`
- `internal/harness/prompt.go:741`
- `internal/harness/stories.go:31`
- `internal/harness/validation.go:26`

## f10

Graph launch verifies captured role, model, skills and resource manifests. Workflow capture also captures a judge role when lf_judge is present and refuses replay when retained judge bytes are missing. A new loader must feed fresh capture without replacing retained bytes.

Evidence:

- `internal/agent/delivery_launch.go:33`
- `internal/teamsource/research.go:179`
- `internal/teamsource/judge.go:16`

## f12

Capture differs by family: judge saves role and digest in its request config; conversations journal protected provider inputs; ask, draft, interpretation, narration, advisors, classifier and doctor keep no instruction capture beyond doctor prompt/guide version labels. There is no uniform contract today.

Evidence:

- `internal/turn/judge.go:124`
- `internal/messaging/conversation_factory.go:63`
- `internal/messaging/response/native.go:30`

## f14

Intent (NativeInterpreter) and reply (generation plus independent checking prompts) agents already exist with inline text; they are current migrations, not future #631 hooks.

Evidence:

- `internal/messaging/interpretation.go:48`
- `internal/messaging/presentation.go:220`
- `internal/messaging/presentation.go:225`

## f15

Judge's challenger role is a Markdown file embedded in the binary. Command requests persist Role and RoleDigest before launch. The builder then appends compiled pin/evidence/result text, so the Markdown alone does not cover all judge wording.

Evidence:

- `internal/teamsource/judge/challenger.md:1`
- `internal/teamsource/judge.go:9`
- `internal/turn/judge.go:364`

## f16

/judge and workflow judge share one service. Workflow capture reads the embedded role asset, not an operator-edited file; advisory status is preserved.

Evidence:

- `internal/turn/workflow_judge.go:22`
- `internal/teamsource/research.go:183`

## f17

Judge authority is code-enforced: exactly five tools (communication_preflight/receipt/submit, evidence_list, evidence_read). Editing the role cannot widen it.

Evidence:

- `internal/agent/judge.go:17`
- `internal/agent/judge.go:42`

## f18

Conversations carry instruction text in three places: ConversationInstructions inside the prompt JSON and as provider system prompt (Claude) or developer instructions (Codex) on every turn; factoryConversationInstructions with correction string; and the action-outcome suffix.

Evidence:

- `internal/agent/conversation.go:60`
- `internal/harness/conversation.go:166`
- `internal/codex/conversation.go:172`
- `internal/messaging/conversation_factory.go:22`
- `internal/messaging/conversation_factory.go:242`
- `internal/messaging/conversation_actions.go:286`

## f19

Conversation inputs are size-checked, protected and journaled before launch; resume reconstructs saved prompts. Migration must keep this, plus deterministic confirmations and strict envelope validation.

Evidence:

- `internal/agent/conversation.go:29`
- `internal/messaging/conversation_factory.go:159`
- `internal/messaging/conversation_protocol.go:99`

## f20

The workflow mode classifier is an omitted agent: classificationPrompt in the daemon, run through a stateless Classify contract on both providers.

Evidence:

- `internal/daemon/mode.go:89`
- `internal/harness/runner.go:25`
- `internal/codex/runner.go:106`

## f21

Response-only calls meter generation and checking separately with a bounded budget. Narration may fall back to raw text; extraction errors must use these paths, not change command results.

Evidence:

- `internal/messaging/response/native.go:51`
- `internal/messaging/presentation.go:148`

## f22

Source read at commit e9acd0a9 in a clean worktree. No providers or tests were run, so this shows source behaviour, not deployed extraction.

Evidence:

- `internal/teamsource/judge_test.go:14`

## f23

Judge concern 1 (high) holds: the standard launch check validates graph captures only, so a non-graph call could launch uncaptured or newer-edited text on retry. Judge and conversation have partial persistence before launch; ask, draft, advisors, classifier and doctor have none. A loader needs mandatory capture, a durable unit identity, persistence before launch, rejection of missing or mismatched capture, and retry/restart tests.

Evidence:

- `internal/agent/delivery_launch.go:33`
- `internal/turn/judge.go:124`

## f24

Judge concern 2 (medium) holds as a design gap: no extraction code exists, so atomic publication, compare-exact-bytes before replacing, preserving competing edits, first-use races, crash-safe digest records and consistent capture of role plus field guide must all be specified and tested.

Evidence:

- `internal/doctor/repair.go:271`

## f25

The judge's own evidence coverage was partial: 2027 inventory entries were unavailable and it read only part of the launch code. Its verdict is a scope check, not an inventory audit.

Evidence:

- `internal/agent/delivery_launch.go:33`

## f26

Chat and conversation coexist. A channel bound to a conversation is skipped by the natural-language step; every other channel uses the intent interpreter, reply writer and reply checker. /ask and /draft work on both paths, and the conversation path reaches draft through the factory.

Evidence:

- `internal/messaging/natural.go:54`
- `internal/daemon/chat.go:147`
- `internal/daemon/chat.go:154`
- `internal/messaging/conversation_feature.go:24`

## f27

Codex supports custom subagents (one TOML per agent with name, description, developer_instructions). They inherit the parent's sandbox and approvals and cannot override sandbox, approval, provider or MCP servers. Codex spawns them only when told. The factory already passes agents.<role>.description and .config_file as -c overrides for graph modes and switches subagents off with features.multi_agent=false for other main roles. Claude receives subagents through --agents, which the factory sets to {} to disable. Public Codex docs do not document -c overrides, nesting depth, or codex exec behaviour; no subagent was run.

Evidence:

- `internal/codex/runner.go:486`
- `internal/codex/runner.go:506`
- `internal/codex/runner.go:510`
- `internal/harness/runner.go:44`
- `internal/harness/conversation.go:166`
