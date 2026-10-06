package report

import (
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/naming"
)

func TestImplementMarkersStartWithTheSelfFilterPrefix(t *testing.T) {
	ref := naming.Ref{Forge: "github", Owner: "acme", Repo: "api", Issue: 7}
	for _, m := range []string{StoryMergedMarker(ref, "story-002", "abc123"), ImplementHandoffMarker(ref)} {
		if !strings.HasPrefix(m, MarkerPrefix) || !strings.HasSuffix(m, " -->") || strings.Contains(m, "\n") {
			t.Errorf("malformed marker %q", m)
		}
	}
}

func TestStoryMergedMarkerRoundTripsAndIsBoundToStoryAndTask(t *testing.T) {
	ref := naming.Ref{Forge: "github", Owner: "acme", Repo: "api", Issue: 7}
	body := StoryMergedMarker(ref, "story-002", "abc123") + "\n\nMerged."
	if c, ok := ParseStoryMergedMarker(body, ref, "story-002"); !ok || c != "abc123" {
		t.Fatalf("parse = %q %v", c, ok)
	}
	if _, ok := ParseStoryMergedMarker(body, ref, "story-003"); ok {
		t.Error("another story matched")
	}
	other := ref
	other.Issue = 8
	if _, ok := ParseStoryMergedMarker(body, other, "story-002"); ok {
		t.Error("another task matched")
	}
	if _, ok := ParseStoryMergedMarker("> "+body, ref, "story-002"); ok {
		t.Error("a quoted marker matched")
	}
}

func TestMarkerTokenKeepsValuesInsideTheComment(t *testing.T) {
	ref := naming.Ref{Forge: "github", Owner: "acme", Repo: "api", Issue: 7}
	m := StoryMergedMarker(ref, "a --> <!-- lf:x", "c\nd")
	if strings.Count(m, "-->") != 1 || strings.Contains(m, "\n") {
		t.Errorf("value escaped the marker: %q", m)
	}
}
