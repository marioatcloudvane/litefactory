package teamrelease

import (
	"regexp"
	"testing"
)

var standardsGrant = regexp.MustCompile(` lf_standards="([a-z-]+)",`)

// withoutStandardsGrant removes the optional standards book grant that editable
// sources declare for the story worker, the technical planner and the
// architecture reviewer, so they can still be compared with releases cut before
// it. Any other grant is a failure rather than something to normalize away.
func withoutStandardsGrant(t *testing.T, raw []byte) []byte {
	t.Helper()
	matches := standardsGrant.FindAllSubmatch(raw, -1)
	if len(matches) > 1 {
		t.Fatalf("graph declares %d standards grants", len(matches))
	}
	for _, match := range matches {
		switch string(match[1]) {
		case "story-worker", "plan-technical", "validate-architecture":
		default:
			t.Fatalf("unexpected standards grant for %s", match[1])
		}
	}
	return standardsGrant.ReplaceAll(raw, nil)
}
