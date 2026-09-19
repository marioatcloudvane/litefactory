# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## f1

Owner report: intermittent missing export records began sometime after an unidentified batch. No failing output was supplied. The affected job, record type, deployed version, expected counts and reproduction are unspecified. This is reported behavior, not independently observed loss. Source: issue #350 body reproduced in the supplied invocation (https://github.com/marioatcloudvane/litefactory/issues/350).

_No source reference was recorded for this finding._

## f2

At repository revision 7d5799834b5031c84286080708aaff6091ea63ee, a concrete candidate is meter export, which writes one registered GitHub project's usage measurements as CSV. Filters combine with AND and time bounds select entire attempts by start time, lower inclusive and upper exclusive. This does not establish that it is the reported job.

Evidence:

- `internal/cli/cmd_meter.go:39`
- `internal/store/meter_snapshot.go:264`

## f3

At the inspected revision, export materializes data in a transaction, reading source pages of 256 rows into a temporary snapshot. Later output pages advance by snapshot ID. Data collected after snapshot capture is intentionally absent from that export. Limits of one million rows, 256 MiB and 30 seconds preparation produce errors rather than a successful truncated snapshot.

Evidence:

- `internal/store/meter_snapshot.go:70`
- `internal/store/meter_snapshot.go:155`
- `internal/store/meter_snapshot.go:210`
- `internal/cli/cmd_meter.go:157`

## f4

At the inspected revision, CSV output streams to stdout after preparation. Cancellation, writer errors and short writes return errors; output already written is not rolled back. Inference: a wrapper retaining partial stdout while overlooking failure could present an incomplete file, but no wrapper or failing run was supplied to test this explanation.

Evidence:

- `internal/cli/meter_csv.go:71`

## f5

At the inspected revision, attempts or invocations without measurements can produce coverage rows with blank numeric fields. Missing values are not automatically missing records. Existing tests cover filter exclusions, coverage rows, frozen snapshots, size rejection and output failures. Tests were inspected, not executed; they do not reproduce this incident.

Evidence:

- `internal/store/meter_snapshot.go:199`
- `internal/cli/meter_csv.go:49`
- `internal/cli/meter_test.go:36`
- `internal/cli/meter_test.go:154`
- `internal/store/meter_test.go:41`
- `internal/store/meter_test.go:142`
