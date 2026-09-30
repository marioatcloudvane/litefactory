# Story 003 host control engineering — run 0024

Frozen scope: story-003 / #434, revision `36627c9d83e9ce38219bd9db4574f85c58e431bdd6d0260b2835b49bfd81f8c2`, dependencies story-001 and story-002. This is engineering and executed fixture evidence, not acceptance or review.

Implemented private instance-bound local drain/restart/observe control on the existing authenticated submission broker. Two bounded control workers are independent of workflow admission; shutdown joins the broker before releasing the actual daemon singleton. Exact durable holds and singleton ownership precede target drain. Stale socket failure permits the down path only after acquisition of the actual lock, never PID inference.

Restart dispatch retains an immutable private marker before any external effect. Foreground/down uses the pinned same executable, fixed recovery arguments, detached session and inherited factory environment. The replacement waits a bounded interval on the actual singleton, checks its exact durable token and starts under repair holds. Observation requires a distinct ready authenticated instance and exact token; the service separately verifies repaired data before acknowledging recovery.

On macOS, installed-service routing verifies both the private plist and loaded launchctl program/arguments/factory home. A running service must match the already-proven singleton process. The exact token is synced before kickstart, and ordinary launchd startup reads it before singleton acquisition/recovery; the plist is never rewritten. A lost reply cannot authorize a second dispatch. Acknowledged token pointers remain historical. Unrecognized loaded-service output refuses the route. Route preflight checks the executable and installed identity before mutation. Unloaded installations use the pinned foreground/down route.

Production files owned/modified in this subtask:

- internal/config/doctor_host_paths.go
- internal/daemon/doctor_repair_host.go
- internal/daemon/doctor_repair_host_files.go
- internal/daemon/doctor_repair_host_restart.go
- internal/daemon/doctor_repair_host_launchd.go
- internal/daemon/doctor_repair_environment.go (approved stale-control fallback and route preflight)
- internal/daemon/daemon.go (host readiness/close)
- internal/daemon/setup.go (singleton recovery entry and configured native/host wiring)
- internal/cli/cmd_daemon.go (recovery arguments/token and host construction)
- internal/launchd/recovery_identity.go
- docs/features/doctor-host-control.md

Test files:

- internal/daemon/doctor_repair_host_test.go
- internal/daemon/doctor_repair_host_launchd_test.go
- internal/cli/cmd_daemon_recovery_test.go
- internal/launchd/recovery_identity_test.go

Tests use real temporary SQLite, owner-private directories, actual isolated Unix sockets and advisory locks, with fake replacement/launchctl callbacks. They cover readiness/foreign-operation refusal, exact drain, old-vs-new-instance observation, stale socket/live lock refusal, down exclusion, duplicate dispatch, lost launchctl replies with durable token carryover, foreign installation rejection, unsafe changed executable refusal and incomplete recovery arguments. No actual process start, kill, restart, launchctl mutation or provider invocation was executed. One existing CLI test reached read-only host status during preflight before the disabled native isolation gate; its failure exposed the gate ordering, which is being corrected by the main service owner.

Observed passing command before the final preflight-order fix:

`go test -race ./internal/daemon ./internal/cli ./internal/config ./internal/launchd -run 'Doctor|DaemonRun|DaemonStart|AcquireLock' -count=1`

```
ok github.com/cloudvane/litefactory/internal/daemon 5.667s
ok github.com/cloudvane/litefactory/internal/cli 3.012s
ok github.com/cloudvane/litefactory/internal/config 2.861s [no tests to run]
ok github.com/cloudvane/litefactory/internal/launchd 2.560s
```

Pinned scoped lint returned `0 issues.` before final preflight wiring. Subsequent check outcomes are appended below.

Authentic host process drain, launchd output/lifecycle, foreground/down restart and provider isolation remain unobserved. The native diagnosis and repair gates remain disabled. These fixtures do not establish authentic host capability; no acceptance claim is made. The parent owns aggregate checks, checkpoint submission and completion status.

## Final scoped execution

The main engineer moved the current provider/flagship/isolation check ahead of host preflight in both apply and capability admission. Closed adapters now refuse before host observation.

Final command:

`go test -race ./internal/daemon ./internal/cli ./internal/config ./internal/launchd -run 'Doctor|DaemonRun|DaemonStart|AcquireLock' -count=1`

```
ok github.com/cloudvane/litefactory/internal/daemon 5.785s
ok github.com/cloudvane/litefactory/internal/cli 3.043s
ok github.com/cloudvane/litefactory/internal/config 2.685s [no tests to run]
ok github.com/cloudvane/litefactory/internal/launchd 2.491s
```

Pinned scoped lint after host preflight wiring:

`GOLANGCI_LINT_CACHE=/private/tmp/litefactory-doctor-lint /Users/mariomh/.local/bin/golangci-lint run ./internal/daemon ./internal/cli ./internal/config ./internal/launchd`

```
0 issues.
```

The earlier CLI ordering failure is retained above as a resolved implementation defect, not relabeled as passing. No aggregate test, acceptance or authentic host-observation claim is made.
