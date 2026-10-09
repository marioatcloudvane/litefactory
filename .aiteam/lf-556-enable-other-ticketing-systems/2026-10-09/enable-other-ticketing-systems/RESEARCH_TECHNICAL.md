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

No Linear or Jira code exists. Repo-wide search found nothing.

_No source reference was recorded for this finding._
