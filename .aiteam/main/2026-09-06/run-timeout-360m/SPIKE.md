# Spike — run-timeout-360m

2026-09-06. Raise the hard wall-clock limit after which a litefactory run is
killed to 360 minutes (6 hours). An earlier bump the same day had taken it to
180 minutes, which turned out not to be enough.

## What was done

- Backed up `~/.litefactory/config.yaml` to
  `config.yaml.bak-timeout360-<timestamp>` before touching it.
- Changed `agents.claude.timeout_seconds` from `10800` (180 min) to `21600`
  (360 min) — line 93 of the config.
- Re-parsed the file to confirm it is still valid YAML and reads back as
  `21600` (360 min).

No Go source was changed. The kill is driven by the config value, not by the
compiled default.

## What was found

- The value that kills a run is `agents.claude.timeout_seconds` in
  `$LF_HOME/config.yaml`. `internal/config/config.go:262` documents it as "the
  hard wall-clock kill for one run".
- Bounds are `MinClaudeTimeoutSeconds = 60` and `MaxClaudeTimeoutSeconds =
  86400` (`internal/config/validate.go:35-36`), so 21600 validates cleanly with
  plenty of headroom — 24 h is the ceiling.
- The compiled defaults `DefaultClaudeTimeoutSeconds` and
  `DefaultCodexTimeoutSeconds` are both still `1800` (30 min)
  (`internal/config/config.go:449,452`). They only apply to a config that omits
  the key, so they are inert here and were deliberately left alone.
- `agents.codex.timeout_seconds` is a **separate** limit, currently `5400`
  (90 min). Codex runs are still killed at 90 minutes.
- There is no config hot-reload anywhere in the tree — no SIGHUP handler, no
  file watcher. The daemon reads config at start, so the new value takes effect
  only after a daemon restart.

## Decisions worth remembering

- Configure the timeout, don't recompile it. The YAML value wins over the
  compiled default, so changing `DefaultClaudeTimeoutSeconds` would have had no
  effect on this machine and would have shifted the number for every future
  `litefactory init` — a wider blast radius than asked for.
- Only the Claude limit was moved. "The limit after which a run is killed"
  matched the value that had already been bumped once today; Codex was left at
  90 minutes rather than silently widened too.

## Not done

- **The daemon was not restarted.** `dist/litefactory daemon run` (pid 4808) is
  live and had work in flight at the time of the change (a test sweep in
  `~/.lf-worktrees/litefactory/task-68`). Restarting would have killed those
  in-flight runs, which is not a spike's call to make. Until it is restarted,
  runs are still killed at 180 minutes.
- `agents.codex.timeout_seconds` left at 5400.
- The compiled 30-minute defaults left at 1800.

## Recommendation

Restart the daemon when the current runs have drained — `litefactory daemon
stop` then `litefactory daemon start` — to pick the new value up. Nothing else
here needs a mode; if the 30-minute compiled defaults are actually wrong for
new installs, that is a small separate change worth its own request.
