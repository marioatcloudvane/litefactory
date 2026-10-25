package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
	"github.com/cloudvane/litefactory/internal/turn"
)

func publicationBatch(t *testing.T, f *publicationIO, comment int64, relationship string, count int) (turn.OwnerGrantControl, string, int64) {
	t.Helper()
	artifacts := make([]submission.CreationArtifact, count)
	for i := range artifacts {
		artifacts[i] = submission.CreationArtifact{Title: fmt.Sprintf("Approved %d draft %d", comment, i), Body: fmt.Sprintf("Exact reviewed content %d/%d", comment, i)}
		if relationship == "attach" {
			artifacts[i].ParentIssue = 278
		}
	}
	raw, err := json.Marshal(submission.CreationEnvelope{Version: 1, Repository: "owner/api", SourceTask: f.session.TaskRef, Relationship: relationship, Artifacts: artifacts})
	publicationMust(t, err)
	control := ownerApproval(f, comment, "/mcp-create prepare "+string(raw), f.now)
	control.Mode = "research"
	prepared, err := f.runtime.PrepareCreationProposal(t.Context(), control)
	publicationMust(t, err)
	approval := ownerApproval(f, comment+1, "/mcp-create "+prepared.Hash+" "+relationship, f.now)
	admitted, err := f.runtime.AdmitCreationApproval(t.Context(), approval)
	publicationMust(t, err)
	return control, prepared.Hash, admitted
}

// Keep twenty genuinely confirmed issue identities in the same repository and
// current 24-hour window, without spending any of the new batch's provider calls.
func seedPublicationVolume(t *testing.T, f *publicationIO) {
	t.Helper()
	for batch := 0; batch < 4; batch++ {
		_, _, intent := publicationBatch(t, f, int64(1000+batch*2), "linked", 5)
		pub, _, err := f.cs.ClaimOwnerPublication(t.Context(), intent, true)
		publicationMust(t, err)
		if pub.State != "dispatched" {
			t.Fatalf("historical batch not admitted: %+v", pub)
		}
		for i := 0; i < 5; i++ {
			begun, err := f.db.BeginOwnerArtifact(t.Context(), intent, i)
			publicationMust(t, err)
			if !begun {
				t.Fatal("historical artifact already begun")
			}
			publicationMust(t, f.db.SaveOwnerArtifact(t.Context(), intent, store.OwnerPublicationArtifact{Ordinal: i, IssueID: int64(7000 + batch*5 + i), IssueNumber: 700 + batch*5 + i, Relationship: "linked"}))
		}
		publicationMust(t, f.cs.FinishOwnerPublication(t.Context(), intent, "confirmed"))
	}
}

// The callback runs after the provider returns a known result, so cancellation
// does not erase a response the publisher already received.
type publicationCreationHook struct {
	turn.CreationForge
	afterCreate  func()
	afterAttach  func()
	loseResponse bool
}

func (h publicationCreationHook) CreateIssue(ctx context.Context, repo github.Repo, title, body redact.Body) (github.Issue, error) {
	issue, err := h.CreationForge.CreateIssue(ctx, repo, title, body)
	if h.afterCreate != nil {
		h.afterCreate()
	}
	if h.loseResponse {
		return github.Issue{}, errors.New("provider write outcome unknown")
	}
	return issue, err
}

func (h publicationCreationHook) AddSubIssue(ctx context.Context, repo github.Repo, parent int, issueID int64) error {
	err := h.CreationForge.AddSubIssue(ctx, repo, parent, issueID)
	if h.afterAttach != nil {
		h.afterAttach()
	}
	return err
}

// The wrapper observes actual attachment writes and can change authority at the
// provider boundary, after an in-flight creation has settled.
func publicationProvider(t *testing.T, f *publicationIO, afterCreate func(), attachments *int) {
	t.Helper()
	f.runtime.Creation = publicationCreationHook{CreationForge: f.runtime.Creation, afterCreate: afterCreate}
	original := f.server.Config.Handler
	f.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/sub_issues") && r.Method == http.MethodPost {
			*attachments++
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":9001,"number":901}`))
			return
		}
		if strings.HasSuffix(r.URL.Path, "/parent") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":2780,"number":278}`))
			return
		}
		original.ServeHTTP(w, r)
	})
}

func TestOwnerPublicationAcceptedContinuationAfterExpiry(t *testing.T) {
	for _, tc := range []struct {
		name, relationship, retained, want string
		known                              bool
		creates, attaches                  int
	}{
		{"confirmed attachment", "attach", "attached", "confirmed", true, 1, 1},
		{"confirmed linked issue", "linked", "linked", "confirmed", true, 1, 0},
		{"unknown attachment", "attach", "uncertain", "uncertain", true, 0, 0},
		{"unknown creation", "linked", "", "uncertain", false, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newPublicationIO(t)
			f.createOK = true
			_, _, intent := publicationBatch(t, f, 80, tc.relationship, 2)
			_, _, err := f.cs.ClaimOwnerPublication(t.Context(), intent, true)
			publicationMust(t, err)
			begun, err := f.db.BeginOwnerArtifact(t.Context(), intent, 0)
			publicationMust(t, err)
			if !begun {
				t.Fatal("not begun")
			}
			if tc.known {
				publicationMust(t, f.db.SaveOwnerArtifact(t.Context(), intent, store.OwnerPublicationArtifact{Ordinal: 0, IssueID: 8800, IssueNumber: 880, Relationship: tc.retained}))
			}
			f.now = f.now.Add(31 * time.Minute)
			rejectionRestart(t, f)
			attachments := 0
			publicationProvider(t, f, nil, &attachments)
			out, err := f.runtime.PublishApprovedCreation(t.Context(), intent)
			publicationMust(t, err)
			if out.State != tc.want || f.creationPosts != tc.creates || attachments != tc.attaches {
				t.Fatalf("out=%+v creates=%d attachments=%d", out, f.creationPosts, attachments)
			}
			if tc.known && (len(out.Issues) == 0 || out.Issues[0].Number != 880) {
				t.Fatalf("lost known identity: %+v", out)
			}
			if tc.name == "unknown attachment" && !strings.Contains(out.Reason, "attach") {
				t.Fatalf("missing attachment uncertainty: %+v", out)
			}
			pub, _, err := f.cs.ClaimOwnerPublication(t.Context(), intent, true)
			publicationMust(t, err)
			if pub.State != tc.want || pub.Reason != out.Reason {
				t.Fatalf("retained %+v", pub)
			}
			_, err = f.runtime.PrepareCreationProposal(t.Context(), successfulPreparationControl(t, f, 90, "Replacement after terminal outcome"))
			publicationMust(t, err)
			replay, err := f.runtime.PublishApprovedCreation(t.Context(), intent)
			if err == nil && (replay.State != tc.want || !reflect.DeepEqual(replay.Issues, out.Issues)) {
				t.Fatalf("historical outcome changed: %+v", replay)
			}
			publicationMust(t, err)
			if f.creationPosts != tc.creates || attachments != tc.attaches {
				t.Fatal("terminal replay wrote again")
			}
		})
	}
}

func TestOwnerPublicationStopsRemainingWrites(t *testing.T) {
	for _, tc := range []struct {
		name, relationship      string
		before, cancel, restore bool
		wantCreates             int
	}{
		{name: "superseded before resume", relationship: "linked", before: true},
		{name: "original bytes at newer revision", relationship: "linked", before: true, restore: true},
		{name: "superseded between artifacts", relationship: "linked", wantCreates: 1},
		{name: "superseded before required attachment", relationship: "attach", wantCreates: 1},
		{name: "cancelled before resume", relationship: "linked", before: true, cancel: true},
		{name: "cancelled between artifacts", relationship: "linked", cancel: true, wantCreates: 1},
		{name: "cancelled before attachment", relationship: "attach", cancel: true, wantCreates: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newPublicationIO(t)
			f.createOK = true
			control, _, intent := publicationBatch(t, f, 80, tc.relationship, 2)
			_, _, err := f.cs.ClaimOwnerPublication(t.Context(), intent, true)
			publicationMust(t, err)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			change := func() {
				if tc.cancel {
					cancel()
					return
				}
				f.now = f.now.Add(time.Second)
				_, err := f.runtime.PrepareCreationProposal(t.Context(), successfulPreparationControl(t, f, 90, "Superseding proposal"))
				publicationMust(t, err)
				if tc.restore {
					control.CommentID = 92
					control.CreatedAt = f.now
					_, err = f.runtime.PrepareCreationProposal(t.Context(), control)
					publicationMust(t, err)
				}
			}
			if tc.before {
				change()
			}
			attachments := 0
			publicationProvider(t, f, func() {
				if !tc.before && f.creationPosts == 1 {
					change()
				}
			}, &attachments)
			out, err := f.runtime.PublishApprovedCreation(ctx, intent)
			publicationMust(t, err)
			if out.State != "uncertain" || out.Reason == "" || f.creationPosts != tc.wantCreates || attachments != 0 {
				t.Fatalf("out=%+v creates=%d attachments=%d", out, f.creationPosts, attachments)
			}
			retained, err := f.db.OwnerArtifacts(t.Context(), intent)
			publicationMust(t, err)
			if tc.wantCreates == 1 && (len(retained) == 0 || retained[0].IssueID != 9001 || retained[0].IssueNumber != 901) {
				t.Fatalf("lost in-flight result: %+v", retained)
			}
			pub, _, err := f.cs.ClaimOwnerPublication(t.Context(), intent, true)
			publicationMust(t, err)
			if pub.State != "uncertain" || pub.Reason != out.Reason {
				t.Fatalf("not stopped: %+v", pub)
			}
			// Reinstating the original bytes cannot resurrect the stopped approval.
			control.CommentID = 94
			control.CreatedAt = f.now
			_, err = f.runtime.PrepareCreationProposal(t.Context(), control)
			publicationMust(t, err)
			replay, err := f.runtime.PublishApprovedCreation(t.Context(), intent)
			publicationMust(t, err)
			if replay.State != pub.State || replay.Reason != out.Reason || !reflect.DeepEqual(replay.Issues, out.Issues) || f.creationPosts != tc.wantCreates || attachments != 0 {
				t.Fatalf("replay revived writes: %+v", replay)
			}
		})
	}
}

// Cancellation while a write is in flight must retain its uncertainty rather
// than infer an identity from the fact that the fake provider received it.
func TestOwnerPublicationCancelledUnknownCreation(t *testing.T) {
	f := newPublicationIO(t)
	f.createOK = true
	_, _, intent := publicationBatch(t, f, 80, "linked", 2)
	_, _, err := f.cs.ClaimOwnerPublication(t.Context(), intent, true)
	publicationMust(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	f.runtime.Creation = publicationCreationHook{CreationForge: f.runtime.Creation, afterCreate: cancel, loseResponse: true}
	out, err := f.runtime.PublishApprovedCreation(ctx, intent)
	publicationMust(t, err)
	if out.State != "uncertain" || f.creationPosts != 1 {
		t.Fatalf("out=%+v creates=%d", out, f.creationPosts)
	}
	artifacts, err := f.db.OwnerArtifacts(t.Context(), intent)
	publicationMust(t, err)
	if len(artifacts) != 1 || artifacts[0].IssueID != 0 {
		t.Fatalf("unknown result changed: %+v", artifacts)
	}
	replay, err := f.runtime.PublishApprovedCreation(t.Context(), intent)
	publicationMust(t, err)
	if replay.State != "uncertain" || f.creationPosts != 1 {
		t.Fatalf("unknown effect retried: %+v creates=%d", replay, f.creationPosts)
	}
}

func TestOwnerPublicationCancelledAfterKnownAttachment(t *testing.T) {
	f := newPublicationIO(t)
	f.createOK = true
	_, _, intent := publicationBatch(t, f, 80, "attach", 2)
	_, _, err := f.cs.ClaimOwnerPublication(t.Context(), intent, true)
	publicationMust(t, err)
	attachments := 0
	publicationProvider(t, f, nil, &attachments)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	f.runtime.Creation = publicationCreationHook{CreationForge: f.runtime.Creation, afterAttach: cancel}
	out, err := f.runtime.PublishApprovedCreation(ctx, intent)
	publicationMust(t, err)
	if out.State != "uncertain" || f.creationPosts != 1 || attachments != 1 {
		t.Fatalf("out=%+v creates=%d attachments=%d", out, f.creationPosts, attachments)
	}
	artifacts, err := f.db.OwnerArtifacts(t.Context(), intent)
	publicationMust(t, err)
	if len(artifacts) != 1 || artifacts[0].IssueID != 9001 || artifacts[0].Relationship != "attached" {
		t.Fatalf("lost confirmed attachment: %+v", artifacts)
	}
	replay, err := f.runtime.PublishApprovedCreation(t.Context(), intent)
	publicationMust(t, err)
	if replay.State != out.State || replay.Reason != out.Reason || !reflect.DeepEqual(replay.Issues, out.Issues) || f.creationPosts != 1 || attachments != 1 {
		t.Fatalf("replay changed outcome: %+v", replay)
	}
}
