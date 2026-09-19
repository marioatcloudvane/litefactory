# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## f1

The only path that ever calls CreationAuthorizer.RetainProposal (which is what gives a proposal a hash and makes it retained) is PrepareCreationProposal, which only accepts a comment whose author is the configured owner and whose body literally starts with '/mcp-create prepare '. There is no other way to get a proposal hash into existence today.

Evidence:

- `internal/turn/publication_recovery.go:115`
- `internal/turn/mcp_creation.go:67`
- `internal/daemon/mcp_creation.go:33`

## f2

CreationPublicationService.Prepare does the same preparation (redaction, link markers, hashing, retention) but takes a Go value, not a parsed owner comment. It is documented as 'a factory-only presenter', is exercised by tests, but has zero callers anywhere in the daemon, MCP tool registration, or runtime wiring.

Evidence:

- `internal/turn/creation_publication.go:95`

## f3

The agent's Research-mode MCP toolset never includes anything that originates a proposal. The only creation-related tools are feedback_preflight (kind 'creation', which only re-validates envelope shape and never persists or hashes) and issues_create_batch, and issues_create_batch is added to the agent's tool list only when a live grant already exists, which itself requires an owner-approved intent, which itself requires a hash that already came from an owner-typed comment.

Evidence:

- `internal/turn/mcp_guidance.go:68`
- `internal/turn/feedback.go:84`
- `internal/turn/mcp_runtime.go:301`

## f4

The reply template shown when a proposal is retained reads as written for the factory presenting its own prepared work back to the owner for review ('Retained creation proposal... preparation grants no publication... To approve these exact bytes'), consistent with a factory-originated proposal rather than an owner-authored one.

Evidence:

- `internal/report/reply.go:737`

## f5

Only two behavioral differences separate the owner-comment prepare path from the unused agent-side prepare path: the owner path derives its nonce deterministically from the comment (hash of repo+comment id+body) and caps the rendered envelope at 48000 bytes before hashing; the agent-side path uses a random nonce and applies no cap. Everything downstream of prepare (RetainProposal, AdmitOwnerIntent, the 30-minute approval TTL, BindCreationGrant, issues_create_batch dispatch) is shared code already exercised by tests and needs no change to add an agent-facing prepare tool.

Evidence:

- `internal/turn/creation_publication.go:97`
- `internal/turn/publication_recovery.go:152`
- `internal/store/mcp_creation.go:16`

## f6

CreationArtifact, the type that becomes a created GitHub issue, carries only Title, Body, ParentIssue and LinkIssues. There is no field for a mode label, so an issue created through this path cannot start already labeled with a selected mode; that would need a separate mechanism if it's wanted.

Evidence:

- `internal/submission/creation.go:26`

## f7

No prior work toward this feature exists in the repository. Git history for the touched files shows only earlier, unrelated work (shared-communication-contract adoption, native-host integration); there is no in-progress branch or session artifact scoped to an agent-facing prepare tool.

Evidence:

- `internal/turn/creation_publication.go`
