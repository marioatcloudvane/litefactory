package turn

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
)

func TestOwnerGrantCommand_ExactPublicGrammar(t *testing.T) {
	hash := strings.Repeat("a", 64)
	for _, relationship := range []string{"linked", "attach"} {
		t.Run(relationship, func(t *testing.T) {
			t.Parallel()
			r, err := ParseOwnerGrantCommand("/mcp-create " + hash + " " + relationship)
			if err != nil || r.ProposalHash != hash || r.Relationship != relationship {
				t.Fatalf("command=%+v %v", r, err)
			}
		})
	}
	cases := []string{"", " /mcp-create " + hash + " linked", "/mcp-create " + hash + " linked\n", "/mcp-create  " + hash + " linked", "/mcp-create\t" + hash + " linked", "> /mcp-create " + hash + " linked", "`/mcp-create " + hash + " linked`", "``\n/mcp-create " + hash + " linked\n``", "Please /mcp-create " + hash + " linked", "/mcp-create " + strings.ToUpper(hash) + " linked", "/mcp-create " + hash[:63] + " linked", "/mcp-create " + hash + " attached", "/mcp-create " + hash + " linked now", "/mcp-create " + hash + " linked\n/mcp-create " + hash + " linked"}
	for _, body := range cases {
		t.Run(body, func(t *testing.T) {
			t.Parallel()
			if _, err := ParseOwnerGrantCommand(body); err == nil {
				t.Fatal("nonexact or quoted approval accepted")
			}
		})
	}
}

func TestCreationAuthorizer_RejectsUntrustedControlBeforeStore(t *testing.T) {
	// A zero store has no I/O dependency. Every case must fail before touching it;
	// trusted eligible approval persistence is exercised by real SQLite tests.
	authorizer, err := NewCreationAuthorizer(CreationAuthorizerOptions{Store: &store.CreationStore{}, SourceOutcomes: absentCreationOutcomes{}, Enabled: true, OwnerID: 42, BotID: 99})
	if err != nil {
		t.Fatal(err)
	}
	valid := OwnerGrantControl{Repository: "owner/repo", SourceTask: "github:owner/repo#278", Mode: "research", Body: "/mcp-create " + strings.Repeat("a", 64) + " linked", CommentID: 11, ActorID: 42, CreatedAt: time.Date(2026, 9, 14, 10, 0, 0, 0, time.UTC)}
	cases := []struct {
		name   string
		change func(*OwnerGrantControl)
	}{
		{"nonowner copied command", func(c *OwnerGrantControl) { c.ActorID = 100 }},
		{"configured machine bot", func(c *OwnerGrantControl) { c.ActorID = 99 }},
		{"bot flagged owner", func(c *OwnerGrantControl) { c.Bot = true }},
		{"claimed author in text", func(c *OwnerGrantControl) { c.ActorID = 100; c.Body += " owner_id=42" }},
		{"missing comment provenance", func(c *OwnerGrantControl) { c.CommentID = 0 }},
		{"missing creation timestamp", func(c *OwnerGrantControl) { c.CreatedAt = time.Time{} }},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := valid
			tt.change(&c)
			id, err := authorizer.AdmitOwnerGrantIntent(t.Context(), c)
			if id != 0 || !errors.Is(err, store.ErrCreationAuthority) {
				t.Fatalf("untrusted control=%d %v", id, err)
			}
		})
	}
}

func TestCreationAuthorizer_GateAndUnassignedPermissions(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		a, err := NewCreationAuthorizer(CreationAuthorizerOptions{Store: &store.CreationStore{}, Enabled: enabled, OwnerID: 42, BotID: 99})
		if err != nil {
			t.Fatal(err)
		}
		if a.CreationVisible(t.Context(), nil) {
			t.Fatal("gate allocated absent attempt authority")
		}
		if r, err := a.ReserveCreation(t.Context(), nil, submission.CreationEnvelope{}); !errors.Is(err, store.ErrCreationAuthority) || r.Permit != nil {
			t.Fatal("gate alone granted reservation")
		}
		if _, err := a.BindCreationGrant(t.Context(), nil, 1); !errors.Is(err, store.ErrCreationAuthority) {
			t.Fatal("gate bound an absent attempt")
		}
		for _, mode := range []string{"research", "plan", "implement", "validate", "spike"} {
			profile, err := submission.NewToolProfile(mode)
			if err != nil {
				t.Fatal(err)
			}
			for _, tool := range profile.List() {
				if tool.Effect == "creation" || tool.Name == "create_issue" {
					t.Fatal("advertised unimplemented creation handler")
				}
			}
			if mode != "research" {
				guard := &AttemptGuard{lease: store.MCPLease{Binding: store.ResultBinding{Mode: mode}}}
				if a.CreationVisible(t.Context(), guard) {
					t.Fatalf("creation exposed in %s", mode)
				}
			}
		}
		if !enabled {
			if _, err := a.AdmitOwnerGrantIntent(t.Context(), OwnerGrantControl{Mode: "research", ActorID: 42, Body: "/mcp-create " + strings.Repeat("a", 64) + " linked"}); !errors.Is(err, store.ErrCreationAuthority) {
				t.Fatal("disabled gate accepted intent")
			}
			if _, err := a.BeginDispatch(t.Context(), &store.CreationPermit{}); !errors.Is(err, store.ErrCreationAuthority) {
				t.Fatal("disabled gate dispatched")
			}
		}
	}
}

// absentCreationOutcomes allows pure input tests to establish no prior success.
type absentCreationOutcomes struct{}

func (absentCreationOutcomes) GetOwnerIntent(context.Context, store.CreationSource) (int64, error) {
	return 0, store.ErrNotFound
}
func (absentCreationOutcomes) GetPreparation(context.Context, store.CreationSource) (store.CreationProposal, error) {
	return store.CreationProposal{}, store.ErrNotFound
}
