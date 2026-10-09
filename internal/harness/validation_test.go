package harness

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
)

func TestValidateRunnerRejectsMutatingAndFreshCheckTools(t *testing.T) {
	r := &Runner{}
	for _, tools := range [][]string{nil, {"Bash"}, {"Write"}, {"Read", "Edit"}} {
		req := agent.Request{Mode: "validate", Permissions: agent.Permissions{Payload: ToolSurface{Tools: tools}}}
		err := r.validateRequest(req)
		if err == nil || !strings.Contains(err.Error(), "Validate") {
			t.Fatal("unsafe surface accepted", tools, err)
		}
	}
}

func TestRetainedValidationRemovesDelegationWithoutMutatingCaller(t *testing.T) {
	tools := []string{"Read", "Task", "Glob", "Grep"}
	allowed := []string{"Read", "Task", "Glob", "Grep"}
	req := agent.Request{Mode: "validate", RetainedValidation: true, Permissions: agent.Permissions{Payload: ToolSurface{Tools: tools, AllowedTools: allowed}}}
	normalized := retainedValidationRequest(req)
	surface, ok := normalized.Permissions.Payload.(ToolSurface)
	if !ok {
		t.Fatal("normalized validation permissions lack a tool surface")
	}
	if strings.Join(surface.Tools, ",") != "Read,Glob,Grep" || strings.Join(surface.AllowedTools, ",") != "Read,Glob,Grep" {
		t.Fatalf("retained review grants delegation: %+v", surface)
	}
	surface.Tools[0], surface.AllowedTools[0] = "changed", "changed"
	if strings.Join(tools, ",") != "Read,Task,Glob,Grep" || strings.Join(allowed, ",") != "Read,Task,Glob,Grep" {
		t.Fatal("normalization aliases caller tool authority")
	}
	if err := (&Runner{}).validateRequest(req); err == nil || !strings.Contains(err.Error(), "Validate") {
		t.Fatalf("direct Task bypass accepted: %v", err)
	}
}

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

// Codex Validate keeps a read-only shell to read the withheld bodies, so both
// contract versions must confine any shell to file reads.
func TestValidationPromptConfinesShellToFileReads(t *testing.T) {
	context := &agent.ValidationContext{Version: 1, Parent: "github:acme/acme-api#471", Generation: 1}
	for _, submission := range []int{0, 2} {
		var b strings.Builder
		if err := writeValidation(&b, PromptInput{Validation: context, SubmissionVersion: submission}); err != nil {
			t.Fatal(err)
		}
		prompt := b.String()
		for _, want := range []string{"If you have a shell, use it only to read files", "Do not run tests, builds, linters, scripts, package managers, git commands that change state, or any other command."} {
			if !strings.Contains(prompt, want) {
				t.Fatalf("submission %d lacks %q", submission, want)
			}
		}
	}
}

// The Validate repair agent is bounded by its validate-repair role surface;
// every other Validate profile keeps the read-only check.
func TestValidateRepairProfileGetsTheFixerSurfaceOnly(t *testing.T) {
	r := &Runner{}
	surface := ToolSurface{Tools: []string{"Read", "Glob", "Grep", "Edit", "Write", "Bash"}, AllowedTools: []string{"Read", "Bash(go test:*)"}}
	err := r.validateRequest(agent.Request{Mode: "validate", Permissions: agent.Permissions{Profile: "validate-repair", Payload: surface}})
	if err != nil && (strings.Contains(err.Error(), "Validate") || strings.Contains(err.Error(), "validate-repair")) {
		t.Fatalf("validate-repair surface refused: %v", err)
	}
	for _, tools := range [][]string{nil, {"Read", "NotebookEdit"}, {"Read", "mcp__forge__merge"}} {
		err := r.validateRequest(agent.Request{Mode: "validate", Permissions: agent.Permissions{Profile: "validate-repair", Payload: ToolSurface{Tools: tools}}})
		if err == nil || !strings.Contains(err.Error(), "validate-repair") {
			t.Fatalf("validate-repair surface %v accepted: %v", tools, err)
		}
	}
	for _, profile := range []string{"validate", "bug-fixer", ""} {
		err := r.validateRequest(agent.Request{Mode: "validate", Permissions: agent.Permissions{Profile: profile, Payload: surface}})
		if err == nil || !strings.Contains(err.Error(), "Validate cannot grant") {
			t.Fatalf("Validate profile %q granted editing tools: %v", profile, err)
		}
	}
}
