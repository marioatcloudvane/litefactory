package turn

import (
	"context"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
	"testing"
	"time"
)

func TestCreationAuthorizerDB_LiveGateGrantAndMode(t *testing.T) {
	for _, mode := range []string{"research", "spike", "plan", "implement", "validate"} {
		t.Run(mode, func(t *testing.T) {
			db, s, opts := agentResultFixture(t, mode)
			service, err := BindAgentResultService(t.Context(), opts)
			if err != nil {
				t.Fatal(err)
			}
			now := time.Now().UTC().Add(-time.Minute)
			cs, err := store.NewCreationStore(db, func() time.Time { return now })
			if err != nil {
				t.Fatal(err)
			}
			enabled, err := NewCreationAuthorizer(CreationAuthorizerOptions{Store: cs, Enabled: true, OwnerID: 10, BotID: 20})
			if err != nil {
				t.Fatal(err)
			}
			disabled, err := NewCreationAuthorizer(CreationAuthorizerOptions{Store: cs, Enabled: false, OwnerID: 10, BotID: 20})
			if err != nil {
				t.Fatal(err)
			}
			e := submission.CreationEnvelope{Version: 1, Repository: "example/results", SourceTask: s.TaskRef, Relationship: "linked", Artifacts: []submission.CreationArtifact{{Title: "Reviewed", Body: "Exact body"}}}
			hash, err := enabled.RetainProposal(t.Context(), e)
			if err != nil {
				t.Fatal(err)
			}
			intent, err := enabled.AdmitOwnerGrantIntent(t.Context(), OwnerGrantControl{Repository: e.Repository, SourceTask: s.TaskRef, Mode: "research", Body: "/mcp-create " + hash + " linked", CommentID: 1, ActorID: 10, CreatedAt: now})
			if err != nil {
				t.Fatal(err)
			}
			authority, err := NewAttemptAuthority(t.Context(), db)
			if err != nil {
				t.Fatal(err)
			}
			defer authority.Close(context.WithoutCancel(t.Context()))
			guard, err := authority.Bind(t.Context(), service.Binding(), service.MCPPermissionDigest(), time.Now().Add(time.Hour))
			if err != nil {
				t.Fatal(err)
			}
			if _, err = disabled.BindCreationGrant(t.Context(), guard, intent); err == nil {
				t.Fatal("gate off bound grant")
			}
			if _, err = disabled.ReserveCreation(t.Context(), guard, e); err == nil {
				t.Fatal("gate off reservation")
			}
			if _, err = enabled.ReserveCreation(t.Context(), guard, e); err == nil {
				t.Fatal("gate only created")
			}
			_, err = enabled.BindCreationGrant(t.Context(), guard, intent)
			if mode != "research" {
				if err == nil {
					t.Fatal("nonresearch granted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			r, err := enabled.ReserveCreation(t.Context(), guard, e)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = disabled.BeginDispatch(t.Context(), r.Permit); err == nil {
				t.Fatal("disabled dispatch")
			}
			if _, err = enabled.BeginDispatch(t.Context(), r.Permit); err != nil {
				t.Fatal(err)
			}
		})
	}
}
