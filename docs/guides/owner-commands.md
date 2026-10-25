# Owner commands

[Documentation](../README.md) · [Using the factory](README.md)

This page lists every comment the factory understands on a GitHub issue, and the
labels it reads. It is checked against the comment parser in `internal/turn` and
the daemon code that routes each command.

## Who can use commands

- Only the **owner** can give instructions. The owner is the GitHub account that
  `litefactory init` found through `gh`. The factory compares the numeric user
  ID, not the name.
- Comments by the **bot** (and by any account GitHub marks as a bot) are the
  factory's own. It never acts on them.
- Comments by **anyone else** are recorded and ignored. They start nothing and
  get no reply.
- Everything below is owner-only unless the text says otherwise.

An issue can also start work without a comment. If the issue is assigned to the
owner, has a body and has no comments yet, the body is read as the first
instruction. A body that names a command that cannot start work (for example a
bare `/spike`, an unknown command, or a Plan or Validate control) does nothing
and gets no reply.

## How a comment is read

- Only the first word counts. `/plan` must open the comment (blank lines before
  it are fine). A `/plan` in the middle of a sentence is plain text.
- A command word is a slash followed by letters, digits, `-` or `_`, and is not
  case-sensitive. A path such as `/usr/bin/git is on the PATH` is plain text.
- Text that does not start with a command is a normal instruction. The factory
  picks the mode (see below) and runs it.
- An empty comment is ignored.
- An unknown slash command gets a reply and starts nothing.

## Choosing a mode

| Syntax | What it causes |
| --- | --- |
| `/research <text>` | Runs Research on the text. |
| `/plan <text>` | Runs Plan. On a parent with an approved, locked plan this is also the way to ask for a replan. |
| `/implement <text>` | Runs Implement. Managed work needs an approved plan. |
| `/validate <text>` | Runs Validate, a read-only check. |
| `/spike <text>` | Runs Spike, a small experiment or ad-hoc change. |
| Plain text | Continues the mode already chosen for the issue. With no mode chosen, the factory picks one itself. |
| `replan` | The whole comment, any case. Reopens Plan on a parent whose plan is locked. There is no `/replan`. |

A mode command with no text after it gets a reply asking for text and starts
nothing. The mode is also stored as a label (see below).

### The ask tag

A line that is exactly `lf: ask` (optionally with trailing spaces) lets a run
whose mode was picked by the factory ask you questions instead of guessing.
Research and Plan can already ask questions without it. Implement, Validate and
Spike never ask. Writing "do not ask" or "no questions" in the instruction turns
questions off. The tag line is removed before the text is used, and a comment
that is only the tag does nothing.

## Retained instructions and standing guidance

Ordinary owner instructions are retained until assigned to the next eligible
logical step, including when another reply occupies its wait or no next step
exists. An explicit target restricts which step can receive them. Pending input
is shown as undelivered; assignment does not grant a new run or scope.
`/guide <text>` sets or replaces optional issue guidance, `/guide` views it,
and `/guide clear` withdraws it. These controls do not start work and remain
separate from one-time instructions and decisions. See [owner input](owner-input.md).

## Controls on a parent issue

These are not modes. They answer a proposal the factory posted, and the posted
comment shows the exact text to copy.

| Syntax | What it causes | What it will not do |
| --- | --- | --- |
| `/approve-plan set <revision>` | Promotes the proposed story set (64-character revision) and creates the stories. | Does not accept a stale revision; a changed set needs a new review. |
| `/approve-plan <story-key> <stage> <candidate>` | Approves one stage (`story`, `technical` or `acceptance`) of one story. Post it on the story's sub-issue or on the parent. | Does not approve a stage that was changed since the review. |
| `/approve-plan-set <ref>` | Approves the final assembled plan, once every story has all three stages approved. | Does not start Implement. |
| `/plan-new-story` | Reserves one more story key. Exact text, nothing else in the comment. | Does not write the story; Plan does that next. |
| `/approve-bug analysis <revision>` | On a Bug task, approves the posted analysis so the fix starts. | Does not work on a task that is not a Bug task. |

A displayed Plan stage also accepts `/approve-plan` or `/approve-plan d-…`;
the assembled plan accepts `/approve-plan-set` or `/approve-plan-set d-…`.
A bare reply requires one unchanged target across the unresolved displays for
that action. After replacement, use the short reference shown with the new
review. An explicit reference or a provider's threaded reply to that review
selects its exact displayed target before bare shorthand is considered. A
reference takes priority when both are present. Neither is silently retargeted;
replying to an older or edited replacement display requires a fresh reference.

Plan controls only work on a task running the graph Plan workflow. Anywhere
else they are answered as unknown commands. See
[Plan workflow](../workflow/plan-graph.md) and [Bug workflow](../workflow/bug-runtime.md).

## Validate controls

Validate posts a report with a 12-character revision and short decision references.
Each command is the whole comment, on one line (at most 2048 characters).
You may replace `<rev>` with the displayed `d-…` reference or leave it out when
all unresolved displays for that action identify the same target and no newer
target has been shown. A replacement requires its displayed short reference;
a delayed reference never selects newer work. Full revision commands remain
available, but a revision displayed for different delivery heads is ambiguous.
On providers with threaded replies, replying directly to an unchanged report
also binds a short command to that report. GitHub issue comments do not carry
threaded reply metadata; use the displayed short reference there when needed.

| Syntax | What it causes |
| --- | --- |
| `/validate-check <rev> <criterion> pass\|fail -- <evidence>` | Records your manual verdict on a criterion. Evidence is required. |
| `/validate-finding <rev> <finding> repair` | Have the factory repair the finding. |
| `/validate-finding <rev> <finding> defer` | Defer the finding. |
| `/validate-finding <rev> <finding> issue bug\|story` | Turn the finding into a new bug or story issue. |
| `/validate-repair <rev> <candidate-id> <review-digest> inspect\|accept\|discard` | Inspect all linked retained review parts, adopt that exact eligible candidate, or discard it without applying files. |
| `/validate-push <rev>` | Push the validated branch. |
| `/validate-merge <rev> <pr-number> <head-sha>` | Merge that pull request at that exact head commit. |
| `/validate-close <rev> <issue-number>...` | Close the listed issues (1 to 64). |
| `/validate-ship [reference] -- <reason>` | Authorizes shipment of the displayed delivery commit despite all its displayed blockers, then pushes, merges and closes. |
| `/return [reference] -- <requested change>` | [Returns reviewed work](return-reviewed-work.md) directly to Implement on the same feature branch with your explanation. |
| `/validate-implement <rev> [-- <guidance>]` | Legacy return of blocking findings or suite failures to Implement. |

Shipping keeps the original failed tests, missing reviews and other evidence.
Its audit retains the tested commit and the displayed delivery commit separately,
exact blocker identities and evidence revision, authenticated owner, source
comment, reason and time. Changed work, new blockers or changed evidence require
a fresh decision. Confirmed delivery says “shipped with owner waiver”; a failed
or uncertain delivery keeps its actual status.

Retained repairs require the current report revision, candidate identity and full
review digest shown in the report. Acceptance requires confirmed complete review,
the authenticated task owner and existing code authority. Changed base, frozen
scope or evidence blocks adoption; frozen acceptance-criteria edits are never
eligible. Inspect and discard grant no code or shipping authority. Reset, restart,
generic agreement and agent reports never count as approval.

A stale revision, wrong grammar or an issue with no Validate run is refused and
changes nothing. See [Validate](../workflow/validate.md) and
[validation and delivery](validation-and-delivery.md).

## Scheduling, conflicts and delivery

| Syntax | What it causes |
| --- | --- |
| `/schedule -by HH:MM:SS <text>` | Runs the text after a delay from now. |
| `/schedule -to MM/DD/YYYY HH:MM:SS [zone] <text>` | Runs it at a clock time. Zone is `UTC` or an IANA name; default is the daemon's local time. |
| `/schedule --after <id[,id...]> -by HH:MM:SS <text>` | Runs it after earlier schedules succeed, plus a delay. |
| `/schedule --propose <text>` | Asks for a reviewed schedule proposal. |
| `/schedule --accept <id>` / `--reject <id>` | Accepts or rejects one proposal. |
| `/confirm <proposal-id> apply this resolution` | Applies a stored conflict-resolution proposal after fresh checks. |
| `/deliver <generation>` | Prepares a delivery proposal (pull request) for a finished managed parent. |
| `/merge <generation> <digest> <pr> <head-sha> main` | Approves that exact pull request, head and target. |
| `/followup <generation> <sha256> linked\|attach` | Approves a proposed follow-up as linked work, or attached as a child. |
| `/waive <generation> <selector[,selector...]> -- <reason>` | Records a reasoned exception for the current frozen generation. |

Notes:

- `/schedule` never runs work directly; it files a request that becomes eligible
  at its due time. Delays and clock parts are two digits and positive. Nested
  `/schedule`, ambiguous daylight-saving times and bad zones are refused. Details:
  [scheduling](scheduling.md).
- `/confirm` needs a positive proposal number and that exact phrase in lowercase
  letters, with nothing extra. A "yes" or reaction is not consent. See
  [sync and conflicts](sync-and-conflicts.md).
- `/deliver`, `/merge`, `/followup` and `/waive` apply to managed parents. A head
  that moved since the proposal needs a new `/deliver`. A follow-up never waives
  the source requirement. The digests are 64 lowercase hex characters, the merge
  head is 40. Waiver selectors are `criterion:<story>:<key>`,
  `review:<story>:code|architecture|plan`, `story:<story>`, `tests:<story>`,
  `child:<id>`, `acceptance`, `readiness` or `all`. See
  [validation and delivery](validation-and-delivery.md).

## MCP story creation

| Syntax | What it causes |
| --- | --- |
| `/mcp-create prepare <JSON>` | Prepares a story-creation request and shows the redacted content with its SHA256. Approves nothing. |
| `/mcp-create <sha256> linked` | Approves the prepared batch as linked issues. |
| `/mcp-create <sha256> attach` | Approves it and attaches the issues to the parent. |

The issue must be in Research mode and creation must be switched on in
`config.yaml`. Approval expires after 30 minutes. See [MCP](mcp.md).

## Doctor

| Syntax | What it causes |
| --- | --- |
| `/doctor <instructions>` | Starts or continues a diagnosis of this issue. Each turn needs the prefix. |
| `/doctor stop` | Ends the current doctor conversation and keeps its history. |
| `/doctor apply` | Approves the current repair proposal. If repair is not set up, it says so and changes nothing. See [Doctor](../operations/doctor.md). |

Doctor only reads and explains. Comments from anyone but the owner are ignored.
`/doctor` with nothing after it, or extra words after `stop` or `apply`, gets a
usage reminder. See [doctor](../operations/doctor.md).

## Judge

| Syntax | What it causes |
| --- | --- |
| `/judge` | Challenges the latest eligible saved research result, Plan or Bug analysis on this ticket. |
| `/judge <focus text>` | Same, with the free text as the focus. It is the same as `--focus "<focus text>"`. |
| `--target <saved-reference>` | Judges that exact saved source. An invalid reference is explained and never replaced. |
| `--agent claude\|codex\|other` | Chooses the judging provider. `other` picks the opposite of the source's provider. |
| `--model <model>` | Chooses the model. |
| `--thinking <level>` | Chooses the provider thinking level. |
| `--focus <text>` | Names what to check. Giving both free text and `--focus` is refused. |

When older saves have no verified order, a plain `/judge` picks the newest listed
save and the report says so; use `--target` to pick another. Unknown or duplicate
options get a refusal with a usage line. The judge only advises and changes no
saved work. See [Challenge saved work](judge.md).

## Labels the factory reads

| Label | Meaning |
| --- | --- |
| `Mode: Research`, `Mode: Plan`, `Mode: Implement`, `Mode: Validate`, `Mode: Spike` | The chosen mode. Use exact capitalization. |
| `Coding Agent: Claude`, `Coding Agent: Codex` | Which configured agent runs the issue. With no label, the first agent in `config.yaml` is used. |
| `Sync: On`, `Sync: Off` | Turn base-branch sync for this issue on or off. With no label, the config setting applies. |

Use one label per family. Two labels of a family, or a malformed one, are
refused with a reply. `lf:tools=<value>` labels are no longer read and are
refused; permissions come from the configured mode profile. Checkboxes,
reactions and manual closing grant no approval.

## Commands that no longer exist

No command was found that the parser used to accept and now refuses. The old
`/replan` form was never accepted; use the word `replan`.

## Discord commands

Discord is driven by ordinary messages in a channel bound to a conversation,
not by GitHub comments. Its only slash command is `/details`, which shows saved
raw details for a conversation answer. See [Discord](../discord.md).

For stopped work, follow the single recommendation in its retained stop notice. Pending automatic recovery needs no command: wait until admission can start the existing action. Ordinary owner guidance resumes a supported current Bug or Research question once. It can also resume the latest cancelled unallocated graph dispatch from its saved launch, including Validate on an Implement issue without a question wait. A missing saved launch retains refusal; it never falls back to ordinary Implement.

For a stale blocker, `/doctor <concrete inspection question>` is diagnosis only. `/doctor apply` approves exactly the displayed proposal. The four bounded graph transitions are dispatch cancellation, wait cancellation, attempt cancellation and instance failure. A whole dependent graph repair, its guidance handoff, verification and numbered journal commit together or all abort. Rollback needs a separate exact inverse proposal and approval and is refused after related progress. Repair does not itself authorize continuation or prove completion. See [Recovery](../operations/recovery.md) and [Doctor](../operations/doctor.md).

Retired single-agent Spike uses a fresh graph task with `/spike`; unsupported legacy Bug uses a new issue for the same bug. Carry the prepared historical context and source link to the new task and post the new link on the old one. Legacy Research needing migration requires operator inspection. Existing history, edits and completed effects remain intact.
