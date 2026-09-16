package submission_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/submission"
)

func exactCreationFixture() submission.CreationEnvelope {
	return submission.CreationEnvelope{Version: 1, Repository: "owner/repository", SourceTask: "github:owner/repository#278", Relationship: "linked", Artifacts: []submission.CreationArtifact{{Title: "Exact reviewed title", Body: "Exact reviewed body", LinkIssues: []int{278}}}}
}
func TestCanonicalCreation_BindsContentAndRelationships(t *testing.T) {
	mutations := []struct {
		name   string
		change func(*submission.CreationEnvelope)
	}{
		{"title", func(e *submission.CreationEnvelope) { e.Artifacts[0].Title += " changed" }},
		{"body whitespace", func(e *submission.CreationEnvelope) { e.Artifacts[0].Body += " " }},
		{"repository", func(e *submission.CreationEnvelope) {
			e.Repository = "other/repository"
			e.SourceTask = "github:other/repository#278"
		}},
		{"source issue", func(e *submission.CreationEnvelope) { e.SourceTask = "github:owner/repository#279" }},
		{"attach relationship", func(e *submission.CreationEnvelope) { e.Relationship = "attach"; e.Artifacts[0].ParentIssue = 278 }},
		{"link target", func(e *submission.CreationEnvelope) { e.Artifacts[0].LinkIssues = []int{279} }},
		{"artifact count", func(e *submission.CreationEnvelope) {
			e.Artifacts = append(e.Artifacts, submission.CreationArtifact{Title: "Second issue", Body: "Second exact body"})
		}},
	}
	for _, tt := range mutations {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			original := exactCreationFixture()
			raw, hash, err := submission.CanonicalCreation(original)
			if err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256(raw)
			if hash != hex.EncodeToString(sum[:]) {
				t.Fatal("hash is not canonical envelope digest")
			}
			snapshot := bytes.Clone(raw)
			tt.change(&original)
			changed, other, err := submission.CanonicalCreation(original)
			if err != nil {
				t.Fatal(err)
			}
			if hash == other || bytes.Equal(raw, changed) {
				t.Fatal("content or target change retained authorization hash")
			}
			if !bytes.Equal(raw, snapshot) {
				t.Fatal("caller mutation aliased retained bytes")
			}
		})
	}
}

func TestCanonicalCreation_RefusalMatrix(t *testing.T) {
	cases := []struct {
		name   string
		change func(*submission.CreationEnvelope)
	}{
		{"unknown version", func(e *submission.CreationEnvelope) { e.Version = 2 }},
		{"foreign repository", func(e *submission.CreationEnvelope) { e.Repository = "foreign/repository" }},
		{"invalid source", func(e *submission.CreationEnvelope) { e.SourceTask = "owner/repository#278" }},
		{"unknown relationship", func(e *submission.CreationEnvelope) { e.Relationship = "create-anywhere" }},
		{"zero artifacts", func(e *submission.CreationEnvelope) { e.Artifacts = nil }},
		{"six artifacts", func(e *submission.CreationEnvelope) {
			for len(e.Artifacts) < 6 {
				e.Artifacts = append(e.Artifacts, e.Artifacts[0])
			}
		}},
		{"empty title", func(e *submission.CreationEnvelope) { e.Artifacts[0].Title = " " }},
		{"title over limit", func(e *submission.CreationEnvelope) { e.Artifacts[0].Title = strings.Repeat("x", 257) }},
		{"empty body", func(e *submission.CreationEnvelope) { e.Artifacts[0].Body = "\n" }},
		{"body over limit", func(e *submission.CreationEnvelope) { e.Artifacts[0].Body = strings.Repeat("x", 65537) }},
		{"invalid utf8", func(e *submission.CreationEnvelope) { e.Artifacts[0].Body = string([]byte{0xff}) }},
		{"attach wrong parent", func(e *submission.CreationEnvelope) { e.Relationship = "attach"; e.Artifacts[0].ParentIssue = 999 }},
		{"linked adds parent", func(e *submission.CreationEnvelope) { e.Artifacts[0].ParentIssue = 278 }},
		{"invalid link", func(e *submission.CreationEnvelope) { e.Artifacts[0].LinkIssues = []int{0} }},
		{"too many links", func(e *submission.CreationEnvelope) {
			e.Artifacts[0].LinkIssues = make([]int, 17)
			for i := range e.Artifacts[0].LinkIssues {
				e.Artifacts[0].LinkIssues[i] = i + 1
			}
		}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := exactCreationFixture()
			tt.change(&e)
			raw, hash, err := submission.CanonicalCreation(e)
			if err == nil || len(raw) != 0 || hash != "" {
				t.Fatalf("invalid creation shape produced authority bytes/hash: %v", err)
			}
		})
	}
	t.Run("exact limits allowed", func(t *testing.T) {
		e := exactCreationFixture()
		e.Artifacts[0].Title = strings.Repeat("x", 256)
		e.Artifacts[0].Body = strings.Repeat("x", 65536)
		for len(e.Artifacts) < 5 {
			e.Artifacts = append(e.Artifacts, e.Artifacts[0])
		}
		if _, _, err := submission.CanonicalCreation(e); err != nil {
			t.Fatal(err)
		}
	})
}
