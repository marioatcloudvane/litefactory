# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## F-1

Ticket access is GitHub-coupled across the code base. About 90 non-test Go files in internal/daemon, internal/turn and internal/cli import internal/github and use github.Repo, github.Issue and github.Comment. The github client has 38 methods covering issues, comments, sub-issues, delivery, hooks and polling cache. Source: internal/github/*.go and its importers.

_No source reference was recorded for this finding._

## F-2

A partial ticket interface exists but uses GitHub types. daemon.Forge (ListIssues, ListCommentedIssues, GetIssue, ListCommentsAfter, AddCommentReaction) and labelForge take github.Repo and return github.Issue/Comment. Source: internal/daemon/daemon.go:92-105.

_No source reference was recorded for this finding._

## F-3

Projects are already stored in the database. store.Repo (repos table) holds path, forge, owner, name, remote_url, base_branch, feature_prefix, worktree_root. Forge is a column but only GitHub is known. There is no ticket-system field. Source: internal/store/repos.go:12-40.

_No source reference was recorded for this finding._

## F-4

config.yaml holds global settings and the GitHub provider: settings, validate, webhook, providers.github (api_base, token_keychain, token_env), tiers, agents, tool_profiles, bug_checks. No project list is in it. Source: internal/config/config.go:48-96, 276-290.

_No source reference was recorded for this finding._

## F-5

The issue model is GitHub-shaped. github.Issue carries APIURL, a database ID distinct from Number (sub-issue API needs both), title and body. Sub-issues, labels and reactions are GitHub concepts the workflow relies on. Source: internal/github/issues.go:25-50, subissues.go.

_No source reference was recorded for this finding._

## F-6

The earlier repository search located no Linear or Jira integration; this is a bounded search observation rather than proof that no related code exists.

_No source reference was recorded for this finding._

## F-7

Existing ticket operations include PostComment, EditComment, CreateIssue, UpdateIssue, SetStoryState, RetirePlanStory and CloseDeliveredParent; the earlier interface list omitted them. Evidence: internal/github/comments.go, issues.go, managed_issues.go, story_state.go, plan_retirement.go and delivery.go.

_No source reference was recorded for this finding._

## F-8

Sessions identify tickets by repo_id plus integer issue_number and build task references from code forge/owner/repository; owner replies compare numeric GitHub author IDs. Evidence: internal/store/sessions.go and internal/daemon/research_owner_resume.go.

_No source reference was recorded for this finding._

## F-9

Webhook configuration includes listen and public_url, hook records are per repository, and polling is validated within 1–180 minutes. Evidence: internal/config/webhook.go, internal/store/repo_webhooks.go and internal/config/validate.go.

_No source reference was recorded for this finding._

## F-10

Linear documentation describes team-specific statuses, a reserved Duplicate status and additional Triage; the prior five-type explanation was inaccurate. Source: https://linear.app/docs/configuring-workflows

_No source reference was recorded for this finding._

## F-11

Linear sub-issues support parent relationships, do not inherit labels, and team-level automation can automatically close parents or children. Source: https://linear.app/docs/parent-and-sub-issues

_No source reference was recorded for this finding._

## F-12

Linear webhooks support issues/comments and workspace or team scope, require admin management rights and HMAC raw-body signature/timestamp verification; API keys can be restricted by permission and team. Sources: https://linear.app/developers/webhooks and https://linear.app/docs/api-and-webhooks

_No source reference was recorded for this finding._

## F-13

Linear has a public GraphQL endpoint and API-key authentication; no authenticated workspace check or live mutation was performed. Source: https://linear.app/developers/graphql

_No source reference was recorded for this finding._

## F-14

Existing factory identities distinguish the authenticated human OwnerUserID from BotUserID, use handles for display and reject owner/bot identity equality. Evidence: internal/turn/classify.go:102-126; owner replies are admitted by numeric actor-ID comparison in internal/daemon/research_owner_resume.go.

_No source reference was recorded for this finding._
