# Implementation Notes — logs-f-issue-follows-only-the-run

## Code Review Advisories

None. The initial review raised two blocking findings, both resolved and re-reviewed cleanly.

## Test Environment

No integration-test lane was run: this repository has no integration tests or integration build tags. The required CLI coverage uses migrated temporary SQLite stores and real JSONL files in the unit suite.

## Bug Loop

The first repository-wide test run exposed unrelated macOS process-tree harness failures. Investigation found a sandbox process-enumeration restriction and a latent BSD `pgrep -P` portability defect. The test suite now explicitly skips only capability-unavailable process-tree checks and supplies BSD `pgrep`'s required pattern argument; the full race suite passes.
