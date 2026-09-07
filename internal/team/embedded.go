package team

import (
	"embed"
	"fmt"
	"io/fs"
)

// The distribution is installed explicitly into the central catalog. Runtime
// resolution never uses these bytes as fallback for a missing installation.
//
//go:embed embedded
var embeddedFS embed.FS

const embeddedTeamsDir = "embedded/teams"
const embeddedIndexPath = embeddedTeamsDir + "/index.json"

// Embedded validates the shipped catalog for explicit installation.
func Embedded() (*Index, error) {
	data, err := embeddedFS.ReadFile(embeddedIndexPath)
	if err != nil {
		return nil, fmt.Errorf("read embedded team index: %w", err)
	}
	idx, err := Decode(data)
	if err != nil {
		return nil, fmt.Errorf("load embedded team index: %w", err)
	}
	return idx, nil
}

// EmbeddedFS is rooted at the installable catalog including skills/resources.
func EmbeddedFS() (fs.FS, error) {
	sub, err := fs.Sub(embeddedFS, embeddedTeamsDir)
	if err != nil {
		return nil, fmt.Errorf("root embedded team filesystem: %w", err)
	}
	return sub, nil
}
