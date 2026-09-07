// Package team owns the version 2 central team registry and safe management
// writes. Index.Teams maps team names to descriptions and five required ordered
// mode arrays. Each roster entry either references Index.Shared or supplies an
// inline id, description, source path, capability tier, and required skill IDs.
// Empty arrays mean root-only operation; shared agents are never implicit.
//
// Decode rejects unsupported versions, duplicate JSON keys, unknown fields,
// missing mode arrays, unresolved refs, and duplicate effective roster IDs.
// Validation aggregates faults in stable order. There is no schema migration.
//
// WithCatalogLock serializes central catalog mutation and source capture.
// Add remains the safe renderer write engine: validate the full batch, refuse
// symlink paths and unowned destinations, write via temporary file and rename,
// then save provenance last. Renderers live above this package; team never
// imports render or teamsource. Classify compares caller-supplied fresh bytes
// with recorded provenance without reading source files or changing them.
package team
