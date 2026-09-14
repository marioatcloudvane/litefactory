package submission_test

import (
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/submission"
)

func TestCanonicalCreation_FamilyByteAndRelationshipBounds(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		change func(*submission.CreationEnvelope)
		valid  bool
	}{
		{"minimum content", func(e *submission.CreationEnvelope) { e.Artifacts[0].Title = "x"; e.Artifacts[0].Body = "y" }, true},
		{"multibyte title at byte limit", func(e *submission.CreationEnvelope) { e.Artifacts[0].Title = strings.Repeat("é", 128) }, true},
		{"multibyte title over byte limit", func(e *submission.CreationEnvelope) { e.Artifacts[0].Title = strings.Repeat("é", 129) }, false},
		{"invalid title utf8", func(e *submission.CreationEnvelope) { e.Artifacts[0].Title = string([]byte{0xff}) }, false},
		{"multibyte body at byte limit", func(e *submission.CreationEnvelope) { e.Artifacts[0].Body = strings.Repeat("é", 32768) }, true},
		{"multibyte body over byte limit", func(e *submission.CreationEnvelope) { e.Artifacts[0].Body = strings.Repeat("é", 32769) }, false},
		{"sixteen positive links", func(e *submission.CreationEnvelope) {
			e.Artifacts[0].LinkIssues = make([]int, 16)
			for i := range e.Artifacts[0].LinkIssues {
				e.Artifacts[0].LinkIssues[i] = i + 1
			}
		}, true},
		{"negative link", func(e *submission.CreationEnvelope) { e.Artifacts[0].LinkIssues = []int{-1} }, false},
		{"attachment to source", func(e *submission.CreationEnvelope) { e.Relationship = "attach"; e.Artifacts[0].ParentIssue = 278 }, true},
		{"attachment without parent", func(e *submission.CreationEnvelope) { e.Relationship = "attach" }, false},
		{"negative parent", func(e *submission.CreationEnvelope) { e.Artifacts[0].ParentIssue = -1 }, false},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := exactCreationFixture()
			tt.change(&e)
			raw, hash, err := submission.CanonicalCreation(e)
			if tt.valid {
				if err != nil || len(raw) == 0 || len(hash) != 64 {
					t.Fatalf("valid envelope: bytes=%d hash=%q err=%v", len(raw), hash, err)
				}
			} else if err == nil || len(raw) != 0 || hash != "" {
				t.Fatalf("invalid envelope: bytes=%d hash=%q err=%v", len(raw), hash, err)
			}
		})
	}
}
