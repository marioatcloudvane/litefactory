package daemon

import (
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
	"github.com/cloudvane/litefactory/internal/turn"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMCPOwnerIntake_RetainedProposalAndTrustedMetadata(t *testing.T) {
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "creation.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	r, err := db.UpsertRepo(t.Context(), store.Repo{Path: root, Forge: store.ForgeGitHub, Owner: "owner", Name: "api", RemoteURL: "git@github.com:owner/api.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: filepath.Join(root, "worktrees")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.GetOrCreateSession(t.Context(), r, 278); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	cs, err := store.NewCreationStore(db, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	authorizer := func(enabled bool) *turn.CreationAuthorizer {
		a, e := turn.NewCreationAuthorizer(turn.CreationAuthorizerOptions{Store: cs, Enabled: enabled, OwnerID: 10, BotID: 20})
		if e != nil {
			t.Fatal(e)
		}
		return a
	}
	a := authorizer(true)
	envelope := submission.CreationEnvelope{Version: 1, Repository: "owner/api", SourceTask: "github:owner/api#278", Relationship: "linked", Artifacts: []submission.CreationArtifact{{Title: "Exact issue", Body: "Reviewed content"}}}
	hash, err := a.RetainProposal(t.Context(), envelope)
	if err != nil {
		t.Fatal(err)
	}
	good := github.Comment{ID: 1, Body: "/mcp-create " + hash + " linked", Author: github.User{ID: 10, Login: "owner"}, CreatedAt: now}
	repo := github.Repo{Owner: "owner", Name: "api"}
	for _, name := range []string{"gate off", "nonowner copied", "bot type", "bot identity", "claimed author", "quoted", "wrong repo", "wrong source", "absent proposal", "wrong relationship", "edited", "stale", "before retained"} {
		t.Run(name, func(t *testing.T) {
			c := good
			selected := a
			target := repo
			issue := 278
			mode := "research"
			switch name {
			case "gate off":
				selected = authorizer(false)
			case "nonowner copied":
				c.Author.ID = 11
			case "bot type":
				c.Author.IsBot = true
			case "bot identity":
				c.Author.ID = 20
			case "claimed author":
				c.Author.ID = 11
				c.Author.Login = "owner"
				c.Body += "\nowner_id: 10"
			case "quoted":
				c.Body = "> " + c.Body
			case "wrong repo":
				target.Owner = "foreign"
			case "wrong source":
				issue = 279
			case "absent proposal":
				c.Body = "/mcp-create " + strings.Repeat("f", 64) + " linked"
			case "wrong relationship":
				c.Body = "/mcp-create " + hash + " attach"
			case "edited":
				c.UpdatedAt = now.Add(time.Second)
			case "stale":
				c.CreatedAt = now.Add(-30 * time.Minute)
			case "before retained":
				c.CreatedAt = now.Add(-time.Nanosecond)
			}
			if _, e := AdmitMCPOwnerCommand(t.Context(), selected, target, issue, mode, c); e == nil {
				t.Fatal("unauthorized intent recorded")
			}
		})
	}
	id, err := AdmitMCPOwnerCommand(t.Context(), a, repo, 278, "research", good)
	if err != nil {
		t.Fatal(err)
	}
	again, err := AdmitMCPOwnerCommand(t.Context(), a, repo, 278, "research", good)
	if err != nil || again != id {
		t.Fatalf("replay minted new intent %d/%d %v", id, again, err)
	}
	now = now.Add(30 * time.Minute)
	if again, err = AdmitMCPOwnerCommand(t.Context(), a, repo, 278, "research", good); err != nil || again != id {
		t.Fatalf("expired admission recovery changed original intent %d/%d %v", id, again, err)
	}
	if pub, _, err := cs.ClaimOwnerPublication(t.Context(), id, true); err != nil || pub.Reason != store.OwnerPublicationExpired {
		t.Fatalf("exact deadline recovery renewed authority: %+v %v", pub, err)
	}
}
