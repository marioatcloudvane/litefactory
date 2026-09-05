// Package team decodes and validates the agent team index: the file that
// says which agents exist, which team(s) they belong to, and where each
// agent's definition comes from and goes to on disk.
//
// # Shape
//
// The index is one JSON document with two top-level sections:
//
//   - teams: an array of team entries. Each team has a name (used as its
//     slug in paths and in error messages) and an inline agents map, keyed
//     by agent name, holding that team's own agent definitions.
//   - shared: a top-level map, same key/value shape as a team's agents map,
//     holding agent definitions available to every team rather than owned
//     by one.
//
// Every agent entry — whether inline under a team or top-level under shared
// — is a {source, dest} pair: source is where the agent's definition is
// read from, dest is where it is materialized for a harness to consume.
//
// # Decoding is strict, in two passes
//
// json.Decoder.DisallowUnknownFields cannot see through a map[string]X: an
// unrecognized key inside teams[i].agents or inside shared decodes cleanly
// as a valid map entry, and a struct-level typo it does catch is reported
// without a usable path. Neither failure mode is acceptable for a config
// file a person hand-edits, so decoding here runs in two passes instead:
//
//  1. A generic walk over map[string]json.RawMessage / any, checked against
//     the known-key table at every level, producing one dotted-path error
//     per unknown field — for example shared.go-architect.souce or
//     teams.go-team.agents.go-architect.souce. The team segment of that path
//     is the team's name, not its array index, because the name is what a
//     person editing the file can find. Every fault this pass finds is
//     collected and returned together via errors.Join, in a stable sorted
//     order, rather than stopping at the first one.
//  2. Once pass one is clean, a typed json.Unmarshal into Index and its
//     nested structs. This pass cannot itself see an unknown field — pass
//     one already guaranteed there isn't one.
//
// # Validation rules
//
//   - Every agent key (in a team's inline agents map, or in shared) must
//     match the filename-safety pattern, and so must every team's name.
//   - Source and Dest must both be non-empty.
//   - An agent key must appear in exactly one place across the whole index:
//     one team's agents map, or shared. The same key inline in two teams, or
//     inline in a team and also in shared, is a load-time error naming the
//     key and every location it was found at (not just the second one).
//
// All validation faults are aggregated the same way the decode faults are:
// collected, sorted for a stable order, and returned together via
// errors.Join, so a person fixing the file sees every problem in one pass
// rather than one per run.
//
// # Invariants
//
// The only litefactory package this package may ever import is
// internal/config, and only for two specific exports: config.ClassifySecret
// (present today) and config.ContainsForbiddenFlag (added by a later story,
// for validating that a Source/Dest does not smuggle in a forbidden CLI
// flag). Nothing in this story's own validation rules needs either one yet,
// so neither is imported here — see the TODO next to Validate.
package team
