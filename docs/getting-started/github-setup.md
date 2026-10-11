# GitHub setup

[Getting started](README.md) · [Documentation](../README.md)

Litefactory takes instructions from one **owner** and posts as a separate **bot**.
Initialization compares numeric GitHub user IDs, so renaming an account does not
change its authority. Using the same account for both is refused.

## Prepare identities and repository access

Log `gh` in to `github.com` as the owner before initialization. The owner needs
access to the target repository and permission to post the comments that direct
work. Litefactory discovers that identity from `gh`'s stored login; it strips
token environment overrides from that discovery process.

Create a separate bot account and credential with access to the selected
repository. The implemented API paths require the following operations; ensure
your token type and organization policy permit them:

| Use | Required access |
| --- | --- |
| Identity | Read the authenticated bot user. |
| Issue workflow | Read issues/comments; create comments and reactions; create/update issues, labels and sub-issue relationships. |
| Managed delivery | Read/create pull requests and merge the explicitly approved head when repository rules allow it. |
| Repository changes | Local Git credentials must independently permit fetch and push of task branches; merge operations also need appropriate repository write access. |

Use a repository-scoped token where available, with issue and pull-request write
permissions and the repository access required by the chosen operations. Account
membership, organization approval/SSO and branch protection can still restrict
it. A successfully resolved bot identity does **not** prove repository permissions
or merge eligibility. These operation requirements come from source, not an
observed token-permission matrix.

Git transport uses your local Git authentication. Storing a bot API token does
not configure Git push credentials. Configure Git's author identity separately.

## Initialize and store the token reference

```sh
litefactory init
```

This creates `$LF_HOME` (default `~/.litefactory`), a default `config.yaml` and the
database. It may warn that no bot token is stored; the rest of initialization can
still succeed. The default reference names a macOS Keychain item. Store its value
interactively, without putting the token in an issue or in YAML:

```sh
security add-generic-password -s litefactory -a github-bot -w
litefactory init
```

The corresponding generated configuration is a reference only:

```yaml
providers:
  github:
    token_keychain:
      service: "litefactory"
      account: "github-bot"
```

Rerunning `init` refreshes identities and preserves your existing configuration.
Its table should identify the intended owner and bot. If they match, initialization
refuses to store that pair. The daemon refuses to start without a resolved bot.
For an existing Keychain item, update it through Keychain Access rather than
creating a second conflicting entry.

An environment-variable reference is also supported; see
[permissions](../configuration/permissions.md). Keychain is preferable for the
LaunchAgent: using `token_env` with `daemon start` materializes its value in the
private plist and produces a warning. Never paste token values into documentation,
GitHub comments or troubleshooting reports.

## Expected feedback and failures

| Feedback | Action |
| --- | --- |
| Owner discovery fails | Check `gh auth status` and the stored `github.com` login. |
| Keychain item missing | Store the token under the exact service/account in `config.yaml`, then rerun `init`. |
| Keychain interaction unavailable | Unlock the login Keychain in your user session before starting again. |
| Bot lookup fails | Check the credential reference, token validity and network access. |
| Repository API operation is forbidden | Check that account, token and organization access allow that specific operation. |
| Git push fails | Inspect local Git authentication and branch rules independently of bot API access. |

Continue with [registration and your first Research request](README.md).
[Configuration](../configuration/README.md) owns the complete field reference;
[troubleshooting](../operations/troubleshooting.md) covers operational failures.

An isolated operator access check and disposable-repository Research observation
remain [unverified and unsatisfied](README.md#verification-status). This page does
not claim a successful live permission check.
