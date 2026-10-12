# Using the CLI

Use the terminal to register repositories, manage teams, run the daemon and look at logs and costs. Start with [Getting started](../getting-started/README.md) for installation and the first `litefactory init`.

| Topic | Use it for |
| --- | --- |
| [Repositories](repositories.md) | Register a clone and choose its base branch, branch prefix and worktree folder. |
| [Teams](teams.md) | Install, copy, link and remove team packages of agent instructions. |
| [Command reference](command-reference.md) | Every command, argument, flag, default and exit code. |
| [Usage and costs](../meter.md) | What `litefactory meter` shows and the CSV export. |

## Terminal conventions

Examples assume `litefactory` is on your `PATH`. Replace placeholders such as `OWNER/REPO`, `NAME` and `/absolute/path`, and leave out the angle brackets and square brackets you see in syntax lines. Quote paths with spaces and task references that contain `#`.

```sh
litefactory --help
litefactory --version
litefactory repo add --help
```

- Help prints the syntax and flags and does not run the command. `litefactory <group>` (for example `litefactory team`) lists a group's subcommands.
- Flags can be written `--flag` or `-flag`, and positional arguments can come before or after them.
- Settings and state live in `$LF_HOME`, which defaults to `~/.litefactory`. There is no general `--config` flag.
- Exit codes: `0` success, `1` the command failed, `2` the command line was wrong, `3` the machine is not ready (for example not initialized). See the [command reference](command-reference.md#exit-codes).
- Read stderr even after success. For example, `init` only warns when it cannot work out the bot identity.

If a command is missing or behaves differently, compare `litefactory --version` and the command's `--help` with the [command reference](command-reference.md). For a missing setup or an invalid `config.yaml`, see [troubleshooting](../operations/troubleshooting.md). Comments you write on a GitHub issue, such as `/implement`, are in the [owner commands](../guides/owner-commands.md). Settings in `config.yaml` are in [config.yaml](../configuration/config-yaml.md).

The `submission` and `mcp serve` commands are run by the factory for a running agent. They are not part of normal use; they are listed at the end of the [command reference](command-reference.md#factory-internal-commands-not-for-everyday-use).

[Documentation index](../README.md)
