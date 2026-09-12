package report_test

import (
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/report"
)

// storyProgressDigest is a distinctive fixture digest: 64 lowercase hex
// characters that do not otherwise appear in any rendered prose, so a
// strings.Contains check for it is proof the digest reached the body, not a
// coincidence of the surrounding text.
const storyProgressDigest = "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"

// baseStoryProgressNotice returns a valid notice for kind, filling in an
// implementation digest only where the kind requires one.
func baseStoryProgressNotice(kind string) report.StoryProgressNotice {
	n := report.StoryProgressNotice{
		Parent:     testRef,
		Generation: 4,
		RunID:      17,
		EffectID:   9001,
		Sequence:   3,
		Story:      "US-3",
		Revision:   storyProgressDigest,
		Kind:       kind,
		Detail:     "evidence line one\nevidence line two",
	}
	if kind == "engineering_complete" || kind == "verified" {
		n.ImplementationDigest = storyProgressDigest
	}
	return n
}

func TestRenderStoryProgressBodyContent(t *testing.T) {
	rep := newReporter(t)

	kinds := []struct {
		kind  string
		state string
	}{
		{"started", "Admitted for launch"},
		{"engineering_complete", "Engineering and executed checks complete"},
		{"verified", "Story acceptance checks, scoped review and scoped tests passed"},
		{"invalidated", "Previous verification is invalidated"},
		{"blocked", "Story is unfinished or blocked"},
	}

	for _, tc := range kinds {
		t.Run(tc.kind, func(t *testing.T) {
			n := baseStoryProgressNotice(tc.kind)
			out, err := rep.RenderStoryProgress(t.Context(), n)
			if err != nil {
				t.Fatalf("RenderStoryProgress: %v", err)
			}
			body := out.String()

			for _, want := range []string{
				"Story " + n.Story,
				tc.state,
				"https://github.com/acme/acme-api/issues/123",
				"    evidence line one",
				"    evidence line two",
			} {
				if !strings.Contains(body, want) {
					t.Errorf("body missing %q\nbody:\n%s", want, body)
				}
			}

			for _, unwanted := range []string{
				"run 17",
				"generation 4",
				"event 3",
				storyProgressDigest,
			} {
				if strings.Contains(body, unwanted) {
					t.Errorf("body must not contain %q\nbody:\n%s", unwanted, body)
				}
			}

			if !strings.HasSuffix(body, n.Marker()) {
				t.Errorf("body does not end with the marker: %s", body)
			}
		})
	}
}

func TestRenderStoryProgressRequiresDigestForCompletionKinds(t *testing.T) {
	rep := newReporter(t)

	for _, kind := range []string{"engineering_complete", "verified"} {
		t.Run(kind, func(t *testing.T) {
			n := baseStoryProgressNotice(kind)
			n.ImplementationDigest = ""
			if _, err := rep.RenderStoryProgress(t.Context(), n); err == nil {
				t.Fatal("expected an error for a missing implementation digest")
			}

			n.ImplementationDigest = "not-a-valid-digest"
			if _, err := rep.RenderStoryProgress(t.Context(), n); err == nil {
				t.Fatal("expected an error for an invalid implementation digest")
			}
		})
	}
}

func TestRenderStoryProgressMarkerIsLastBytes(t *testing.T) {
	rep := newReporter(t)

	for _, kind := range []string{"started", "engineering_complete", "verified", "invalidated", "blocked"} {
		n := baseStoryProgressNotice(kind)
		out, err := rep.RenderStoryProgress(t.Context(), n)
		if err != nil {
			t.Fatalf("%s: RenderStoryProgress: %v", kind, err)
		}
		if !strings.HasSuffix(out.String(), n.Marker()) {
			t.Errorf("%s: body does not end with Marker()", kind)
		}
	}
}

func TestRenderStoryProgressEvidenceCap(t *testing.T) {
	rep := newReporter(t)

	n := baseStoryProgressNotice("blocked")
	n.Detail = strings.Repeat("a", 20000)
	if _, err := rep.RenderStoryProgress(t.Context(), n); err != nil {
		t.Fatalf("20000-byte detail should still be accepted: %v", err)
	}

	n.Detail = strings.Repeat("a", 20001)
	if _, err := rep.RenderStoryProgress(t.Context(), n); err == nil {
		t.Fatal("expected an error for a detail exceeding the 20000-byte cap")
	}
}
