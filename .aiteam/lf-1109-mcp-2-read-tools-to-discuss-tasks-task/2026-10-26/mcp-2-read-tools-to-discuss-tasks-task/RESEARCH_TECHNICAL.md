# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## f1

The current remote connector knows only factory:overview. Scope normalization rejects unknown tokens; consent enumerates known requested scopes. Adding a category affects registration, advertised/issued scopes, consent and dispatch authorization.

Evidence:

- `internal/connector/server.go:22`
- `internal/connector/consent.go:35`
- `internal/connector/consent.go:86`

## f2

Every MCP request currently requires overview scope before dispatch; tools/list and tools/call implement only factory_overview. A new discussion-only grant cannot work by adding catalogue entries alone. Listing and calls need capability-specific enforcement, including direct calls and already connected revoked clients.

Evidence:

- `internal/connector/mcp.go:48`
- `internal/connector/mcp.go:135`
- `internal/connector/provider.go:8`
- `docs/cli/connector-grants.md:63`

## f3

Connector grant administration already supports inspect and revoke, absolute expiry and renewal. Existing coverage is all registered repositories including private and future registrations; a new category must state its repository exposure clearly rather than assume task-level isolation.

Evidence:

- `docs/cli/connector-grants.md:26`
- `internal/store/connector_types.go:36`
- `internal/store/connector_types.go:73`

## f4

Existing projectread defines repo_read, repo_grep and repo_log, with closed schemas, pinned reads, cancellation, redaction and bounded results. Pin defaults to origin base branch then local base branch, never fetches, and exposes a gap on missing clones/heads. Request has no branch selector. Reuse needs trusted task/base branch resolution to a pinned SHA and must not silently fall back when task branch is absent.

Evidence:

- `internal/projectread/projectread.go:19`
- `internal/projectread/projectread.go:40`
- `internal/projectread/projectread.go:73`
- `internal/projectread/projectread.go:89`
- `internal/projectread/pages_test.go:1`

## f5

Task identity and local state are available through session lookup by canonical task reference. Plan state has shared, published and saved revisions plus per-story stage approvals, candidates and waits. ReviewRevision binds current stage content and upstream semantics, and changing shared content invalidates approvals. Task context must use these semantics rather than display a bare historical approved flag.

Evidence:

- `internal/store/sessions.go:28`
- `internal/store/sessions.go:135`
- `internal/store/plan_workflow.go:158`
- `internal/planbrief/plan_workflow.go:18`
- `internal/planbrief/plan_workflow.go:124`
- `internal/planbrief/plan_workflow.go:267`

## f6

Application result history retains immutable snapshots and independent received, input, engineering, acceptance and publication states. Published owner waits already have bounded reads and current-revision checks. These are reusable sources for task details and research results; summary status alone must not conflate acknowledged input with acceptance or publication. A complete join and legacy-session coverage were not traced in this bounded pass.

Evidence:

- `internal/store/result_history.go:19`
- `internal/store/result_history.go:32`
- `internal/store/result_history.go:49`
- `internal/store/overview_waits.go:35`
- `internal/store/overview_waits.go:66`

## f7

The transport-neutral meterview service already reads bounded snapshots and validates project/task/time filters. Task costs require a project selector; time bounds accept RFC3339 and plain dates in Read. Reports preserve collection/pricing uncertainty and omit estimated USD for incomplete or unpriced rows. Scope/period vocabulary and defaults for the proposed costs tool remain design choices.

Evidence:

- `internal/meterview/request.go:49`
- `internal/meterview/request.go:75`
- `internal/meterview/request.go:138`
- `internal/meterview/view.go:29`
- `internal/meterview/view.go:70`

## f8

Standards are stored as immutable published revisions with category metadata and structured standards naming applicable roles. Existing BookLoad counts standards by role and category. Reuse can expose published book content, revision and load; whether a category filter changes the load totals remains open.

Evidence:

- `internal/store/standards.go:23`
- `internal/store/standards.go:49`
- `internal/store/standards.go:115`
- `internal/standards/render.go:73`

## f9

Remote MCP already has a 15-second tool timeout and 256 KiB serialized response ceiling. The overview uses bounded text and structured output with coverage warnings. New tools need budgets including both representations and explicit omissions; transport rejection alone is not a usable partial read.

Evidence:

- `internal/connector/transport.go:18`
- `internal/connector/mcp.go:170`
- `internal/connector/mcp.go:232`
- `internal/connector/overview_schema.go:3`
