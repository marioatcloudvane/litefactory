package store

import (
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func chatResearchFixture(t *testing.T) (*DB, GraphOwnerReply) {
	t.Helper()
	db, in := predatingReplyFixture(t, true)
	wait, err := db.GetGraphOwnerWait(t.Context(), in.InstanceID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.sql.ExecContext(t.Context(), `UPDATE graph_comment_intents SET candidate=? WHERE delivery_key=?`, `{"outcome":"incomplete","questions":[{"id":"Q-1","text":"Which boundary?"}]}`, in.DeliveryKey)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.RecordGraphOutput(t.Context(), GraphOutput{NodeAttemptID: wait.SourceAttemptID, Kind: GraphOutputJSON, Payload: []byte(`{"version":1,"response":"ask","result":{"questions":[{"id":"Q-1","text":"Which boundary?"}]}}`), CreatedAt: in.At})
	if err != nil {
		t.Fatal(err)
	}
	return db, in
}

func TestChatResearchClaimAndForgeRace(t *testing.T) {
	t.Parallel()
	db, in := chatResearchFixture(t)
	before, err := db.GetGraphOwnerWait(t.Context(), in.InstanceID)
	if err != nil || before.ReplySource != "forge" {
		t.Fatalf("default source: %+v, %v", before, err)
	}
	var claims [2]GraphOwnerWait
	var errs [2]error
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range 2 {
		wg.Go(func() {
			<-start
			if i == 0 {
				claims[i], errs[i] = db.ClaimChatResearchReply(t.Context(), in)
			} else {
				claims[i], errs[i] = db.ClaimGraphOwnerReply(t.Context(), in)
			}
		})
	}
	close(start)
	wg.Wait()
	winner := 0
	if errs[0] != nil {
		winner = 1
	}
	if errs[winner] != nil || !errors.Is(errs[1-winner], ErrGraphOwnerWait) {
		t.Fatalf("race results: %v", errs)
	}
	wait := claims[winner]
	if wait.State != "claimed" || wait.ReplyBody != in.Body || wait.ReplyAuthorID != in.OwnerID {
		t.Fatalf("claim: %+v", wait)
	}
	due, err := db.ListDueGraphDispatches(t.Context(), in.At, 10)
	if err != nil {
		t.Fatal(err)
	}
	if winner == 0 {
		if wait.ReplySource != "chat" || !IsChatTrigger(wait.ReplyID) || !IsPrivateTrigger(wait.ReplyID) || len(due) != 1 || due[0].Node != "shape" {
			t.Fatalf("chat claim and dispatch: %+v %+v", wait, due)
		}
	} else if wait.ReplySource != "forge" || len(due) != 0 {
		t.Fatalf("forge claim: %+v %+v", wait, due)
	}
	var watermark int64
	if err = db.sql.QueryRowContext(t.Context(), `SELECT COALESCE(MAX(last_comment_id),0) FROM watermarks`).Scan(&watermark); err != nil || IsPrivateTrigger(watermark) {
		t.Fatalf("private watermark: %d %v", watermark, err)
	}
	if _, err = db.ClaimChatResearchReply(t.Context(), in); !errors.Is(err, ErrGraphOwnerWait) {
		t.Fatalf("repeated answer: %v", err)
	}
}

func TestChatResearchClaimRefusalsAreAtomic(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"unpublished", "review", "done", "ended", "superseded", "wrong mode", "oversized", "storage failure"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			db, in := chatResearchFixture(t)
			var query string
			switch name {
			case "unpublished":
				query = `UPDATE report_deliveries SET completed=0`
			case "review":
				query = `UPDATE graph_comment_intents SET kind='review'`
			case "done":
				query = `UPDATE graph_outputs SET payload='{"response":"done"}'`
			case "ended":
				query = `UPDATE graph_instances SET status='failed',finished_at=started_at,next_due_at=NULL`
			case "superseded":
				query = `INSERT INTO graph_instances(session_id,definition_id,current_node,status,started_at) SELECT session_id,definition_id,current_node,'running',started_at FROM graph_instances`
			case "wrong mode":
				in.Mode = "plan"
			case "oversized":
				in.Body = strings.Repeat("a", 65537)
			case "storage failure":
				query = `CREATE TRIGGER fail_chat_dispatch BEFORE INSERT ON graph_dispatches BEGIN SELECT RAISE(ABORT,'fixture failure'); END`
			}
			if query != "" {
				if _, err := db.sql.ExecContext(t.Context(), query); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := db.ClaimChatResearchReply(t.Context(), in); err == nil {
				t.Fatal("refused claim succeeded")
			}
			wait, err := db.GetGraphOwnerWait(t.Context(), in.InstanceID)
			if err != nil || wait.State != "waiting" || wait.ReplyID != 0 || wait.ReplySource != "forge" {
				t.Fatalf("refusal changed wait: %+v %v", wait, err)
			}
			var count int
			if err = db.sql.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM graph_dispatches`).Scan(&count); err != nil || count != 0 {
				t.Fatalf("refusal parked work: %d %v", count, err)
			}
		})
	}
}

func TestForgeClaimRefusesChatIdentity(t *testing.T) {
	t.Parallel()
	var db *DB
	in := GraphOwnerReply{InstanceID: 1, OwnerID: 7, AuthorID: 7, CommentID: chatTriggerBase + 1, QuestionCommentID: 2, TaskRef: "github:acme/repo#1", SourceRef: "github:acme/repo#1", Mode: "research", DeliveryKey: "question", Body: "answer", At: graphTestTime()}
	if _, err := db.ClaimGraphOwnerReply(t.Context(), in); !errors.Is(err, ErrGraphInvariant) {
		t.Fatalf("forge admitted private identity: %v", err)
	}
}

func TestChatResearchMigrationPreservesExistingRows(t *testing.T) {
	t.Parallel()
	db, err := Open(t.Context(), filepath.Join(t.TempDir(), "migration.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	oldInbox, err := migrationFS.ReadFile("migrations/20261010174220992_chat_inbox.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{
		string(oldInbox),
		`CREATE TABLE repos(id INTEGER PRIMARY KEY)`,
		`ALTER TABLE chat_inbox ADD COLUMN arguments TEXT NOT NULL DEFAULT '{}'`,
		`ALTER TABLE chat_inbox ADD COLUMN question TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE chat_inbox ADD COLUMN project_option TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE chat_inbox ADD COLUMN resolved_repo_id INTEGER`,
		`ALTER TABLE chat_inbox ADD COLUMN answer_attempts INTEGER NOT NULL DEFAULT 0`,
		`CREATE TABLE graph_owner_waits(delivery_key TEXT PRIMARY KEY,state TEXT NOT NULL)`,
		`INSERT INTO graph_owner_waits VALUES('old-wait','claimed')`,
		`INSERT INTO chat_inbox(transport,message_id,channel_ref,command,state,attempts,next_attempt_at,reply_text,created_at) VALUES('discord','1','2','health','pending',3,'2026-10-10T00:00:00.000Z','retained reply','2026-10-10T00:00:00.000Z')`,
	} {
		if _, err = db.sql.ExecContext(t.Context(), query); err != nil {
			t.Fatal(err)
		}
	}
	body, err := migrationFS.ReadFile("migrations/20261010211952115_chat_research.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.sql.ExecContext(t.Context(), string(body)); err != nil {
		t.Fatal(err)
	}
	var source, state string
	if err = db.sql.QueryRowContext(t.Context(), `SELECT reply_source,state FROM graph_owner_waits WHERE delivery_key='old-wait'`).Scan(&source, &state); err != nil || source != "forge" || state != "claimed" {
		t.Fatalf("old wait: %s %s %v", source, state, err)
	}
	r, err := db.ReadChatReply(t.Context(), "discord", "1")
	if err != nil || r.State != "pending" || r.Attempts != 3 || r.ReplyText != "retained reply" || r.TargetRef != "" {
		t.Fatalf("old inbox row: %+v %v", r, err)
	}
	if _, err = db.sql.ExecContext(t.Context(), `UPDATE chat_inbox SET state='working',target_ref='github:acme/research#378'`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.sql.ExecContext(t.Context(), `UPDATE chat_inbox SET state='sent'`); err != nil {
		t.Fatal(err)
	}
	r, err = db.ReadChatReply(t.Context(), "discord", "1")
	if err != nil {
		t.Fatal(err)
	}
	if err = db.FinishChatReply(t.Context(), r, false, graphTestTime()); err != nil {
		t.Fatal(err)
	}
	r, err = db.ReadChatReply(t.Context(), "discord", "1")
	if err != nil || r.TargetRef != "" || r.ReplyText != "" {
		t.Fatalf("confirmed row retained content: %+v %v", r, err)
	}
}

func TestResearchMigrationPreservesPendingProjectQuestion(t *testing.T) {
	db, err := Open(t.Context(), filepath.Join(t.TempDir(), "project-question.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	var prior []migration
	for _, m := range migrations {
		if !strings.Contains(m.name, "_chat_research.sql") {
			prior = append(prior, m)
		}
	}
	if err = db.migrateAll(t.Context(), prior); err != nil {
		t.Fatal(err)
	}
	repo, err := db.UpsertRepo(t.Context(), Repo{Path: t.TempDir(), Forge: "github", Owner: "acme", Name: "app", BaseBranch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.AdmitChatReply(t.Context(), ChatReply{Transport: "discord", MessageID: "1", ChannelRef: "2", Command: "ask", CreatedAt: time.Now().UTC(), Arguments: map[string]string{"question": "What work is open?", "project": "acme/app"}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.sql.ExecContext(t.Context(), `UPDATE chat_inbox SET resolved_repo_id=?,answer_attempts=1`, repo.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	r, err := db.ReadChatReply(t.Context(), "discord", "1")
	if err != nil || r.State != "answering" || r.Question != "What work is open?" || r.ProjectOption != "acme/app" || r.ResolvedRepoID != repo.ID || r.AnswerAttempts != 1 || r.TargetRef != "" {
		t.Fatalf("pending project question lost: %+v %v", r, err)
	}
}
