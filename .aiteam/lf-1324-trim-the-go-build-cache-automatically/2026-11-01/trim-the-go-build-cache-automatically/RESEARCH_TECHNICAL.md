# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## F-01

Current default is 2 GiB, not approximately 16 GB. Settings.MinFreeDiskBytes shifts by 30; 0 disables the guard, and validation permits 0..10000. Explicit operator configuration still overrides the default. PR #1159 merged this default and status visibility on 2026-10-24.

Evidence:

- `internal/config/config.go:566`

## F-02

The daemon probes the LF_HOME volume before normal continuations, schedules, candidate and graph dispatch. Below threshold it defers work, logs free/required bytes, and records RunsHeld. Probe errors log and allow dispatch. This path has no cache trim.

Evidence:

- `internal/daemon/tick.go:316`

## F-03

Check launches independently enforce the same threshold against their worktree volume before launch. Setup passes the shared setting after hashing isolation; current admission and check probes can target different volumes.

Evidence:

- `internal/bugchecks/executor.go:376`

## F-04

In-flight sessions span dispatch through reporting, and Tick is serialized. The set explicitly protects only this process. Normal dispatch uses enterDoctorGuarded; judge children also enter the set. This is reusable idle evidence, not proof of exclusion against all independent cache users.

Evidence:

- `internal/daemon/inflight.go:9`

## F-05

Setup obtains a singleton lock for the factory home. Separate factory homes and external Go commands are outside that lock; safe shared-cache coordination cannot be inferred from the singleton alone.

Evidence:

- `internal/daemon/setup.go:344`

## F-06

Read-only Claude reviewers and Spike investigators explicitly receive GOCACHE under Paths.ReviewCacheDir()/go-build, a sibling of LF_HOME. Codex writable roots and Bug check writable roots are separately configured. The reported host Go cache is therefore not the only existing Go cache surface.

Evidence:

- `internal/harness/readonly_shell.go:46`

## F-07

Existing housekeeping is daily, runs at the end of ticks after dispatch, and registers backup/log work. It has no idle cache exclusion or pressure trigger. Its step interface is extensible but using it unchanged would not establish pre-admission trimming.

Evidence:

- `internal/daemon/housekeeping.go:11`

## F-08

RunsHeld already travels through the health snapshot to status, free-form answer facts and daemon health. Thus visibility exists on request; proactive incident alerts are a different gap. Open #1308 proposes Discord alerts, incident deduplication and all-clear behavior; existing workflow notices project task events, not generic disk incidents.

Evidence:

- `internal/store/daemon_health.go:51`

## F-09

Earlier #664 / merged PR #668 addressed repeated checkout cache growth with -trimpath and added disk guards. Makefile exports GOFLAGS += -trimpath and check recipes use it. Cache trimming would complement, rather than replace, that prevention.

Evidence:

- `Makefile:8`

## F-10

Impact: daemon tick/options/setup and configuration defaults/validation/template/docs are primary surfaces. Cache users include harness reviewers, Codex agents and isolated checks. Existing daemon disk tests, config disk tests and check-launch tests cover admission boundaries and provide extension points. No team role or skill change is established by this request.

Evidence:

- `internal/daemon/tick_test.go:1398`

## F-11

Compatibility: existing YAML lacks cache settings, so defaults must support omitted keys. No cache registry or cache-trimming config was found in inspected config, daemon, harness or Codex code. No required schema migration is established; durable alert incident state would depend on shaping and #1308. Existing run-stop configuration, -trimpath and backup/log retention should continue.

Evidence:

- `internal/config/disk_settings_unit_test.go:9`

## F-12

Context checked: related open/closed issues and PRs include #1308, #664/#668 and #1156/#1159; recent affected-file history was inspected. No matching Research/Plan briefs for those issue numbers were found under .aiteam. Repository check recipe requires trimmed build/test/vet and lint; no root AGENTS.md was found. This pass ran no build or tests and made no source changes.

Evidence:

- `.litefactory/bug-checks.yaml:1`

## F-13

Shaping confirms that general Codex cache writable roots are intentionally operator configured, while the reviewer runner creates its own Go cache and supplies GOCACHE; therefore dedicated factory caching changes launch environments and explicit cache permissions, not just maintenance configuration.

Evidence:

- `internal/config/config.go:216`
- `internal/harness/readonly_shell.go:46`

## F-14

The daemon disk probe fails open, but isolated check launches fail on disk-probe errors; preserving each existing admission behavior requires treating cleanup authorization separately from dispatch authorization.

Evidence:

- `internal/daemon/tick.go:3472`
- `internal/bugchecks/executor.go:376`

## F-15

Impact extension: the selected dedicated-cache scope touches provider/check environment assembly and cache-only sandbox grants; status reporting can extend existing health snapshots without a new command or external API; the host cache is left untouched and owner answer A-01 excludes alert integration.

Evidence:

- `internal/harness/readonly_shell.go:84`
- `internal/daemon/setup.go:606`
- `internal/store/daemon_health.go:51`
