package teamrelease

import "testing"

// The go child of base-team must build byte for byte the published
// go-development release, including GENERATED.md, team.config and the
// catalogue entry. The check also refuses any change to a published version.
func TestGoDevelopmentSourceMatchesPublishedRelease(t *testing.T) {
	if err := GenerateSources(t.Context(), "../..", true, []string{"teams/source/go-development"}); err != nil {
		t.Fatalf("drift against go-development v1.0.5: %v", err)
	}
}
