# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## f1

Inventory, doctor: diagnosis role is the Go constant FixedPrompt. It is joined with an embedded field guide (field_guide_v2.txt, 3.4 KB) and a JSON context in BuildPrompt. Version strings PromptVersion=doctor-diagnosis-v2 and GuideVersion are stored per attempt in the database.

Evidence:

- `internal/doctor/prompt.go:13`
- `internal/doctor/prompt.go:49`
- `internal/doctor/service.go:28`

## f2

Inventory, doctor repair: FixedRepairPrompt is a Go constant. Line 210 builds the prompt from it, the field guide, and a fixed 'REPAIR AUTHORITY' paragraph inline in code.

Evidence:

- `internal/doctor/repair.go:483`
- `internal/doctor/repair.go:210`

## f3

Inventory, chat /ask: the prompt is an inline string. It mixes wording ('keep the answer short') with a JSON answer schema, the 'untrusted data' rule and the citation rule. A second correction text is appended on retry. Answers are decoded and checked against fact handles after the model returns (DecodeAnswer).

Evidence:

- `internal/messaging/ask/service.go:107`
- `internal/messaging/ask/service.go:122`

## f4

Inventory, chat /draft: inline prompt string with 'no tools', size limits (120 and 1300 characters), 'no commands, markers or URLs' and a JSON shape. Code enforces size and content again afterwards (Sanitize, preview, redaction, 120 and 1500 rune limits), so the text and the checks overlap by design.

Evidence:

- `internal/messaging/feature/draft.go:199`
- `internal/messaging/feature/draft.go:242`

## f5

Inventory, conflict advisor and schedule proposal advisor: two Go prompt builders, ConflictResolutionPrompt and ScheduleProposalPrompt. Both say 'do not call tools', define a JSON schema and put the untrusted data last under a 'data, not instructions' label. Both are called from the harness and Codex runners. Parsing and validation of the reply are separate Go code.

Evidence:

- `internal/agent/conflict.go:139`
- `internal/agent/proposal.go:62`
- `internal/harness/runner.go:78`
- `internal/harness/runner.go:132`
- `internal/codex/runner.go:152`
- `internal/codex/runner.go:202`

## f6

Inventory, story progress: StoryEventInstructions and EngineeringEventInstructions are long constants appended to approved prompts. They hold the event file schema, byte limits (4096 events, 4 MiB, 64 MiB) and digest recipe. This is mostly a protocol contract the factory validates on read, so most of it is enforcement, with a small amount of explanatory wording.

Evidence:

- `internal/agent/story_progress.go:190`
- `internal/agent/story_progress.go:243`

## f7

Inventory, fixed text appended to graph-mode prompts: implementNodeInstructions and implementNodeHardRules (result shapes, no commit/push); implementDelegationRequest and the wall-clock limit sentence from implementAttemptGuidance; result-helper access, baseline and 'required final route' text in result_finalization.go; per-mode hints (validate, plan, research/spike, implement) in mcp_guidance.go; the untrusted-note wrapper for the story handoff; validate retry texts. The Research role file itself repeats parts of this text (e.g. how it is invoked), so there are already two sources for some rules.

Evidence:

- `internal/turn/implement_policy.go:17`
- `internal/turn/implement_policy.go:19`
- `internal/turn/research_agent.go:441`
- `internal/turn/research_agent.go:448`
- `internal/turn/result_finalization.go:53`
- `internal/turn/mcp_guidance.go:93`
- `internal/turn/implement_steps.go:419`
- `internal/turn/validate_flow.go:285`

## f8

Drift already happened: the Go hard rule said 'when you cannot finish, submit incomplete' while the 1.2.6 role said otherwise. Fix #643 changed Go (implement_policy.go) in PR #651 and added delegation and time-limit sentences, again in Go. A role edit alone cannot fix such a case today.

Evidence:

- `internal/turn/implement_policy.go:19`
- `internal/turn/research_agent.go:448`

## f9

Graph roles are Markdown with frontmatter (name, description, tier, task) in teams/<team>/v<version>/<mode>/. Release metadata is in release.json, and a generator (go run ./cmd/teamrelease, -check) validates and produces the versioned directories and teams.json. Source exists in four copies (internal/teamsource, python-fastapi, swift-ios, teams/source/dotnet-web). Installed releases are immutable; changed bytes need a new version.

Evidence:

- `internal/teamsource/README.md:1`
- `internal/teamsource/release.json:1`
- `teams/go-development/v1.2.7/research/research.md:1`

## f10

Graph roles are loaded by capture. CaptureWorkflow reads the role, skills and graph from the selected source, hashes them, and keeps the bytes in the run so resume and retry reuse the same text. Research uses a built-in embedded package. Launch refuses a graph request whose delivered role, model, skills or rendered files disagree with the captured selection (ErrDeliveryRefused); advisory calls skip delivery verification.

Evidence:

- `internal/teamsource/research.go:58`
- `internal/teamsource/research.go:129`
- `internal/agent/delivery_launch.go:33`
- `internal/agent/delivery_launch.go:45`

## f11

User editing today: teams install into a central catalog under $LF_HOME/teams. Operators can replace a single agent source with 'team manage team:TEAM:AGENT set source=PATH' (an operator-global override that survives upstream updates), and local clones exist (teampackage clone with origin record). Installed team status reports in-sync, locally-modified, upstream-updated or conflict, and update needs --force to overwrite local edits. Override and clone only cover graph-mode agents.

Evidence:

- `docs/cli/teams.md:29`
- `docs/cli/teams.md:35`
- `internal/teampackage/clone.go:1`

## f12

Non-graph agents have no capture, digest or role/skill delivery. Chat goes through NativeModel.Answer, which takes a plain prompt string, picks the first provider with verified isolation, uses the balanced tier in a private scratch directory and writes a meter attempt. ChatRequest has only Prompt, Model, WorkingDirectory and Usage fields: there is no role or skills field.

Evidence:

- `internal/messaging/ask/native.go:39`
- `internal/agent/chat.go:12`

## f13

Safety is partly in code already: the doctor and chat run only with verified isolation (DoctorIsolation proof, else ErrUnavailable); chat answers are decoded and checked against fact handles; drafts are sanitized, redacted and size-checked; the doctor prompt is size-bounded and checked for identity before send; conflict replies are parsed against exact region lists; doctor repair steps are executed and re-verified by typed code, not by prompt text. Prompt text repeats these limits ('no tools', 'untrusted data'), so editing the text cannot remove the code checks, but it can remove the defense-in-depth sentences.

Evidence:

- `internal/messaging/ask/native.go:43`
- `internal/messaging/ask/service.go:126`
- `internal/messaging/feature/draft.go:242`
- `internal/doctor/prompt.go:36`
- `internal/doctor/repair.go:461`

## f14

Related work: #631 will add new chat agents (intent and reply) and #346 serves team definitions once per factory. #631 has research questions about the narrator having no tools and keeping numbers and links from the data. These new agents are the first natural users of the new file layout.

Evidence:

- `internal/messaging/ask/native.go:23`
