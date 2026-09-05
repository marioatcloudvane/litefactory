# Implementation Notes — App-level log view (issue #89)

## Assumptions / scheduling decision

The plan brief's Workflow section gives a strict shipping order with hard file-level
couplings (US-1/US-2/US-5/US-7/US-9 all read/write overlapping files: `cmd_logs.go`,
`cmd_logs_app.go`, `cmd_logs_render.go`, `logsOptions.bind`, `router_test.go`,
`cmd_logs_test.go`'s `logsSourceFiles`). Running all 9 story engineers concurrently
against the same worktree would produce conflicting concurrent edits to those shared
files. Resolved by scheduling engineers in dependency order instead of one giant
parallel batch, per the plan's own Workflow/dependency graph, and only parallelizing
truly independent tracks:

- Batch 1 (parallel): US-3 (renderer, no deps) + US-6 (independent track, daemon-run/start files only, no overlap with cmd_logs*.go).
- Batch 2: US-9 (tail-by-default; depends on US-3).
- Batch 3: US-1 + US-4 together, single engineer invocation (plan brief: "ship as one atomic change"; also touch the same file `cmd_logs.go`).
- Batch 4: US-2 (follow; depends on US-1's `cmd_logs_app.go` per its own architecture notes, plus US-3/US-9).
- Batch 5: US-5 (level filter; depends on US-3, and binds flags in the same `logsOptions.bind` touched by US-1/US-9).
- Batch 6: US-7 (grep/-i/--invert; depends on US-3, US-5).
- Batch 7: US-8 (stderr advisory; depends on US-5, US-2).

This is the most reasonable reading of the plan's explicit Workflow section and is
recorded here per the Implement mode's "ordinary uncertainty" handling.
