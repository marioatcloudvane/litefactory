# Installation and upgrades

[Getting started](README.md) · [Documentation](../README.md)

Litefactory is a macOS application operated through a terminal and a per-user
LaunchAgent. Use a logged-in GUI user session for the daemon and Keychain access.
The repository does not establish a minimum macOS release here; Linux and Windows
service installation are not documented supported paths.

## Prerequisites

| Requirement | What to prepare |
| --- | --- |
| Go | Go 1.26 or later, matching `go.mod`, to build from source. |
| Git | Available on PATH, with a commit identity and authenticated access to fetch and push your project remote. |
| GitHub CLI (`gh`) | Installed and logged in to `github.com` as the owner; initialization uses it for identity discovery. |
| Coding agent | Install and authenticate Claude Code or Codex separately. Configure every enabled agent's executable; the first configured entry is the default. |
| GitHub accounts | Separate owner and bot identities; see [GitHub setup](github-setup.md). |

Check local tools before setup:

```sh
go version
git --version
gh --version
gh auth status
```

Agent installation and login belong to the selected provider. Litefactory does
not install agents or provide their subscription/API access. The generated
configuration enables Claude by default; adjust it before daemon startup if you
use Codex. See [coding agents](../configuration/coding-agents.md).

## Build a separate executable

Clone the source into a new directory, then build to a new, version-specific
location. Replace `REVISION` with the source revision you intend to install.

```sh
git clone https://github.com/marioatcloudvane/litefactory.git
cd litefactory
git checkout REVISION
mkdir -p "$HOME/.local/lib/litefactory/REVISION"
CGO_ENABLED=0 go build -trimpath -o "$HOME/.local/lib/litefactory/REVISION/litefactory" ./cmd/litefactory
export PATH="$HOME/.local/lib/litefactory/REVISION:$PATH"
litefactory --help
```

Use a fresh output directory for each build. Do not build over an executable
used by a running daemon. The help output confirms the executable starts; it
does not verify GitHub, credentials or agent execution. Keep the chosen directory
on PATH in the terminal used for setup, or use the absolute executable path.
This is a source-build recipe, not a package-manager or binary-release promise.

Continue with [GitHub setup](github-setup.md), then the
[first-run journey](README.md).

## Upgrade an existing installation

1. Read changes for the desired revision and build it into another new directory.
2. Let active work finish. Stop the LaunchAgent with the currently installed
   `litefactory daemon stop`; stop any foreground `daemon run` in its own terminal.
3. Select the new executable on PATH and run `litefactory init`. It refreshes
   identities, applies database migrations and preserves an existing `config.yaml`.
4. Run the new executable's `litefactory daemon start`, then
   `litefactory daemon status`. Starting again rewrites and reloads the plist
   with that executable and the current PATH.
5. Inspect [logs](../operations/running-the-daemon.md) before sending new work.

Changing config requires a daemon restart; there is no general hot reload.
Retaining an old binary does not guarantee database downgrade compatibility.
If a command fails, keep its diagnostic and use [troubleshooting](../operations/troubleshooting.md).
