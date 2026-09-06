package team

import (
	"crypto/sha256"
	"encoding/hex"
)

// HashBytes returns a hex-encoded sha256 of b. Used for both a rendered
// standalone file's RenderedHash and, for a root-file target, the
// marker-enclosed span's "virtual file" hash (see marker.go's markerSpan).
// Exported so internal/teamsource can hash an agent's raw source bytes for
// AgentRender.SourceHash without duplicating this implementation.
func HashBytes(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
