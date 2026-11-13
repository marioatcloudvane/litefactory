package messaging_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/factory"
	"github.com/cloudvane/litefactory/internal/messaging"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/testutil"
)

// A parked owner wait stays pending until the owner answers. Its stop notice
// is admitted once; later passes must not read the issue again, whether the
// read fails (as under a provider rate limit) or succeeds (#1513).
func TestNoticeParkedWaitReadsIssueOnce(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	dir, err := os.MkdirTemp(testutil.ShortTempRoot(), "notice-park-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	db, err := store.Open(ctx, filepath.Join(dir, "notices.db"))
	must(err)
	t.Cleanup(func() { _ = db.Close() })
	must(db.Migrate(ctx))
	repo, err := db.UpsertRepo(ctx, store.Repo{Path: dir, Forge: "github", Owner: "acme", Name: "repo", RemoteURL: "git@github.com:acme/repo.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: dir})
	must(err)
	session, err := db.GetOrCreateSession(ctx, repo, 1507)
	must(err)
	at := time.Now().UTC()
	issues := &noticeIssues{fail: true}
	c := &messaging.NoticeCollector{Factory: factory.NewMessaging(db), Transport: "test", Channel: "3", Issues: issues}
	must(c.Collect(ctx, at))

	definition, err := db.CreateGraphDefinition(ctx, store.GraphDefinition{Team: "notices", Mode: "implement", ProfileVersion: 1, Path: "implement.dot", Digest: strings.Repeat("c", 64), CreatedAt: at})
	must(err)
	instance, err := db.CreateGraphInstance(ctx, store.GraphInstance{SessionID: session.ID, DefinitionID: definition.ID, CurrentNode: "deliver", StartedAt: at})
	must(err)
	stop := store.GraphStop{Key: "parked-stop", InstanceID: instance.ID, Origin: "deliver", Cause: "blocked", Reason: "Delivery is blocked", Candidate: []byte(`{"reason":"Delivery is blocked"}`), At: at, AllowRunless: true}
	must(db.ParkGraphNode(ctx, stop))

	must(c.Collect(ctx, at.Add(time.Second)))
	if issues.calls != 1 {
		t.Fatalf("first pass reads=%d", issues.calls)
	}
	notice, err := db.ReadChatNotice(ctx, "test", "stop:parked-stop@"+store.FormatTime(at)+"@"+session.TaskRef)
	must(err)
	if notice.Command != "stopped" || !strings.Contains(notice.ReplyText, "#1507") {
		t.Fatalf("untitled stop notice: %+v", notice)
	}
	for i := 2; i <= 30; i++ {
		issues.fail = i%2 == 0
		must(c.Collect(ctx, at.Add(time.Duration(i)*time.Second)))
	}
	if issues.calls != 1 {
		t.Fatalf("a pending parked wait re-read its issue: reads=%d", issues.calls)
	}
	_, records, err := db.ListNoticeRecords(ctx, "test", "owner_waits", at.Add(time.Minute))
	must(err)
	if len(records) != 1 || records[0].Ready {
		t.Fatalf("parked wait no longer pending: %+v", records)
	}
}
