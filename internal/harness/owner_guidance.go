package harness

import "fmt"

// OwnerGuidanceSection is persistent owner direction, separate from this run's
// one-time instruction and historical notes. Positive empty revisions explicitly
// withdraw earlier direction when a provider conversation is resumed.
func OwnerGuidanceSection(revision int64, body string) string {
	if revision == 0 {
		return ""
	}
	if body == "" {
		return fmt.Sprintf("\n## Standing owner guidance (revision %d)\n\n(cleared)\n", revision)
	}
	return fmt.Sprintf("\n## Standing owner guidance (revision %d)\n\n%s\n", revision, body)
}
