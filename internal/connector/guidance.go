package connector

import (
	"fmt"
	"io/fs"
	"strings"
)

// Guidance is read from the factory library's connector directory
// (teams/source/factory/connector), whose files sit under guidance/.

// loadGuidance accepts only the stable installed tool identifiers. Bounding each
// resource prevents initialization from becoming an unbounded evidence channel.
func loadGuidance(files fs.FS, names []string) (string, error) {
	parts := make([]string, 0, len(names))
	for _, name := range names {
		if !isKnownTool(name) {
			return "", fmt.Errorf("unsupported guidance capability")
		}
		b, err := fs.ReadFile(files, "guidance/"+name+".md")
		if err != nil || len(strings.TrimSpace(string(b))) == 0 || len(b) > 8192 {
			return "", fmt.Errorf("capability guidance unavailable")
		}
		parts = append(parts, strings.TrimSpace(string(b)))
	}
	return strings.Join(parts, "\n\n"), nil
}
