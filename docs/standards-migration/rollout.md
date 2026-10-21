# Shared guidance migration rollout

This migration prepares review inputs and new delivery artifacts. It does not
publish a standards book, approve a proposal, install teams, change a repository
link or change an owner overlay. All deployment operations below remain pending.
The inventory and proposal inputs are review evidence, excluded from team and
embedded instruction capture. Runtime code never reads these files as fallback.

## Review and publish before adoption

1. Inspect the inventory and complete proposed bodies. Inspect the target
   factory's `litefactory standards book current` and `category list`. The
   checked-in proposal starts from an unpublished base for reproducible review.
   For an existing book, explicitly prepare a proposal against its exact current
   revision. Compare every stable ID with existing categories and standards;
   preserve unrelated owner content, and use reviewed replacements for collisions
   instead of silently overwriting or renaming changed meaning.
2. The owner creates the proposal with `litefactory standards proposal create
   shared-guidance-migration --file PROPOSAL_FILE`. Validate that exact proposal
   version using `standards proposal validate shared-guidance-migration --version
   VERSION`. Read the complete `standards proposal review
   shared-guidance-migration --version VERSION` output, including before/after
   data and digests. Any edit or rebase requires a fresh exact review.
3. Only the owner approves and publishes the reviewed version, separately:
   `standards proposal approve shared-guidance-migration --version VERSION
   --digest REVIEW_CHANGE_DIGEST`, then `standards proposal publish
   shared-guidance-migration --version VERSION --digest REVIEW_CHANGE_DIGEST`.
   No automatic approval, SQL seeding or publication accompanies this code change.
4. Record the actual factory identity, proposal ID/version/change digest,
   approval and publication identities, published revision and book digest in
   the deployment record. Until observed, each reference is **pending**. Recover
   a lost publication response by inspecting that same proposal identity before
   retrying; do not create a second proposal merely to hide uncertainty.
5. Only after publication, deploy a binary containing the migrated built-in
   workflows (unlinked repositories use those sources), and use the existing
   team installation and repository
   linking operations documented in [team management](../cli/teams.md) to adopt
   the new release for each affected repository. Existing installed or linked
   releases do not advance automatically. Do not rewrite a historical release.
6. Start a new owner invocation. Inspect its selected delivery closure and use
   `litefactory standards run TASK --run RUN --json` to compare the retained
   book revision/digest with publication. Category selection remains the agent's
   relevance decision, including explicit empty selection. Confirm selected
   guidance was read completely. Observations show delivery and the agent's
   completion report, not comprehension or universal model compliance.
7. Resume that invocation after a later publication: its captured instructions,
   selected categories and retained book must remain pinned. A new owner request
   may bind the later publication; a resumed request must not.

## Prepared team releases

These versions are generated repository artifacts, pending operational adoption:

| Stack | Previous catalogue version | Prepared version |
|---|---|---|
| Go | 1.0.2 | 1.0.3 |
| Python/FastAPI and optional React/TypeScript/Vite | 1.0.2 | 1.0.3 |
| .NET/Blazor | 1.0.1 | 1.0.2 |
| Swift/iOS | 1.0.1 | 1.0.2 |

Generate or check all affected sources together using the existing command:

```sh
go run ./cmd/teamrelease -check \
  -source internal/teamsource \
  -source internal/teamsource/python-fastapi \
  -source internal/teamsource/swift-ios \
  -source teams/source/dotnet-web
```

Generation validates all six workflows and their referenced resources before
writing a new immutable release. The migration preserves all older catalogue
entries and release bytes. Owners must not regenerate an already adopted identity
with edited content; a later change requires another version.

## Delivery and compatibility

New canonical sources retain role procedures, result schemas, receipt recovery,
loop limits, tool ceilings, human gates, executable stack profiles and supported
version/host contracts. Procedure-only pointer skills preserve the captured
profiles' specialist-selection routes and resource closure identities. The
agent-setup stub, for example, retains the profile-selected route to the book
and the local authority boundary; it supplies no migrated design advice. Shared categories apply across participating roles;
stack categories apply to the technology being worked on. Guidance never changes
factory authority, mode, permissions or publication gates.

Missing, corrupt, unsupported and partially read retained books remain visibly
degraded. Work continues under factory controls. Failure is neither an empty
selection nor permission to substitute the current book, old internal advice or
these proposal files. MCP-disabled and tool-ceiling paths do not gain tools.

All older immutable releases, saved captures and owner overlays are preserved.
Embedded fixed-output helpers and conversation roles retain their advice because
they lack the bound run's standards retrieval contract. Their shipped bytes and
metadata version remain unchanged. Their advice is outside the newly migrated
workflow closure.

## Deployment record

| Operation | State / reference |
|---|---|
| Target factory and existing-book comparison | Pending owner operation |
| Production proposal creation and exact review | Pending owner operation |
| Production approval and publication | Pending owner operation |
| Published revision and digest | Pending; no production reference observed |
| Binary deployment, team installation and repository adoption | Pending owner operation |
| New production invocation delivery inspection | Pending |
| Live provider observation | Not performed |

Offline fixture publication and delivery checks are recorded separately in
[evidence](evidence.md). They are not production deployment or provider observations.
