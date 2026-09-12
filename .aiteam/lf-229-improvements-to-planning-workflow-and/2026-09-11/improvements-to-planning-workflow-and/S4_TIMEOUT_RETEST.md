# Aggregate timeout diagnostic retries

The orchestrator requested these two bounded retries after the aggregate gate exceeded its eight-minute package timeout. Both existing tests passed individually. These diagnostic passes do not replace a full aggregate pass or erase its failures.

## Daemon retry

Command:

```sh
go test ./internal/daemon -run '^TestLegacyToolsLabelsAlwaysRefuseBeforeDispatch$' -race -count=1 -timeout=8m
```

Exit status: 0. Actual output:

```text
ok  	github.com/cloudvane/litefactory/internal/daemon	10.852s
```

## Turn retry

Command:

```sh
go test ./internal/turn -run '^TestPlanRejectsInvalidCurrentHandoff$' -race -count=1 -timeout=8m
```

Exit status: 0. Actual output:

```text
ok  	github.com/cloudvane/litefactory/internal/turn	42.739s
```

Both commands were started concurrently, using exact anchored selections. No implementation files or tests changed. Only this evidence report was written for the diagnostic task. No coverage percentage was collected for these exact requested commands.
