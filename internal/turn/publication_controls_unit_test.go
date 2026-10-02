package turn

import (
	"errors"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
)

func TestMCPRuntime_PrepareCreationProposalRejectsUntrustedControls(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	valid := OwnerGrantControl{Repository: "owner/repo", SourceTask: "github:owner/repo#264", Mode: "research", Body: `/mcp-create prepare {"version":1,"relationship":"linked","artifacts":[{"title":"Follow-up","body":"Exact owner content"}]}`, CommentID: 11, ActorID: 42, CreatedAt: now}
	cases := []struct {
		name   string
		change func(*MCPRuntime, *OwnerGrantControl)
	}{
		{"missing authorizer/refused", func(r *MCPRuntime, _ *OwnerGrantControl) { r.CreationAuthorizer = nil }},
		{"disabled gate/refused", func(r *MCPRuntime, _ *OwnerGrantControl) { r.CreationAuthorizer.enabled = false }},
		{"nonowner/refused", func(_ *MCPRuntime, c *OwnerGrantControl) { c.ActorID = 43 }},
		{"bot identity/refused", func(_ *MCPRuntime, c *OwnerGrantControl) { c.ActorID = 99 }},
		{"bot flag on owner/refused", func(_ *MCPRuntime, c *OwnerGrantControl) { c.Bot = true }},
		{"missing comment provenance/refused", func(_ *MCPRuntime, c *OwnerGrantControl) { c.CommentID = 0 }},
		{"missing creation time/refused", func(_ *MCPRuntime, c *OwnerGrantControl) { c.CreatedAt = time.Time{} }},
		{"edited control/refused", func(_ *MCPRuntime, c *OwnerGrantControl) { c.UpdatedAt = now.Add(time.Second) }},
		{"repository disagrees with source/refused", func(_ *MCPRuntime, c *OwnerGrantControl) { c.Repository = "unrelated/repo" }},
		{"prose prefix/refused", func(_ *MCPRuntime, c *OwnerGrantControl) { c.Body = "Please " + c.Body }},
		{"quoted control/refused", func(_ *MCPRuntime, c *OwnerGrantControl) { c.Body = "> " + c.Body }},
		{"approval is not preparation/refused", func(_ *MCPRuntime, c *OwnerGrantControl) { c.Body = "/mcp-create abc linked" }},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// Observation establishes no prior success; new writes and forge effects
			// remain unavailable for rejected controls.
			runtime := &MCPRuntime{BotID: 99, CreationAuthorizer: &CreationAuthorizer{store: &store.CreationStore{}, sourceOutcomes: absentCreationOutcomes{}, enabled: true, ownerID: 42, botID: 99}}
			control := valid
			tt.change(runtime, &control)
			got, err := runtime.PrepareCreationProposal(t.Context(), control)
			if !errors.Is(err, store.ErrCreationAuthority) || got.Hash != "" || len(got.Envelope.Artifacts) != 0 {
				t.Fatalf("untrusted preparation returned hash=%q envelope=%+v error=%v", got.Hash, got.Envelope, err)
			}
		})
	}
}

func TestMCPRuntime_PrepareCreationProposalRejectsUnsupportedInput(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, body string }{
		{"future version", `{"version":2}`},
		{"negative version", `{"version":-1}`},
		{"string version", `{"version":"1"}`},
		{"malformed JSON", `{"version":`},
		{"second document", `{"version":1} {}`},
		{"claimed authority", `{"version":1,"approved":true}`},
		{"nested claimed authority", `{"version":1,"artifacts":[{"title":"test","body":"test","permit":{}}]}`},
	}
	for _, tt := range cases {
		t.Run(tt.name+"/refused without persistence", func(t *testing.T) {
			t.Parallel()
			runtime := &MCPRuntime{BotID: 99, CreationAuthorizer: &CreationAuthorizer{store: &store.CreationStore{}, sourceOutcomes: absentCreationOutcomes{}, enabled: true, ownerID: 42, botID: 99}}
			control := OwnerGrantControl{Repository: "owner/repo", SourceTask: "github:owner/repo#264", Mode: "research", Body: "/mcp-create prepare " + tt.body, ActorID: 42, CommentID: 11, CreatedAt: time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)}
			got, err := runtime.PrepareCreationProposal(t.Context(), control)
			if err == nil || got.Hash != "" || len(got.Envelope.Artifacts) != 0 {
				t.Fatalf("unsupported preparation returned hash=%q envelope=%+v error=%v", got.Hash, got.Envelope, err)
			}
		})
	}
}
