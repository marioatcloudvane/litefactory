# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## f1

CLI team commands in docs do not exist. Code registers team install, clone, update, list, inspect, remove, link, unlink. Docs describe team add, show, validate, status, manage.

Evidence:

- `internal/cli/commands.go:13`
- `docs/cli/teams.md:9`
- `docs/cli/command-reference.md:60`

## f2

config.yaml reference lists settings.poll_interval_seconds (10..60); code key is poll_interval_minutes.

Evidence:

- `internal/config/config.go:105`
- `docs/configuration/config-yaml.md:13`

## f3

Settings keys in code with no docs: team_source, deploy_backup_keep, log_retain_days, archive_retain_days. Top-level blocks with no config reference: validate.auto_start, webhook.listen/public_url, chat.discord (enabled, application_id, public_key, token_keychain, token_env, owner_id, channel_ids, notice_channel_id), bug_checks.

Evidence:

- `internal/config/config.go:50`
- `internal/config/chat.go:17`
- `internal/config/webhook.go:15`

## f4

CLI commands in code absent from docs/cli: doctor (plus status, stop, apply), rights catalogue/list/grant/revoke/history, reviewer assign/remove/list, repo webhook. Docs list task clean/retire and story-contract upgrade; I did not verify these still exist in the router.

Evidence:

- `internal/cli/commands.go:33`
- `internal/cli/cmd_rights.go:41`
- `internal/cli/cmd_repo_webhook.go:27`

## f5

Validate owner commands /validate-check, -finding, -push, -merge, -close, -ship exist in code and are documented only in docs/workflow/validate.md; guides/validation-and-delivery.md and the issue command reference describe only /waive, /deliver, /merge and omit them. Other code-recognised commands undocumented in the guide include /approve-bug, /approve-plan, /approve-plan-set, /plan-new-story, /status, /retry, /reset, /doctor, /mcp-on, /mcp-off.

Evidence:

- `internal/turn/validate_commands.go:18`
- `docs/workflow/validate.md:132`
- `docs/guides/issue-command-reference.md:38`

## f6

README and glossary say five modes. Bug runtime is a separate graph workflow (analyst, /approve-bug analysis, one fixer, finalization). docs/workflow/bug-*.md are five three-line retired stubs plus bug-runtime.md.

Evidence:

- `docs/README.md:6`
- `docs/workflow/bug-runtime.md:1`
- `docs/workflow/bug-close.md:1`

## f7

docs/discord-chat.md is current in outline (config block, route /litefactory/discord, notice_channel_id, daemon status chat line, /help /health /status /diagnose /question /answer /ask /meter /prices). It lacks the developer-portal walkthrough, invite scopes/permissions and where to find IDs, and verification order of the Interactions Endpoint URL. docs/chat-feature-walkthrough.md covers /draft and /confirm, and states it was never run against live Discord.

Evidence:

- `docs/discord-chat.md:1`
- `docs/chat-feature-walkthrough.md:1`
- `internal/config/chat.go:17`

## f8

docs/features/github-webhooks.md documents webhook.public_url, repo webhook --all, listener default 127.0.0.1:8787, path /litefactory/webhook, reverse proxy options, daemon status checks. It is not linked from README or getting-started; github-setup.md has only token/Keychain and no webhook, labels, or App option. I did not find a GitHub App path in the docs; whether the code supports an App is unverified.

Evidence:

- `docs/features/github-webhooks.md:19`
- `internal/config/webhook.go:13`
- `docs/getting-started/github-setup.md:1`

## f9

docs/README.md links only getting-started, guides, cli, configuration, operations, glossary. discord-chat, chat-feature-walkthrough, bug-checks, stack-profiles, release-runbook, features/ and workflow/ are not linked from it. Several pages reference non-existent docs (../meter.md, ../story-contract.md, ../mcp-tool-families.md, ../workflow/owner-exceptions.md, mcp.md under guides) so link health needs a check.

Evidence:

- `docs/README.md:10`
- `docs/cli/command-reference.md:3`

## f10

Daemon deploy and pre-deploy DB backups: settings.deploy_backup_keep prunes backups in daily cleanup; docs/release-runbook.md covers only migration 0079 (Bug store consolidation) as an operator pre-ship check. No user guide on deploy/backup.

Evidence:

- `internal/config/template.go:66`
- `docs/release-runbook.md:1`
