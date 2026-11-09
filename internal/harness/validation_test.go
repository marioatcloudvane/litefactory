package harness

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
)

func TestValidationPromptWithholdsSnapshotBodies(t *testing.T) {
	body := []byte(strings.Repeat("package turn // retained source\n", 400))
	context := &agent.ValidationContext{
		Version: 1, Parent: "github:acme/acme-api#207", Generation: 1,
		Stories: []agent.ValidationStory{{
			Story:    agent.ApprovedStory{Key: "US-001"},
			Criteria: []string{"AC-001-1"},
			Snapshot: "snapshot-digest",
			Evidence: &agent.StoryEvidence{
				ImplementationDigest: "digest",
				Files: []agent.StorySnapshotFile{
					{Path: "internal/turn/managed_scope.go", Content: body, SHA256: "abc123"},
					{Path: "internal/turn/gone.go", Deleted: true, SHA256: "def456"},
				},
				Tests: []agent.StoryCheck{{Name: "go test ./...", Passed: true, Evidence: "ok"}},
			},
			EvidenceStatus: "published",
		}},
	}
	// Both contract versions describe the same retained snapshots, so neither
	// may carry their bodies.
	var prompt string
	for _, submission := range []int{0, 2} {
		var b strings.Builder
		if err := writeValidation(&b, PromptInput{Validation: context, SubmissionVersion: submission}); err != nil {
			t.Fatal(err)
		}
		prompt = b.String()
		if strings.Contains(prompt, base64.StdEncoding.EncodeToString(body)) {
			t.Fatalf("submission %d inlines the retained body; %d bytes", submission, len(prompt))
		}
		if len(prompt) > 4000 {
			t.Fatalf("submission %d carries the body some other way: %d bytes", submission, len(prompt))
		}
		if !strings.Contains(prompt, "sha256") {
			t.Fatalf("submission %d dropped the hashes the reviewer verifies against", submission)
		}
	}
	// What the reviewer needs to check integrity itself must survive.
	for _, want := range []string{"internal/turn/managed_scope.go", "abc123", "def456", "snapshot-digest", "AC-001-1", "go test ./...", "sha256"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt dropped %q", want)
		}
	}
	// The caller's context keeps its bodies: only the projection is reduced.
	if !bytes.Equal(context.Stories[0].Evidence.Files[0].Content, body) {
		t.Fatal("withholding mutated the caller's retained evidence")
	}
}
