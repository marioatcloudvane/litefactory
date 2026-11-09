# Research brief — #1489 Rights per repository: plain-text grants decided by a classifier

Follow-up of #1267. Commit read: `d0257178541dd24f9f87e4561ec3a9bd76329fd7`.
Permalink base: `https://github.com/marioatcloudvane/litefactory/blob/d0257178541dd24f9f87e4561ec3a9bd76329fd7/`.
Data model and interface sketch: [ACCESS_MODEL.md](ACCESS_MODEL.md).

Status: scope drafted; five owner questions open (q1–q5). The recommended answers are already written in as `O-` entries marked "pending q…". If the owner says "yes to all", they stand as written.

## Outcome ledger

### S-01 mode=feature+change+agent-guidance, size=large — new grant store, a classifier role, and a change to every owner-only input gate (agent's call)
Feature: grants and the classifier are new. Change: about 12 places compare the comment author with the single owner id, and they must accept an authorized grantee. Agent-guidance: the classifier is a new Markdown role. Plan should split the work into independent stories (see "First slice").

### O-01 The owner can give a person a plain-text grant per repository (owner said: issue body "Authorize a user id per repository")
For one registered project, the owner grants one external identity (GitHub user, or Linear user, matching the project's ticket connection) and attaches free text that says what the person may do. Each project can hold several grants. A grant is revocable and every change goes to an append-only history. The grant text is stored as written. Go never parses it.

### O-02 The owner manages grants from the local CLI (agent's call)
`litefactory repo access grant|list|revoke|history <project-id> …`. It sits beside `repo approver` and uses the same identity resolution: login to numeric id for GitHub, and Linear user resolution through the project connection. Text comes from `--text` or `--file`. Changing the text creates a new revision. The comment path never changes grants. An MCP or chat tool for this is N-02.

### O-03 A foreign request from a grantee is checked by a classifier before anything acts on it (owner said: "Classify each foreign request")
A request is any issue body or comment, from a person who is neither the owner nor the bot, on a project where that person has an active grant. The check runs:
- before an issue body would self-start a run;
- before a comment or command would start a run or resume a waiting stage;
- before an answer, change request or approval given at a waiting node.

All three go through one admission point, the place where today's filter returns `third_party` and where the issue-body path drops non-owner bodies. "Allowed" means the input is treated exactly like an owner instruction, attributed to the grantee.

### O-04 The classifier sees data, never Go constants (owner said: "must not assume a fixed list of modes or stages")
Inputs:
- the grant text;
- the request: issue title, issue body, the comment text and the parsed command, if any;
- the target: the workflow it would start, or the workflow and node it would answer.

The target's facts are read from the repository's installed team graph: the node name, `lf_kind`, `lf_action` and `lf_access` (read or write) of the target node and of the agent nodes it could reach. The read/write facts let the classifier judge grants like "read-only runs only". No Go code names a workflow, node or stage in the decision.

### O-05 The classifier returns a strict structured decision; anything else is a refusal (owner said: "deny by default")
The output is `{"decision":"allow"|"deny","reason":"<one plain sentence>"}`, parsed exactly. All of these count as deny: malformed output, a timeout, a provider error, a missing team graph, or an input over the size cap. Nothing is retried automatically. The route for this output is pending q5.

### O-06 A refusal is visible on the issue in plain words (owner said: "a refusal must be visible on the issue in plain words")
The factory posts one marked comment that names the person, says the request was not run, and gives the classifier's reason. A classifier failure gets its own wording ("could not be checked, so it was not run; the owner can repeat it"). The owner can always repeat or approve the request themselves.

### O-07 Every decision is recorded once and never reused for another request (agent's call)
A decision ledger stores one row per request (provider comment or issue id). The row holds the actor, the grant id and revision, the target, the decision, the reason, the model and the time. It makes the check idempotent across retries and gives the owner an audit trail. Decisions are not cached across requests. A grant change takes effect from the next request on (forward-only, matching #495 d18). Already admitted inputs and running work are not revoked. The next wait reply of a running task is a new request and is checked against the current grant.

### O-08 Hard owner-only floor, enforced in Go before the classifier (pending q1; recommended)
No grant text can unlock these:
- code-control commands (`/deliver`, `/merge`, `/validate-ship|push|merge|close`);
- a reply that equals a node's `lf_approve_reply` (today `ship` on review nodes; this is a generic graph attribute, not a workflow name);
- grant, team, approver or config changes (already local-CLI only);
- reading secrets (no comment path exists).

A floor hit is refused without calling the classifier. The refusal comment says "only the owner can do this".

### O-09 Grantee input flows through the existing owner pipeline (agent's call)
The single-owner equality checks move behind one helper: "is this input an authorized instruction, and from whom". The callers include owner input, follow-ups, owner exceptions, plan children, plan story task, story refinements, implement timeout reply, the research resume, owner batching, Linear delivery and the in-turn approver claim. Owner batching (#1466) keeps its rule: one author per batch, and a different author ends it. Each grantee batch is classified once, as a whole.

### O-10 The permission classifier is a Markdown role with a factory default (pending q4; recommended: team content with factory fallback)
The role text lives in the team source, as `teams/source/base-team/access/access-classifier.md`, so a team can tune it. If an installed team has none, the factory uses its embedded default. The Go side only assembles inputs, runs it with no tools and no session, and parses the output.

### O-11 Cost is bounded (agent's call)
Only people with an active grant ever reach the classifier. Strangers stay silent as today: no call, no comment. The input is capped at 64 KiB, the same as owner batches; above that the request is refused. The classifier runs with no tools and no session through the existing classify path. A per-grantee cap of 30 checks per project per hour applies; over the cap the request is refused with a "too many requests, try later" note. Plan may tune the number.

### O-12 The old fixed-catalogue rights system is retired (pending q2; recommended)
Removed: `internal/rights`, `internal/factory/localrights`, the `litefactory rights …` CLI, the `lf_rights` graph attribute and its validation, and the `rights_grants`, `reviewer_assignments` and `rights_history` tables. A migration drops the tables. Before that, any existing rows are listed in the migration notice. They are not converted, because fixed workflow/stage/action triples do not map to plain text.

### O-13 First slice, deliverable on its own (pending q3; recommended: slice A)
Slice A:
- grant table, CLI and history (O-01, O-02);
- the classifier and its decision ledger (O-04, O-05, O-07);
- the floor (O-08);
- refusal comments (O-06);
- GitHub and Linear grantees can start runs and answer or approve at waiting nodes (O-03, O-09).

Slice B (later): an MCP owner tool for grants, and a "dry-run" CLI that shows what the classifier would say for a sample request.

### N-01 No change for the owner (owner said: "The owner is untouched")
The owner never reaches the classifier: no call, no added latency, no new ledger write on their inputs.

### N-02 No MCP or chat tool for grant management in the first slice (agent's call)
Slice B.

### N-03 Repository roles on GitHub grant nothing (agent's call, carried from #495 a6)
Collaborator, maintainer or Linear team membership gives no factory rights. Only a stored grant does.

### N-04 No team-defined new modes (agent's call)
The set of modes (research, plan, implement, validate, spike, bug, doctor) is fixed in Go today. A "Ram mode" would first need the factory to support team-defined modes. That is separate work. This design already handles one, because names reach the classifier as data.

### N-05 No grants for strangers through issue text, and no self-service requests for access (agent's call)

### N-06 No automatic undo of work already admitted when a grant is revoked (agent's call; forward-only)

### K-01 The owner path, the project approver and its claims ledger keep working unchanged
This covers `project_approvers`, `project_approver_appointments`, `project_authority_claims`, `RequireFresh`/`ActivatedAt` and legacy identities. The approver remains "the owner" of a project.

### K-02 Bot, forge-bot and marker comments stay `self`; strangers without a grant stay silent third-party

### K-03 Code control stays pinned to the owner
`verifyTurnCodeAuthority` and the non-GitHub ticket owner check are unchanged.

### K-04 Factory triggers keep skipping the approver claim
These are scheduled bug filings and continuations.

### K-05 The mode classifier and its behaviour are unchanged
Picking a mode for a grantee's free text still uses the mode classifier, after the permission check allows the request.

### K-06 No Go code names a workflow, node or stage in the permission decision

## Owner questions (open)

- q1 (business) Owner-only floor: recommended (a) a fixed floor of code control, delivering approvals and admin; (b) the grant text decides everything.
- q2 (business) Old fixed-catalogue rights system: recommended (a) retire it and drop its tables; (b) keep both.
- q3 (business) First slice: recommended (a) run starts plus stage answers and approvals; (b) run starts only.
- q4 (technical) Home of the classifier role: recommended (a) team content with an embedded factory default; (b) factory-embedded only, like the mode classifier.
- q5 (technical) Result route: recommended (a) strict JSON on stdout from a no-tools run; (b) MCP `communication_*` tools.

## Findings (observed unless marked)

1. **Today exactly one person per project can instruct the factory.**
   - `ClassifyReply` returns `instruction` only when the author equals the owner id; everyone else is `third_party` ([internal/turn/ticket_classify.go#L40-L57](https://github.com/marioatcloudvane/litefactory/blob/d0257178541dd24f9f87e4561ec3a9bd76329fd7/internal/turn/ticket_classify.go#L40-L57)).
   - In project mode that owner is the project approver, which supports GitHub and Linear (`internal/factory/runtime/ticket_admission.go` L70-105).
   - A non-owner comment is consumed silently (`internal/daemon/tick.go` ~L2449).
   - A non-owner issue body only logs (`tick.go` ~L2131).
   - Code-control commands get a refusal comment (`internal/daemon/delivery.go` L56-125).
2. **The single-owner check is repeated in about 12 places** in `internal/daemon`, as equality with `OwnerUserID`: owner_input, followups, owner_exceptions, plan_children, plan_story_task, story_refinements, implement_timeout_reply, research_owner_resume, owner_batch and linear_delivery. A second check runs inside the turn ([internal/turn/execute.go#L537-L568](https://github.com/marioatcloudvane/litefactory/blob/d0257178541dd24f9f87e4561ec3a9bd76329fd7/internal/turn/execute.go#L537-L568); `internal/turn/project_approver.go`). This is the "change" part of S-01.
3. **The existing rights system cannot be used, which is what #1267 meant by "not useable at the moment".**
   - `internal/rights` stores fixed `workflow | stage | action` grants in `rights_grants` (`internal/store/migrations/0101_rights.sql`).
   - `Check` has no caller outside tests ([internal/rights/check.go](https://github.com/marioatcloudvane/litefactory/blob/d0257178541dd24f9f87e4561ec3a9bd76329fd7/internal/rights/check.go)).
   - Its catalogue lists only nodes tagged `lf_rights`, and no shipped team graph has that tag (grep of `teams/source` found none).
   - Its identity is a GitHub-only int64, so Linear has no place in it.
   - It also hard-codes the workflow list in Go (`internal/rights/catalogue.go` L60-67).
   - The earlier #1267 work recorded the same: "The rights foundation stores precise grants, but shipped workflows and runtime approval gates do not use it" (commit `ba3a31bf`).
4. **The project approver is reusable for identity, not for grants.**
   - `project_approver_appointments` stores provider, api_origin, actor_id (TEXT) and handle, for GitHub and Linear (`internal/store/migrations/20261011163100000_project_approvers.sql`).
   - `project_approvers` allows exactly one per repo.
   - The new grant table should copy its identity columns and its resolution code (`internal/projectconnection/approver.go` L154-169; `linear.go` L156-164).
   - The `project_authority_claims` ledger (unique repo+kind+key) is the right place for a new claim kind for admitted grantee input (inferred).
5. **The mode classifier is factory-embedded, not team content.** This corrects an assumption in the issue.
   - The role lives at `internal/instructionpackage/embedded/agents/mode-classifier.md`; only an operator overlay in `$LF_HOME/internal-instructions` can change it.
   - It runs through `agent.Classifier`: a stateless `claude -p` with no tools, no session, no setting sources and empty agents (`internal/harness/runner.go` L28-61; codex in read-only mode).
   - It returns one bare word, parsed exactly.
   - It has no fallback; an error leaves the comment unconsumed and retried (`internal/daemon/mode.go` L94-245).
   - The timeout is the work-run timeout, and there is no cost cap.
6. **The team graph already carries the data the classifier needs.**
   - Every node has a name, `lf_action` and `lf_kind`.
   - Agent nodes that write declare `lf_access="write"` (bug, doctor, spike, implement workers, validate repair; `teams/source/base-team/*/*.dot`).
   - So "read-only versus write" is data, not Go knowledge (`internal/graphexec/registry.go` L97-138).
7. **Commands are Go-known factory syntax, not workflow knowledge.**
   - These include `/plan`, `/research`, `/implement`, `/validate`, `/spike`, `/doctor`, `/approve-plan`, `/approve-plan-set` and `/plan-new-story` (`internal/turn/command.go`).
   - The classifier receives the parsed command as data.
8. **Owner batching (#1466) folds same-author free-text comments, up to 64 KiB.** A command or another author ends the batch (`internal/daemon/owner_batch.go` L35-87).
9. **The webhook is wake-up only and does no authorization** (`internal/webhook/doc.go`). No change is needed there.
10. **Owner-filed issues self-start; bot-filed issues never do.** Owner-confirmed bug filings start through a factory schedule (`internal/daemon/bug_filing_enqueue.go`). With this feature, a grantee-filed issue goes through O-03 first.

## Security analysis (recommendation)

- **Prompt injection.** The requester's text is untrusted. Several layers keep it contained (inferred design):
  1. The classifier runs with no tools, no session and no repository access, so text can at worst change its one-word verdict.
  2. The grant text and the target facts sit in a trusted section, and the requester text is fenced as quoted data with an instruction to ignore directions inside it.
  3. The output is parsed strictly, and anything off-shape is a deny.
  4. The O-08 floor is enforced in Go, so even a fooled classifier cannot ship, merge or change rights.
  5. Every decision is in the ledger, so the owner can audit and revoke.
- **Residual risk.** A clever request might still win an "allow" inside what the grant permits in general. That is accepted. The blast radius is what the grant text allows, minus the floor.
- **Grant text is trusted.** Only the local owner writes it.
- **Identity.** Grants match qualified immutable identities (provider + origin + id), never handles. This is the same rule as `ClassifyReply`.

## Impact analysis

- **Touched:**
  - `internal/store`: new migration and queries.
  - `internal/projectconnection`: identity resolution reuse.
  - `internal/cli`: the new `repo access` commands; `cmd_rights.go` removed under q2.
  - `internal/turn`: `ticket_classify.go` admission and the `project_approver.go` claim.
  - `internal/daemon`: tick body path, comment handling, and the ~12 owner checks behind one helper.
  - `internal/agent`/`internal/harness`/`internal/codex`: a classifier call with a new parser.
  - `internal/instructionpackage`: the embedded default role.
  - `internal/teamsource`/`teambuild`: an optional new team role file under q4.
  - `teams/source/base-team`: the new access classifier role.
  - `internal/graphexec`: `lf_rights` removed under q2.
  - Docs: the rights and approver docs (looked in `docs/`; Plan to list).
- **Depends on it:**
  - every ticket input path, for GitHub and Linear;
  - owner batching;
  - Research, Plan and Spike resumes at waiting nodes;
  - Plan control commands;
  - the code-control refusal path, unchanged.
- **Superseded:** `internal/rights`, `localrights`, `litefactory rights` and `lf_rights` (O-12, pending q2). Until removal they stay inert; nothing calls them.
- **Data and compatibility:**
  - New tables (see ACCESS_MODEL.md).
  - Old rights tables are dropped under q2. There may be rows on existing installations; the migration must list them before dropping them.
  - Installed teams without the new role use the factory default (q4).
  - No team release format change beyond one optional role file.

## Edge cases and how the owner tells it works

- Grantee files an issue that the grant allows → the run starts as for the owner, and the decision is in the ledger.
- Grantee files an issue that the grant forbids → a refusal comment with the reason, and nothing runs.
- A plan-stage approver grant: an answer at `story_ask` is allowed, while `/implement` is refused.
- "Read-only" grant: `/research` is allowed; `/implement` is refused, because its graph has `lf_access="write"` nodes.
- `/deliver` or a `ship` reply from any grantee → a floor refusal, with no classifier call.
- The classifier times out → a "could not be checked" refusal, and the owner can repeat the request.
- The grant is revoked between two comments → the second comment is refused, and a run already started continues.
- Linear user on a Linear project → the same behaviour. A GitHub grant on a Linear project is rejected at grant time (provider must match the connection).
- A stranger with no grant → silent, as today.
- An owner comment and a grantee comment are interleaved → batches split by author, and each grantee batch is classified on its own.

## Private notes for the next turn
- Prior research for the old system: `.aiteam/lf-495-rights-system/2026-10-06/rights-system/RESEARCH_BRIEF.md` (decisions d1–d19). Forward-only revocation and "repository roles grant nothing" carried over.
- #1267's issue summary could not be fetched (the MCP request did not complete); I relied on the instruction text and the git history.
