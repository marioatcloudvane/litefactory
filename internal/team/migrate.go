package team

import (
	"encoding/json"
	"errors"
	"fmt"
)

// CurrentSchemaVersion is the schemaVersion this binary's Index type shape
// corresponds to. Every index document loaded through [Migrate] must
// declare a schemaVersion no greater than this one; anything higher is a
// hard load failure (see [SchemaVersionTooNewError]), never a silent
// downgrade or a best-effort partial read.
//
// Schema v1 — a top-level teams array plus a top-level shared map, each
// agent entry a {source, dest} pair — is the only version that has ever
// existed, so this is the only value CurrentSchemaVersion has ever held.
const CurrentSchemaVersion = 1

// migrationStep transforms the raw top-level shape of an index document
// written at one schemaVersion into the shape the next schemaVersion
// expects. Steps operate on the generic map[string]json.RawMessage
// representation of the document rather than a typed struct, because a
// migration step's whole reason to exist is that the shape changed — a
// typed struct for an old version would have to be kept around forever
// purely to migrate away from it.
type migrationStep func(map[string]json.RawMessage) (map[string]json.RawMessage, error)

// migrationSteps is the registry of sequential migration steps, keyed by
// the schemaVersion a step migrates *from* (a step registered under key N
// produces the N+1 shape). Schema v1 is the only version that has ever
// existed, so this registry is empty today.
//
// When a v2 shape is introduced, register its step here under key 1 (v1 ->
// v2); when v3 arrives, add key 2 (v2 -> v3), and so on. [Migrate] walks
// this chain one step at a time from whatever schemaVersion it reads up to
// CurrentSchemaVersion, so a document several versions behind still
// migrates correctly without every intermediate step being hand-written
// into Migrate itself.
var migrationSteps = map[int]migrationStep{}

// ErrSchemaVersionTooNew reports an index document whose schemaVersion is
// newer than this binary's CurrentSchemaVersion — for example a file
// written by a newer litefactory and then read by an older one that has
// not been upgraded yet. There is no forward migration path, so this is
// always a hard failure rather than a silent downgrade or a best-effort
// read.
var ErrSchemaVersionTooNew = errors.New("team index schema version is newer than supported")

// SchemaVersionTooNewError reports the specific mismatch: the schemaVersion
// found in the document and the highest version this binary supports.
type SchemaVersionTooNewError struct {
	Found     int
	Supported int
}

func (e *SchemaVersionTooNewError) Error() string {
	return fmt.Sprintf("index schemaVersion %d is newer than this binary supports (max %d); upgrade litefactory to load this index", e.Found, e.Supported)
}

func (e *SchemaVersionTooNewError) Unwrap() error { return ErrSchemaVersionTooNew }

// Migrate is the entry point for loading an on-disk team index. It reads
// the document's schemaVersion, applies every sequential migration step
// needed to bring its raw shape up to CurrentSchemaVersion, and then
// decodes and validates the result exactly as [Decode] does for an
// already-current document.
//
// schemaVersion is a migration concern only — it is stripped out of the
// document before the typed decode, so the returned Index carries no
// schema-version field of its own, and [Decode]'s own unknown-field check
// is unaffected by it.
//
// A schemaVersion newer than CurrentSchemaVersion is a hard failure
// (*SchemaVersionTooNewError), never a silent downgrade or a best-effort
// partial read. A document already at CurrentSchemaVersion runs the loop
// zero times and decodes unchanged.
func Migrate(data []byte) (*Index, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		return nil, fmt.Errorf("parse team index: %w", err)
	}

	version, err := readSchemaVersion(top)
	if err != nil {
		return nil, err
	}
	if version > CurrentSchemaVersion {
		return nil, &SchemaVersionTooNewError{Found: version, Supported: CurrentSchemaVersion}
	}
	delete(top, "schemaVersion")

	for version < CurrentSchemaVersion {
		step, ok := migrationSteps[version]
		if !ok {
			return nil, fmt.Errorf("no migration registered from schema version %d to %d", version, version+1)
		}
		top, err = step(top)
		if err != nil {
			return nil, fmt.Errorf("migrate schema version %d to %d: %w", version, version+1, err)
		}
		version++
	}

	body, err := json.Marshal(top)
	if err != nil {
		return nil, fmt.Errorf("re-marshal migrated team index: %w", err)
	}
	return Decode(body)
}

// readSchemaVersion reads and validates the schemaVersion field off the raw
// top-level document. It must be present and a positive integer — an index
// document is meaningless without knowing which shape it was written
// under, so a missing or malformed schemaVersion fails to load rather than
// silently defaulting to one.
func readSchemaVersion(top map[string]json.RawMessage) (int, error) {
	raw, ok := top["schemaVersion"]
	if !ok {
		return 0, errors.New("team index is missing required schemaVersion field")
	}
	var version int
	if err := json.Unmarshal(raw, &version); err != nil {
		return 0, fmt.Errorf("schemaVersion: %w", err)
	}
	if version < 1 {
		return 0, fmt.Errorf("schemaVersion must be a positive integer, got %d", version)
	}
	return version, nil
}
