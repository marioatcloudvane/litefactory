package turn

import (
	"context"
	"errors"
	"html"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
)

func parkNoteFixture(t *testing.T) (*fixture, *fakeForge, *implementRuntime) {
	t.Helper()
	f, forge, _ := assembledImplementFixture(t, lifecycleMCPHost(func(context.Context, agent.Request) (*agent.RunResult, error) {
		t.Fatal("no agent expected")
		return nil, nil
	}))
	seedImplementStory(t, f)
	for range store.MaxImplementAttempts {
		_, err := f.db.RecordImplementFailure(t.Context(), implementTaskParent, 1, "story-001", "the build did not compile", implementClock)
		mustProgress(t, err)
	}
	row := implementWorkerRow(t, f)
	parent, err := naming.ParseRef(row.Parent)
	mustProgress(t, err)
	rt := &implementRuntime{opts: f.opts, parentRef: parent, generation: 1, worker: &row, definition: mustCaptureImplement(t)}
	return f, forge, rt
}

func TestImplementParkNoteFullResultAndOversize(t *testing.T) {
	f, _, _ := parkNoteFixture(t)
	node := agentresult.ResearchNode{Response: "blocked", Result: agentresult.Result{
		Comment:   strings.Repeat("Comment 界 | ", 180) + "COMMENT_END",
		Reason:    strings.Repeat("Reason ", 320) + "REASON_END",
		Questions: []agentresult.Question{{ID: "q1", Text: "First question?", Options: []agentresult.Option{{ID: "yes", Text: "option text"}}}, {ID: "q2", Text: "Second question? @owner <!-- marker --> [link](url)"}},
	}}
	body, err := renderImplementParkAnswer(t.Context(), f.opts.ResearchReporter, node)
	mustProgress(t, err)
	decoded := html.UnescapeString(body.String())
	for _, want := range []string{node.Result.Comment, node.Result.Reason, "First question?", "Second question?", "option text"} {
		if !strings.Contains(decoded, want) {
			t.Fatalf("full answer missing %q", want)
		}
	}
	if strings.Contains(body.String(), "@owner") || strings.Contains(body.String(), "<!-- marker") || strings.Contains(body.String(), "[link]") {
		t.Fatal("answer text was active")
	}
	node.Result.Reason = strings.Repeat("large ", 20000)
	if _, err := renderImplementParkAnswer(t.Context(), f.opts.ResearchReporter, node); err == nil {
		t.Fatal("oversized answer was accepted")
	}
}

func TestImplementParkNoteReplayResumeAndHistorical(t *testing.T) {
	f, forge, rt := parkNoteFixture(t)
	for range 2 {
		_, err := rt.parkStory(t.Context(), f.opts, ActivityRequest{})
		mustProgress(t, err)
	}
	if forge.attemptCount() != 1 {
		t.Fatalf("replay posted %d times", forge.attemptCount())
	}
	row := implementWorkerRow(t, f)
	notes, err := implementParkNotes(t.Context(), f.db, ImplementAuthority{Parent: rt.parentRef, Generation: 1, Stories: []ImplementStory{{Key: row.Key, IssueNumber: f.session.IssueNumber}}}, []store.ImplementStory{row})
	mustProgress(t, err)
	if notes[row.Key].CommentID == 0 {
		t.Fatal("missing confirmed comment")
	}
	for _, move := range [][2]string{{store.ImplementParked, store.ImplementWaiting}, {store.ImplementWaiting, store.ImplementRunning}} {
		row, err = f.db.TransitionImplementStory(t.Context(), store.ImplementTransition{Parent: row.Parent, Generation: 1, Key: row.Key, From: move[0], To: move[1], ExpectedRevision: row.Revision, At: implementClock})
		mustProgress(t, err)
	}
	mustProgress(t, f.db.SetImplementWorkerSession(t.Context(), row.Parent, 1, row.Key, f.session.ID, implementClock))
	for range store.MaxImplementAttempts {
		_, err := f.db.RecordImplementFailure(t.Context(), row.Parent, 1, row.Key, "new failure", implementClock)
		mustProgress(t, err)
	}
	_, err = rt.parkStory(t.Context(), f.opts, ActivityRequest{})
	mustProgress(t, err)
	if forge.attemptCount() != 2 {
		t.Fatal("new parking event did not publish a new answer")
	}
	// A historical row with no matching receipt remains unlinked and unposted.
	row = implementWorkerRow(t, f)
	row.Revision++
	if err := rt.publishParkAnswer(t.Context(), f.opts, ActivityRequest{}, row, "historical"); err != nil {
		t.Fatal(err)
	}
	if forge.attemptCount() != 2 {
		t.Fatal("backfilled history")
	}
}

func TestImplementParkNoteLostResponseAndFailure(t *testing.T) {
	for _, name := range []string{"post failure", "lost response", "lost response after parking"} {
		lost := name != "post failure"
		t.Run(name, func(t *testing.T) {
			f, forge, rt := parkNoteFixture(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if lost {
				forge.afterPost = cancel
			} else {
				forge.failWith(503)
			}
			_, err := rt.parkStory(ctx, f.opts, ActivityRequest{})
			if err == nil {
				t.Fatal("delivery failure was hidden")
			}
			row := implementWorkerRow(t, f)
			if row.State != store.ImplementRunning {
				t.Fatal("failed publication parked ledger")
			}
			if name == "lost response after parking" {
				row, err = f.db.TransitionImplementStory(t.Context(), store.ImplementTransition{Parent: row.Parent, Generation: row.Generation, Key: row.Key, From: store.ImplementRunning, To: store.ImplementParked, ExpectedRevision: row.Revision, Reason: "retained reason", At: implementClock})
				mustProgress(t, err)
				notes, err := implementParkNotes(t.Context(), f.db, ImplementAuthority{Parent: rt.parentRef, Generation: 1, Stories: []ImplementStory{{Key: row.Key, IssueNumber: f.session.IssueNumber}}}, []store.ImplementStory{row})
				mustProgress(t, err)
				if len(notes) != 0 {
					t.Fatal("uncertain receipt became a link")
				}
			}
			forge.recover()
			_, retryErr := rt.parkStory(t.Context(), f.opts, ActivityRequest{})
			if lost {
				mustProgress(t, retryErr)
			} else if !errors.Is(retryErr, ErrReportReceiptUncertain) {
				t.Fatalf("expected uncertainty, got %v", retryErr)
			}
			if forge.attemptCount() != 1 {
				t.Fatal("uncertain publication duplicated")
			}
		})
	}
}

func TestImplementParkAnswerRedactionBeforeEncoding(t *testing.T) {
	f, _, _ := parkNoteFixture(t)
	for _, boundary := range []int{599, 1999} {
		node := agentresult.ResearchNode{Response: "blocked", Result: agentresult.Result{Reason: strings.Repeat("x", boundary) + testBotToken + "TAIL"}}
		body, err := renderImplementParkAnswer(t.Context(), f.opts.ResearchReporter, node)
		mustProgress(t, err)
		decoded := html.UnescapeString(body.String())
		if strings.Contains(decoded, testBotToken) || !strings.Contains(decoded, "TAIL") {
			t.Fatal("full answer did not redact before encoding")
		}
	}
}
