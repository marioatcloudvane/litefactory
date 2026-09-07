// Package teamsource resolves exactly one team index source per invocation,
// in a fixed precedence order — embedded default < static-path override <
// registered-repo override (US-2) — and loads that source's index plus its
// markdown tree. Exactly one tier is ever consulted; there is no
// field-by-field merge between tiers, and a lower-precedence tier is never
// even read once a higher-precedence one applies.
//
// This package sits above internal/team, which owns the index document's
// shape, decoding and validation. internal/team's own doc.go restricts it to
// importing at most internal/config, which is why this source-selection
// logic — needing internal/repos and internal/store for the registered-repo
// tier — lives here instead of inside internal/team itself.
//
// Every tier loads its index document through team.Migrate, never
// team.Decode directly: Migrate reads and strips the top-level schemaVersion
// field before decoding, and team.Decode's own known-key table has no entry
// for schemaVersion (it only recognizes teams/shared at the top level). An
// index.json produced by any tier is read the same way: as
// <root>/teams/index.json, with its markdown tree rooted at <root>/teams/.
package teamsource
