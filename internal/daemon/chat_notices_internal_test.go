package daemon

import (
	"crypto/ed25519"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/projectconnection"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
	"github.com/cloudvane/litefactory/internal/turn"
)

func TestChatNoticeUsesConfiguredLinearReader(t *testing.T) {
	ctx := t.Context()
	remote := newLinearWorkflowServer(t)
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "notice.db"))
	linearWorkflowCheck(t, err)
	t.Cleanup(func() { linearWorkflowCheck(t, db.Close()) })
	linearWorkflowCheck(t, db.Migrate(ctx))
	repo, err := db.UpsertRepo(ctx, store.Repo{Path: t.TempDir(), Forge: "linear", Owner: "team-uuid", Name: "project-uuid", RemoteURL: "git@github.com:code/repo.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: t.TempDir()})
	linearWorkflowCheck(t, err)
	version, err := db.SetConnection(ctx, store.ConnectionVersion{RepoID: repo.ID, Role: store.TicketConnection, Provider: ticket.Linear, Origin: ticket.Origin(remote.server.URL), Destination: "team-uuid/project-uuid", CredentialSource: "env", CredentialEnv: "NOTICE_LINEAR_TOKEN", Enabled: true, Provenance: "explicit", Linear: store.LinearConfiguration{WorkspaceID: "workspace-uuid", TeamID: "team-uuid", ProjectID: "project-uuid", AuthKind: "api_key", ModeLabelsJSON: `{"plan":"label-plan"}`, LifecycleStatesJSON: `{"open":"state-open","completed":"state-completed","retired":"state-retired"}`}}, 0)
	linearWorkflowCheck(t, err)
	factory := projectconnection.NewFactory(projectconnection.Options{DB: db, HTTPClient: remote.server.Client(), Resolver: secrets.Resolver{LookupEnv: func(string) (string, bool) { return "test-linear-token", true }}})
	clients, err := factory.Clients(ctx, version, 0)
	linearWorkflowCheck(t, err)
	snapshot, err := clients.Linear.Resolve(ctx, ticket.Locator{Provider: ticket.Linear, Origin: version.Origin, Container: version.Destination, Key: "parent-uuid"})
	linearWorkflowCheck(t, err)
	slot, err := db.LocalTicketSlot(ctx, "issue", snapshot.Ref.Identity)
	linearWorkflowCheck(t, err)
	session, err := db.GetOrCreateSession(ctx, repo, int(slot))
	linearWorkflowCheck(t, err)
	segment, err := db.GetOrCreateSegment(ctx, session.ID, 1, "session", "codex")
	linearWorkflowCheck(t, err)
	filter, err := turn.NewFilter(db, turn.Identities{OwnerUserID: 42, BotUserID: 99}, nil)
	linearWorkflowCheck(t, err)
	d := &Daemon{db: db, connections: factory, filter: filter, log: slog.New(slog.DiscardHandler)}
	d.buildChat(&ChatOptions{Settings: config.DiscordChat{ApplicationID: "1", OwnerID: "2", ChannelIDs: []string{"3"}, NoticeChannelID: "3"}}, make(ed25519.PublicKey, ed25519.PublicKeySize), secrets.NewToken("chat-fixture"))
	at := time.Now().UTC()
	ctx = projectContext(ctx, repo.ID)
	linearWorkflowCheck(t, d.chat.collector.Collect(ctx, at))
	run, err := db.AllocateRun(ctx, store.AllocateRunParams{SessionID: session.ID, SegmentID: segment.ID, Mode: "plan", Agent: "codex", TriggerCommentID: 1, StartedAt: at, LogPath: func(int) string { return "/logs/test" }})
	linearWorkflowCheck(t, err)
	linearWorkflowCheck(t, d.chat.collector.Collect(ctx, at))
	notice, err := db.ReadChatNotice(ctx, "discord", fmt.Sprintf("run:%d:start", run.ID))
	linearWorkflowCheck(t, err)
	if !strings.Contains(notice.ReplyText, "Plan LF-1 (Parent) started.") || !strings.Contains(notice.ReplyText, "https://linear.app/test/issue/LF-1") || strings.Contains(notice.ReplyText, fmt.Sprint(slot)) {
		t.Fatal(notice.ReplyText)
	}
}
