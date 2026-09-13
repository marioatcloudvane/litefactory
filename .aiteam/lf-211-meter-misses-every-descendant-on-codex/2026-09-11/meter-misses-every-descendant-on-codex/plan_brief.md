# Plan: materially closer meter estimates

Feature: meter-misses-every-descendant-on-codex  
Branch: lf/211-meter-misses-every-descendant-on-codex  
Session date: 2026-09-11; Plan iteration: 2026-09-13 (run 0008 retry)  
Parent: https://github.com/marioatcloudvane/litefactory/issues/211  
Citation base: c5ebc85688e6348a4f54cd69071e9dc55296f892

## Shared Decisions

Improve estimates by collecting evidenced usage, never guessed multipliers. Preserve exact recorded-rate arithmetic, unknown versus zero, non-additive subsets, original attempt attribution and existing measured values. Provider-reported refusal zero remains separate from unknown token/model pricing.

Reuse provider adapters, neutral agent observations, pure meter reconciliation, store-only persistence, and existing CLI/report surfaces. No new service or package. Evidence inventories live in typed stories; authentic historical extracts and synthetic test envelopes have different proof limits. Passing fixtures do not establish present-day runtime or invoice accuracy.

## Workflow

US-001 and US-002 are independently useful; serialize edits where shared files overlap. US-003 depends on both to finish integrated output and operator documentation. The factory retains the persisted workflow version; no mode transition is automatic. Run and retain appropriate checks including make check during Implement; Plan executes no engineering checks. Actual live recovery is a separate explicit operator action after delivery.

## Cross-Story Constraints

All SQL stays in store with ctx-first APIs, a single SQLite connection and additive embedded migrations only if required. Prepare source evidence outside transactions. Preserve normal attempt lifecycle fences, bounded source access and safe path handling. Use isolated stores and sanitized fixtures; do not alter runtime state, dist binaries or protected instruction trees. No paid nested captures or relaxed permissions. Unknown coverage stays visible.

## Story Index

| ID | Title | Sub-issue |
| --- | --- | --- |
| US-001 | Recover missing descendant costs safely | [#260](https://github.com/marioatcloudvane/litefactory/issues/260) |
| US-002 | Explain proven zero-cost resume refusals | [#261](https://github.com/marioatcloudvane/litefactory/issues/261) |
| US-003 | Show useful partial estimates and document recovery | [#262](https://github.com/marioatcloudvane/litefactory/issues/262) |

## Out of Scope

Invoice reconciliation, fleet-wide accuracy guarantees, invented uplift factors, pricing redesign, unbounded late discovery, background repair jobs, restoring meter costs to run comments, actual live backfill during engineering, and certification of present-day nested-agent execution. Older issue acceptance is not closed by these bounded checks. Authentic present-day hierarchy evidence remains unavailable and unverified.

## Provenance

Research and sanitized runtime evidence are retained in this session. Architecture reviewed current source at the citation base above; old branch-divergence notes are historical. Implement must reconcile source changes against that base without reassigning historical consumption. The owner requested a simple complete scoped fix and useful closer estimate; no unresolved product decisions remain. Assumptions: explicit scoped preview/apply, tests against isolated state, and a later operator-controlled live repair.

Run 0008 independently revalidated retained requirements and architecture at the unchanged citation base; relevant source also matches the locally available origin/main reference, not a fresh remote fetch. The unchanged typed contribution passed current-binding preflight and was acknowledged with valid input and pending Plan state. The earlier receipt could not confirm completion. Published story identities/links and readiness remain unconfirmed until the factory finishes publication and save; the pending Story Index is intentional.
