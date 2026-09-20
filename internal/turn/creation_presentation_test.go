package turn

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
)

// These tests run the presentation against the real store and the fake forge.
// Real GitHub outage behaviour (a POST accepted whose response is lost, comment
// listing lag) is not observed here and remains unverified.

const presentationTrigger = 40

type presentationFixture struct {
	*fixture
	cs       *store.CreationStore
	run      store.Run
	proposal store.CreationProposal
	path     string
}

// newPresentationFixture seeds a reported run on the fixture's task and the
// bookkeeping row of a retained proposal it produced.
func newPresentationFixture(t *testing.T) *presentationFixture {
	t.Helper()
	f := newFixture(t, nil)
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	cs, err := store.NewCreationStore(f.db, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	run := f.seedRun(store.FirstSegmentOrdinal, storedClaudeSessionID, presentationTrigger, "research", store.RunReported)
	raw, err := json.Marshal(map[string]any{"version": 1, "repository": "acme/acme-api", "source_task": f.session.TaskRef, "relationship": "linked", "artifacts": []map[string]string{{"title": "Proposal", "body": "Exact publishable content"}}})
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	proposal := store.CreationProposal{Hash: hex.EncodeToString(sum[:]), Repository: "acme/acme-api", SourceTask: f.session.TaskRef, Relationship: "linked", Artifacts: 1, Envelope: raw}
	if err := cs.RetainProposalForRun(t.Context(), proposal, store.CreationPresentation{RunID: run.ID}); err != nil {
		t.Fatal(err)
	}
	return &presentationFixture{fixture: f, cs: cs, run: run, proposal: proposal, path: "/repos/acme/acme-api/issues/123/comments"}
}

func (p *presentationFixture) pending(t *testing.T) (store.CreationPresentation, bool) {
	t.Helper()
	row, err := p.cs.PendingCreationPresentation(t.Context(), p.run.ID)
	if errors.Is(err, store.ErrNotFound) {
		return store.CreationPresentation{}, false
	}
	if err != nil {
		t.Fatal(err)
	}
	return row, true
}

func (p *presentationFixture) deliveryKey(t *testing.T) string {
	t.Helper()
	ref := p.ref()
	return (&Reporter{botUserID: 77}).deliveryIntent(ref, ref, p.session.ID, presentationTrigger, 0, creationPresentationPurpose+p.proposal.Hash).Key
}

func TestReportCreationPresentationPostsOnceWithoutTouchingTheRunResult(t *testing.T) {
	p := newPresentationFixture(t)
	forge := newFakeForge(t)
	reporter := newReporter(t, p.fixture, forge, nil)
	if watermark(t, p.fixture) != presentationTrigger {
		t.Fatal("precondition: the run's trigger was consumed by its result report")
	}

	// Observe the bookkeeping state at the moment the POST is in flight.
	var mu sync.Mutex
	var atPost store.CreationPresentation
	var observed bool
	forge.afterPost = func() {
		row, err := p.cs.PendingCreationPresentation(t.Context(), p.run.ID)
		mu.Lock()
		defer mu.Unlock()
		if err == nil {
			atPost, observed = row, true
		}
	}

	posted, err := reporter.ReportCreationPresentation(t.Context(), p.run.ID)
	if err != nil || posted.Skipped || posted.CommentID == 0 {
		t.Fatalf("presentation not posted: %+v %v", posted, err)
	}
	mu.Lock()
	if !observed || atPost.DeliveryKey != p.deliveryKey(t) || atPost.Presented {
		t.Fatalf("at POST time the key must be bound and presented=0: observed=%v %+v", observed, atPost)
	}
	mu.Unlock()

	post := forge.only(t)
	if post.path != p.path {
		t.Fatalf("posted to %s", post.path)
	}
	for _, want := range []string{string(p.proposal.Envelope), p.proposal.Hash, "`/mcp-create " + p.proposal.Hash + " linked`", "<!-- lf:delivery v=1 key=" + p.deliveryKey(t) + " -->"} {
		if !strings.Contains(post.body, want) {
			t.Errorf("comment lacks %q", want)
		}
	}

	delivery, err := p.db.GetReportDelivery(t.Context(), p.deliveryKey(t))
	if err != nil {
		t.Fatal(err)
	}
	if delivery.RunID != 0 || delivery.Purpose != "creation-proposal:"+p.proposal.Hash || !delivery.Completed || delivery.CommentID != posted.CommentID {
		t.Fatalf("delivery = %+v", delivery)
	}
	run, err := p.db.GetRun(t.Context(), p.run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if run.ResultCommentID != p.run.ResultCommentID || run.Status != store.RunReported {
		t.Fatalf("run result changed: %+v -> %+v", p.run, run)
	}
	if watermark(t, p.fixture) != presentationTrigger {
		t.Fatal("presentation moved the watermark")
	}
	if _, owed := p.pending(t); owed {
		t.Fatal("presented flag not set after confirmed completion")
	}

	again, err := reporter.ReportCreationPresentation(t.Context(), p.run.ID)
	if err != nil || !again.Skipped || forge.attemptCount() != 1 {
		t.Fatalf("second presentation: %+v %v attempts=%d", again, err, forge.attemptCount())
	}
}

func TestReportCreationPresentationDoesNotConsumeAFreshOwnerComment(t *testing.T) {
	p := newPresentationFixture(t)
	forge := newFakeForge(t)
	const fresh = presentationTrigger + 10
	if _, err := newReporter(t, p.fixture, forge, nil).ReportCreationPresentation(t.Context(), p.run.ID); err != nil {
		t.Fatal(err)
	}
	// The owner's newer comment stays ahead of the watermark.
	if wm := watermark(t, p.fixture); wm != presentationTrigger || wm >= fresh {
		t.Fatalf("watermark %d must stay at the run's trigger %d, short of the fresh comment %d", wm, presentationTrigger, fresh)
	}
	consumed, err := (&Reporter{db: p.db}).consumed(t.Context(), p.session.ID, fresh)
	if err != nil || consumed {
		t.Fatalf("fresh owner comment consumed=%v err=%v", consumed, err)
	}
}

func TestReportCreationPresentationRecoversPostThenCrashWithOneComment(t *testing.T) {
	p := newPresentationFixture(t)
	forge := newFakeForge(t)
	forge.afterPost = func() {
		if err := p.db.Close(); err != nil {
			t.Error(err)
		}
	}
	posted, err := newReporter(t, p.fixture, forge, nil).ReportCreationPresentation(t.Context(), p.run.ID)
	var pending *DeliveryError
	if err == nil || !errors.As(err, &pending) || posted.CommentID == 0 {
		t.Fatalf("lost confirmation not exposed: %+v %v", posted, err)
	}
	original := forge.only(t)

	reopened, err := store.Open(t.Context(), p.paths.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	if err := reopened.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	*p.db = *reopened
	forge.afterPost = nil
	row, owed := p.pending(t)
	if !owed || row.DeliveryKey != p.deliveryKey(t) {
		t.Fatalf("crash left row %+v owed=%v; want the bound key and presented=0", row, owed)
	}

	// A renderer that always fails proves recovery does not rerender: it matches
	// the existing comment by delivery marker and confirms it.
	recovered, err := newReporter(t, p.fixture, forge, stubRenderer{err: errors.New("must not rerender")}).ReportCreationPresentation(t.Context(), p.run.ID)
	if err != nil || recovered.CommentID != posted.CommentID {
		t.Fatalf("recovery: %+v %v", recovered, err)
	}
	if forge.attemptCount() != 1 || len(forge.all()) != 1 || forge.only(t).body != original.body {
		t.Fatal("recovery reposted or changed the comment")
	}
	if _, owed := p.pending(t); owed {
		t.Fatal("presented not set after recovery")
	}
}

func TestReportCreationPresentationFailureLeavesItOwed(t *testing.T) {
	p := newPresentationFixture(t)
	forge := newFakeForge(t)
	forge.failWith(500)
	if _, err := newReporter(t, p.fixture, forge, nil).ReportCreationPresentation(t.Context(), p.run.ID); err == nil {
		t.Fatal("a failed POST reported success")
	}
	row, owed := p.pending(t)
	if !owed || row.Presented || row.DeliveryKey != p.deliveryKey(t) {
		t.Fatalf("failure must leave presented=0 with the key bound: %+v owed=%v", row, owed)
	}
	if len(forge.all()) != 0 {
		t.Fatal("a comment exists despite the failure")
	}
}

// The daemon's existing report recovery can complete the pending delivery
// before the hook runs again. The presented flag is then still owed, and the
// next call finalises it without a second comment.
func TestReportCreationPresentationFinalisesDeliveryCompletedByReportRecovery(t *testing.T) {
	p := newPresentationFixture(t)
	forge := newFakeForge(t)
	forge.afterPost = func() {
		if err := p.db.Close(); err != nil {
			t.Error(err)
		}
	}
	posted, err := newReporter(t, p.fixture, forge, nil).ReportCreationPresentation(t.Context(), p.run.ID)
	if err == nil || posted.CommentID == 0 {
		t.Fatalf("lost confirmation not exposed: %+v %v", posted, err)
	}
	reopened, err := store.Open(t.Context(), p.paths.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	if err := reopened.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	*p.db = *reopened
	forge.afterPost = nil

	reporter := newReporter(t, p.fixture, forge, stubRenderer{err: errors.New("must not rerender")})
	if _, pending, err := reporter.ReportPending(t.Context(), p.session.ID, presentationTrigger); err != nil || !pending {
		t.Fatalf("report recovery: pending=%v %v", pending, err)
	}
	if _, owed := p.pending(t); !owed {
		t.Fatal("report recovery alone must not mark the proposal presented")
	}
	final, err := reporter.ReportCreationPresentation(t.Context(), p.run.ID)
	if err != nil || !final.Skipped || forge.attemptCount() != 1 {
		t.Fatalf("finalise: %+v %v attempts=%d", final, err, forge.attemptCount())
	}
	if _, owed := p.pending(t); owed {
		t.Fatal("presented not set")
	}
}

func TestReportCreationPresentationSkipsLegacyProbeButOrdinaryRunlessRepliesKeepIt(t *testing.T) {
	p := newPresentationFixture(t)
	forge := newFakeForge(t)
	ref := p.ref()
	// A pre-journal bot comment: task-only reply marker, no delivery marker.
	forge.mu.Lock()
	forge.posts = append(forge.posts, forgePost{path: p.path, id: 4001, body: report.ReplyMarker(ref) + "\nold pre-journal reply"})
	forge.mu.Unlock()
	reporter := newReporter(t, p.fixture, forge, nil)

	_, err := reporter.ReportReply(t.Context(), ReplyRequest{Ref: ref, SessionID: p.session.ID, CommentID: presentationTrigger + 20, Command: ParseCommand("/unknown command")})
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("ordinary runless reply skipped the probe: %v", err)
	}
	if forge.attemptCount() != 0 {
		t.Fatal("ordinary runless reply posted despite ambiguity")
	}

	posted, err := reporter.ReportCreationPresentation(t.Context(), p.run.ID)
	if err != nil || posted.CommentID == 0 || forge.attemptCount() != 1 {
		t.Fatalf("presentation blocked by a legacy comment: %+v %v", posted, err)
	}
	if _, owed := p.pending(t); owed {
		t.Fatal("presented not set")
	}
}

func TestReportCreationPresentationNothingOwed(t *testing.T) {
	p := newPresentationFixture(t)
	forge := newFakeForge(t)
	reporter := newReporter(t, p.fixture, forge, nil)
	other := p.seedRun(store.FirstSegmentOrdinal, storedClaudeSessionID, presentationTrigger+5, "research", store.RunReported)
	posted, err := reporter.ReportCreationPresentation(t.Context(), other.ID)
	if err != nil || !posted.Skipped || forge.attemptCount() != 0 {
		t.Fatalf("run with no proposal: %+v %v", posted, err)
	}
	if _, err := reporter.ReportCreationPresentation(t.Context(), 0); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("run id 0: %v", err)
	}
}
