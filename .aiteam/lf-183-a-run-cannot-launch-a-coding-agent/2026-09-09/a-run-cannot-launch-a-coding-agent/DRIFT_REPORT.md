# Drift Report — a-run-cannot-launch-a-coding-agent

Branch: lf/183-a-run-cannot-launch-a-coding-agent | Date: 2026-09-09

## Delivered as planned

Partial US-1: durable decision, persistent context lookup and EXT-4 constraint, and identical evidence-review guidance in the three Claude authoring sources. No story is verified.

## Added scope

None.

## Dropped or deferred

- US-1 (#184): required Codex counterparts could not be edited under the run permission boundary. AC-2 and AC-5 fail; AC-3 is partial across planning paths. Independent scoped review is BLOCKING (CR-B-001). AC-1 and AC-4 pass retained-scope review.
- US-2 (#185): not implemented because the US-1 prerequisite is blocked. Capture/provenance procedure and its review remain obligations.
- US-3 (#186): not implemented because US-1 and US-2 prerequisites are unfinished. Recognition, reporting, troubleshooting and offline tests remain obligations.

The future capture and troubleshooting links in the decision point to planned files not created in this blocked run. No recovery claim for historical #73/#125 is made.

## Architectural deviations

Claude/Codex authoring guidance temporarily differs because the required `.codex/skills` edit was denied. No permission bypass, runtime permission change, forge action, real agent probe, commit or push was attempted.

## Verdict

DRIFTED — implementation is blocked by an immutable filesystem permission boundary before US-1 parity; all three approved stories remain unfinished. Resume the same approved scope in an environment where the required repository instruction copies can be edited normally. No scope reduction or replanning is assumed.

## Aggregate validation

`make check` passed (exit 0), including formatting, vet, pinned lint (0 issues), and the full race suite. Scoped documentation review remains BLOCKING; passing regression tests do not imply story acceptance.
