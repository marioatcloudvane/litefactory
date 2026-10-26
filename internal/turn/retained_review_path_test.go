package turn

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	ar "github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
)

type retainedReviewRecoveryRenderer struct{ Renderer }

func (retainedReviewRecoveryRenderer) RenderRun(context.Context, report.Run) (redact.Body, error) {
	return redact.Body{}, errors.New("original renderer unavailable")
}

func TestRetainedReviewSubmissionToOwnerRecovery(t *testing.T) {
	for _, interruption := range []string{"content", "summary"} {
		t.Run(interruption, func(t *testing.T) {
			f := newFixture(t, nil)
			attempt := agentResultRun(t, f.db, f.session, "research", "review-path", testComment)
			service, err := BindAgentResultService(t.Context(), AgentResultServiceOptions{
				DB: f.db, Attempt: attempt,
				Resolve: func(context.Context) (ar.Context, error) {
					return ar.Context{SchemaVersion: 1, WorkflowVersion: 2, Mode: "research", QuestionsAllowed: true}, nil
				},
				ReadTargets: ar.EditTargets("research"), EditTargets: ar.EditTargets("research"),
			})
			if err != nil {
				t.Fatal(err)
			}
			accepted, err := service.Submit(t.Context(), "accepted", resultCreate("research"))
			if err != nil || accepted.Snapshot == nil {
				t.Fatalf("accepted baseline: %+v %v", accepted, err)
			}
			before, err := f.db.GetResultRevision(t.Context(), service.Binding(), 1)
			if err != nil {
				t.Fatal(err)
			}
			var useful strings.Builder
			for i := range 4000 {
				fmt.Fprintf(&useful, "Finding %04d: useful evidence and uncertainty remain visible. λ\n", i)
			}
			useful.WriteString("Credential: " + testBotToken + "\nFinal caveat: evidence is incomplete.")
			payload, err := json.Marshal(map[string]any{"result": map[string]any{
				"outcome": "incomplete", "reason": "Review still required", "comment": useful.String(),
				"payload": map[string]any{"research": map[string]any{"request": "Inspect boundary"}},
			}})
			if err != nil {
				t.Fatal(err)
			}
			source := append([]byte(" \n```json\n"), payload...)
			source = append(source, []byte("\n``` \n")...)
			rejected, err := service.Submit(t.Context(), "review", source)
			if err != nil || rejected.Validation.Valid || rejected.Review == nil || rejected.Snapshot != nil {
				t.Fatalf("rejected review: %+v %v", rejected, err)
			}
			if !bytes.Equal(rejected.Review.Source, source) || !rejected.Review.DisplaySimplified {
				t.Fatal("review changed source or failed to simplify its enclosing fence")
			}
			replay, err := service.Submit(t.Context(), "review", source)
			if err != nil || replay.Receipt.RequestHash != rejected.Receipt.RequestHash {
				t.Fatal("exact replay lost receipt", err)
			}
			if _, err := service.Submit(t.Context(), "review", append(bytes.Clone(source), ' ')); !errors.Is(err, store.ErrResultCorrelation) {
				t.Fatal("changed submission reused request identity", err)
			}
			run, err := f.db.FinishRun(t.Context(), attempt.RunID, store.RunOutcome{Status: store.RunFailed})
			if err != nil {
				t.Fatal(err)
			}
			forge := newFakeForge(t)
			reporter := newReporter(t, f, forge, nil)
			request := RunReport{Run: report.Run{Ref: f.ref(), Ordinal: run.Ordinal, Result: &agent.RunResult{ResultText: rejected.Review.Markdown()}}, SessionID: f.session.ID, CommentID: testComment, RunID: run.ID}
			forge.afterPost = func() {
				posts := forge.all()
				summary := len(posts) > 0 && strings.Contains(posts[len(posts)-1].body, "Complete retained content (all parts confirmed)")
				if interruption == "content" && forge.attemptCount() == 2 || interruption == "summary" && summary {
					if err := f.db.Close(); err != nil {
						t.Error(err)
					}
				}
			}
			if _, err := reporter.ReportRun(t.Context(), request); err == nil {
				t.Fatal("lost multipart receipt was reported as complete")
			}
			reopened, err := store.Open(t.Context(), f.paths.DBPath())
			if err != nil {
				t.Fatal(err)
			}
			*f.db = *reopened
			forge.afterPost = nil
			if watermark(t, f) != 0 {
				t.Fatal("partial multipart delivery consumed source")
			}
			interrupted, err := f.db.GetRun(t.Context(), run.ID)
			if err != nil || interrupted.Status == store.RunReported {
				t.Fatal("partial multipart delivery completed run", err)
			}
			// Recovery uses the durable rendered source, including when the caller can
			// no longer render the original submission.
			recovery := newReporter(t, f, forge, retainedReviewRecoveryRenderer{Renderer: reporter.renderer})
			if _, pending, err := recovery.ReportPending(t.Context(), f.session.ID, testComment); err != nil || !pending {
				t.Fatalf("resume multipart delivery: pending=%v err=%v", pending, err)
			}
			posts := forge.all()
			if len(posts) < 4 || forge.attemptCount() != len(posts) {
				t.Fatalf("incomplete or duplicate multipart delivery: posts=%d attempts=%d", len(posts), forge.attemptCount())
			}
			var delivered strings.Builder
			for i, post := range posts {
				if strings.Contains(post.body, testBotToken) {
					t.Fatal("owner delivery exposed a credential")
				}
				if i == len(posts)-1 {
					continue
				}
				prefix := fmt.Sprintf("Full retained result — part %d/%d\n\n", i+1, len(posts)-1)
				if !strings.HasPrefix(post.body, prefix) {
					t.Fatalf("full-content part %d has no ordered identity", i+1)
				}
				chunk, _, found := strings.Cut(strings.TrimPrefix(post.body, prefix), "\n\n<!-- lf:delivery")
				if !found {
					t.Fatal("full-content part lost durable delivery identity")
				}
				chunk = strings.TrimPrefix(chunk, "<!-- lf:part-context-open --><pre>")
				chunk = strings.TrimSuffix(chunk, "</pre><!-- lf:part-context-close -->")
				delivered.WriteString(chunk)
			}
			complete := delivered.String()
			expected, err := redact.New(redact.Options{Token: secrets.NewToken(testBotToken), Environ: []string{}}).Apply(t.Context(), rejected.Review.Markdown(), 16<<20)
			if err != nil || expected.Truncated() || complete != expected.String() {
				t.Fatalf("multipart owner review differs from complete redacted retained source: got %d bytes, want %d: %v", len(complete), len(expected.String()), err)
			}
			if !strings.Contains(complete, "Final caveat: evidence is incomplete.") {
				t.Fatal("full review lost final limitation")
			}
			for _, part := range posts[:len(posts)-1] {
				if !strings.Contains(posts[len(posts)-1].body, fmt.Sprintf("#issuecomment-%d", part.id)) {
					t.Fatal("short report did not link every full-content part")
				}
			}
			if watermark(t, f) != testComment {
				t.Fatal("confirmed full delivery did not consume source")
			}
			after, err := f.db.GetResultRevision(t.Context(), service.Binding(), 1)
			if err != nil || !bytes.Equal(before.Snapshot, after.Snapshot) {
				t.Fatal("review delivery changed accepted result", err)
			}
			revisions, err := f.db.ResultRevisionNumbers(t.Context(), service.Binding())
			if err != nil || len(revisions) != 1 {
				t.Fatal("review delivery advanced accepted revision", err)
			}
			if _, pending, err := recovery.ReportPending(t.Context(), f.session.ID, testComment); err != nil || pending {
				t.Fatalf("complete delivery remained pending: %v %v", pending, err)
			}
		})
	}
}
