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
// # The write engine: Add
//
// Add takes already-rendered artifacts (AgentRender, holding one or more
// RenderedFile per agent/harness pair) and installs them into a target
// repo. This package never renders anything itself and never imports
// internal/render — a caller (internal/teamsource) renders first, converts
// render.RenderedFile into this package's own RenderedFile, and calls Add.
//
// Every artifact from every AgentRender is validated up front — harness
// known, path an Owned Path or a recognized root-file target, no literal
// secret per config.ClassifySecret — before any file is written. Every
// problem found across the whole batch is collected as validation proceeds
// and returned together via errors.Join, rather than stopping at the first
// one, so a caller with several bad renders in one call sees every problem
// at once. A validation failure anywhere in the batch still aborts the
// whole call with nothing written: writing only ever starts once the full
// batch has validated cleanly. Once validation passes, files are written
// (standalone files, then root-file sections), and the provenance lockfile
// is saved last, so a failure or process death partway through writing
// never leaves the lockfile claiming an artifact exists that was never
// actually written.
//
// Concurrent Add calls against the same repo serialize on a per-repo flock
// held for the duration of the call (lock.go) rather than racing or failing
// fast, since two invocations installing overlapping agents are expected to
// happen — for example, two team members running the same command at
// close to the same time.
//
// # The provenance lockfile
//
// .litefactory/team.lock.json (lockfile.go) records one LockRecord per
// installed artifact: which agent and harness produced it, where its source
// was read from and its hash at render time (SourcePath, SourceHash), where
// it was written (DestPath), and a hash of what was actually written there
// (RenderedHash). A record's key is its (Harness, AgentKey, DestPath)
// triple: a standalone file and a root-file section for the same agent are
// two different records, since they have two different DestPaths.
//
// Add's no-op short-circuit compares a freshly rendered artifact's would-be
// record against the existing record occupying the same key: if both
// SourceHash and RenderedHash match, nothing is written and the existing
// record is reported back as skipped rather than rewritten. This is
// deliberately narrower than a full diff of on-disk content — it answers
// "would this render produce something different from what is already
// recorded", not "does the file on disk still match the record" (drift
// detection, which ErrDrifted names, is a separate concern for another
// story to wire up against this same lockfile).
//
// # Root-file composition and marker-span hashing
//
// Some artifacts are not a whole file but a section of a shared root file
// (RootFileClaude = CLAUDE.md, RootFileOpencode = AGENTS.md) — marker.go's
// mechanics. Each agent's section is delimited by:
//
//	<!-- litefactory:team BEGIN team=<team[,team...]> agent=<agent-key> -->
//	...body...
//	<!-- litefactory:team END -->
//
// upsertMarkerSection locates an existing section by agent key alone (the
// team list can change across re-adds without changing which section it
// is), replacing it in place, or appends a new section separated by exactly
// one blank line if none exists yet. An agent shared by more than one team
// gets exactly one section, whose BEGIN line lists every contributing team,
// sorted and comma-separated — the caller (internal/teamsource) is
// responsible for collecting every contributing team into one AgentRender
// before calling Add, so this package never has to merge two calls' worth
// of contributions itself.
//
// Because a root file is shared with content this package did not write,
// RenderedHash for a root-file record is not a hash of the whole file: it
// is markerSpanHash, the hash of just the marker-enclosed span (BEGIN line,
// body, END line) that would be produced for that agent — a "virtual file"
// hash. Hashing the BEGIN line along with the body means a change in the
// set of contributing teams alone, with no change to body content, still
// invalidates a previously recorded RenderedHash, so the no-op
// short-circuit never leaves a stale team list sitting in a root file.
//
// # Invariants
//
// The only litefactory package this package may ever import is
// internal/config, and only for two specific exports: config.ClassifySecret
// and config.ContainsForbiddenFlag. Validate uses ContainsForbiddenFlag
// against each agent entry's Source and Dest; Add uses both ClassifySecret
// and ContainsForbiddenFlag against every rendered file's bytes,
// post-render, before any write lands (US-14).
package team
