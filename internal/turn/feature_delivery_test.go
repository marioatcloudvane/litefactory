package turn

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
)

func TestParseDelivery(t *testing.T) {
	key, head := strings.Repeat("a", 64), strings.Repeat("b", 40)
	for _, tc := range []struct {
		body  string
		valid bool
	}{{"/deliver 1", true}, {"/merge 1 " + key + " 70 " + head + " main", true}, {"/deliver 0", false}, {"/merge 1 " + key + " 70 " + head + " release", false}, {"/merge 1 " + key + " 0 " + head + " main", false}, {"/merge 1 unknown 70 " + head + " main", false}, {"please merge everything", false}, {strings.Repeat("x", 513), false}} {
		t.Run(tc.body[:min(len(tc.body), 70)], func(t *testing.T) {
			got, err := ParseDelivery(tc.body)
			if (err == nil) != tc.valid {
				t.Fatalf("parsed %+v err=%v", got, err)
			}
		})
	}
}

type deliveryTestForge struct {
	*exceptionTestForge
	db                      *store.DB
	head                    string
	prs                     []github.DeliveryPR
	parent                  github.Issue
	creates, merges, closes int
	lose                    string
}

func (f *deliveryTestForge) GetDeliveryHead(context.Context, github.Repo, string) (string, error) {
	return f.head, nil
}
func (f *deliveryTestForge) ListDeliveryPRs(context.Context, github.Repo, string) ([]github.DeliveryPR, error) {
	return f.prs, nil
}
func (f *deliveryTestForge) GetDeliveryPR(_ context.Context, _ github.Repo, n int) (github.DeliveryPR, error) {
	for _, pr := range f.prs {
		if pr.Number == n {
			return pr, nil
		}
	}
	return github.DeliveryPR{}, errors.New("unknown PR")
}
func (f *deliveryTestForge) GetIssue(ctx context.Context, r github.Repo, n int) (github.Issue, error) {
	if n == testIssue {
		return f.parent, nil
	}
	return f.exceptionTestForge.GetIssue(ctx, r, n)
}
func (f *deliveryTestForge) CreateDeliveryPR(ctx context.Context, r github.Repo, branch, target string, title, body redact.Body) (github.DeliveryPR, error) {
	qs, err := f.db.ListDeliveries(ctx, f.sessionParent())
	if err != nil {
		return github.DeliveryPR{}, err
	}
	intent := false
	for _, q := range qs {
		if q.CreateAttempted && q.Title == title.String() && q.Body == body.String() {
			intent = true
		}
	}
	if !intent {
		return github.DeliveryPR{}, errors.New("create without durable concrete intent")
	}
	f.creates++
	pr := github.DeliveryPR{ID: int64(9000 + f.creates), CreatorID: 9, Number: 900 + f.creates, Title: title.String(), Body: body.String(), Head: f.head, Branch: branch, Target: target, HeadRepo: r.Owner + "/" + r.Name, BaseRepo: r.Owner + "/" + r.Name, State: "open"}
	f.prs = append(f.prs, pr)
	if f.lose == "create" {
		f.lose = ""
		return github.DeliveryPR{}, errors.New("lost PR create response")
	}
	return pr, nil
}
func (f *deliveryTestForge) sessionParent() string { return "github:acme/acme-api#123" }
func (f *deliveryTestForge) MergeDeliveryPR(ctx context.Context, _ github.Repo, n int, head string) error {
	qs, err := f.db.ListDeliveries(ctx, f.sessionParent())
	if err != nil {
		return err
	}
	authorized := false
	for _, q := range qs {
		if q.PRNumber == n && q.Head == head && q.MergeAttempted && q.ApprovalComment > 0 {
			authorized = true
		}
	}
	if !authorized {
		return errors.New("merge without durable exact approval")
	}
	f.merges++
	if f.lose == "denied" {
		f.lose = ""
		return errors.New("merge denied by required checks or conflicts")
	}
	for i := range f.prs {
		if f.prs[i].Number == n {
			if f.prs[i].Head != head {
				return errors.New("stale head")
			}
			if f.lose != "false success" {
				f.prs[i].Merged = true
				f.prs[i].MergeSHA = strings.Repeat("c", 40)
				f.prs[i].State = "closed"
			}
		}
	}
	if f.lose == "merge" {
		f.lose = ""
		return errors.New("lost merge response")
	}
	if f.lose == "false success" {
		f.lose = ""
	}
	return nil
}
func (f *deliveryTestForge) CloseDeliveredParent(ctx context.Context, _ github.Repo, n int, id int64) error {
	qs, err := f.db.ListDeliveries(ctx, f.sessionParent())
	if err != nil {
		return err
	}
	confirmed := false
	for _, q := range qs {
		if q.CloseAttempted && q.MergeConfirmed && q.MergeSHA != "" {
			confirmed = true
		}
	}
	if !confirmed {
		return errors.New("close before durable confirmed merge")
	}
	if f.parent.ID != id || f.parent.Number != n {
		return errors.New("parent identity mismatch")
	}
	f.closes++
	f.parent.State = "closed"
	f.parent.StateReason = "completed"
	if f.lose == "close" {
		f.lose = ""
		return errors.New("lost close response")
	}
	return nil
}
func (f *deliveryTestForge) PostComment(ctx context.Context, r github.Repo, n int, b redact.Body) (github.Comment, error) {
	if n != testIssue {
		return f.exceptionTestForge.PostComment(ctx, r, n, b)
	}
	c, err := f.progressForge.PostComment(ctx, r, n, b)
	if f.lose == "report" || (f.lose == "final report" && f.parent.State == "closed") {
		f.lose = ""
		return github.Comment{}, errors.New("lost delivery report")
	}
	return c, err
}
func featureDeliveryFixture(t *testing.T, ready bool) (*fixture, *deliveryTestForge) {
	t.Helper()
	f, base, _ := ownerExceptionFixture(t, "unverified")
	var freezeErr error
	f.session, freezeErr = f.db.FreezeSessionSlug(t.Context(), f.session.ID, testSlug, testBranch)
	mustProgress(t, freezeErr)
	forge := &deliveryTestForge{exceptionTestForge: base, db: f.db, head: strings.Repeat("b", 40), parent: github.Issue{ID: 1230, Number: testIssue, State: "open"}}
	p, err := NewStoryProgress(f.db, forge, f.opts.StoryProgress.renderer, 9, filepath.Join(t.TempDir(), "delivery-locks"))
	mustProgress(t, err)
	f.opts.StoryProgress = p
	if ready {
		mustProgress(t, p.ApplyOwnerException(t.Context(), ownerExceptionControl(f.session.TaskRef, "/waive 1 all -- accepted documented readiness risk")))
	}
	return f, forge
}
func deliveryControl(parent, body string) DeliveryControl {
	return DeliveryControl{Parent: parent, CommentID: 500, ActorID: 9, OwnerID: 9, Body: body}
}
func prepareTestDelivery(t *testing.T, f *fixture) store.DeliveryProposal {
	t.Helper()
	mustProgress(t, f.opts.StoryProgress.ApplyDelivery(t.Context(), deliveryControl(f.session.TaskRef, "/deliver 1")))
	qs, err := f.db.ListDeliveries(t.Context(), f.session.TaskRef)
	mustProgress(t, err)
	for _, q := range qs {
		if !q.Superseded {
			return q
		}
	}
	t.Fatal("no active delivery proposal")
	return store.DeliveryProposal{}
}
func mergeControl(q store.DeliveryProposal) DeliveryControl {
	c := deliveryControl(q.Parent, fmt.Sprintf("/merge %d %s %d %s main", q.Generation, q.Key, q.PRNumber, q.Head))
	c.CommentID = 700
	return c
}
func restartDelivery(t *testing.T, f *fixture, forge *deliveryTestForge) {
	t.Helper()
	p, err := NewStoryProgress(f.db, forge, f.opts.StoryProgress.renderer, 9, filepath.Join(t.TempDir(), "delivery-restart"))
	mustProgress(t, err)
	f.opts.StoryProgress = p
}

func TestDeliveryReadinessIncludesUnmanagedChildrenAndIndependentAcceptance(t *testing.T) {
	f, forge := featureDeliveryFixture(t, false)
	for n, i := range forge.children {
		i.State = "closed"
		forge.children[n] = i
	}
	forge.children[300] = github.Issue{ID: 3000, Number: 300, State: "open"}
	r, err := f.opts.StoryProgress.DeliveryReadiness(t.Context(), f.session.TaskRef)
	mustProgress(t, err)
	text := r.String()
	if !strings.Contains(text, "3000") || !strings.Contains(text, "US-1") || len(r.Blockers) == 0 {
		t.Fatalf("lost child or managed acceptance blocker %+v", r)
	}
	mustProgress(t, f.opts.StoryProgress.ApplyOwnerException(t.Context(), ownerExceptionControl(f.session.TaskRef, "/waive 1 all -- explicitly accept current scope")))
	r, err = f.opts.StoryProgress.DeliveryReadiness(t.Context(), f.session.TaskRef)
	mustProgress(t, err)
	if len(r.Blockers) != 0 || len(r.Exceptions) == 0 {
		t.Fatalf("exact readiness waiver ignored %+v", r)
	}
	forge.children[301] = github.Issue{ID: 3010, Number: 301, State: "open"}
	r, err = f.opts.StoryProgress.DeliveryReadiness(t.Context(), f.session.TaskRef)
	mustProgress(t, err)
	if !strings.Contains(r.String(), "3010") || len(r.Blockers) == 0 {
		t.Fatal("earlier waiver silently covered new child")
	}
}

func TestDeliveryRequiresConcreteApprovalBeforeMergeAndClosesOnlyAfterConfirmation(t *testing.T) {
	f, forge := featureDeliveryFixture(t, true)
	q := prepareTestDelivery(t, f)
	if !q.Proposed || q.PRNumber < 1 || q.Head != forge.head || q.Target != "main" || q.ApprovalComment != 0 || forge.merges != 0 || forge.parent.State != "open" {
		t.Fatalf("invalid proposal %+v", q)
	}
	for _, word := range []string{"closes #", "fixes #", "resolves #"} {
		if strings.Contains(strings.ToLower(q.Body), word) {
			t.Fatal("automatic closing keyword bypasses ordered closure")
		}
	}
	if q.Readiness == "" || q.Branch == "" {
		t.Fatal("proposal not concrete")
	}
	mustProgress(t, f.opts.StoryProgress.ApplyDelivery(t.Context(), mergeControl(q)))
	got, err := f.db.GetDelivery(t.Context(), q.Key)
	mustProgress(t, err)
	if !got.MergeConfirmed || !got.CloseAttempted || !got.Closed || !got.Reported || got.MergeSHA == "" || forge.parent.State != "closed" {
		t.Fatalf("delivery not confirmed %+v parent=%+v", got, forge.parent)
	}
	feature, err := f.db.GetManagedFeature(t.Context(), q.Parent)
	mustProgress(t, err)
	if feature.State != "completed" {
		t.Fatalf("confirmed integration did not settle feature: %+v", feature)
	}
	mustProgress(t, f.opts.StoryProgress.pollDelivery(t.Context(), q.Parent))
	before := len(forge.comments[testIssue])
	mustProgress(t, f.opts.StoryProgress.ApplyDelivery(t.Context(), mergeControl(q)))
	if forge.creates != 1 || forge.merges != 1 || forge.closes != 1 || len(forge.comments[testIssue]) != before {
		t.Fatal("duplicate approval duplicated delivery")
	}
}

func TestDeliveryLostResponsesRecoverWithoutDuplicateEffects(t *testing.T) {
	for _, lost := range []string{"create", "report", "merge", "close", "final report"} {
		t.Run(lost, func(t *testing.T) {
			f, forge := featureDeliveryFixture(t, true)
			var q store.DeliveryProposal
			if lost == "create" || lost == "report" {
				forge.lose = lost
				err := f.opts.StoryProgress.ApplyDelivery(t.Context(), deliveryControl(f.session.TaskRef, "/deliver 1"))
				if err == nil {
					t.Fatal("lost response ignored")
				}
			} else {
				q = prepareTestDelivery(t, f)
				forge.lose = lost
				if err := f.opts.StoryProgress.ApplyDelivery(t.Context(), mergeControl(q)); err == nil {
					t.Fatal("lost response ignored")
				}
			}
			restartDelivery(t, f, forge)
			mustProgress(t, f.opts.StoryProgress.pollDelivery(t.Context(), f.session.TaskRef))
			qs, err := f.db.ListDeliveries(t.Context(), f.session.TaskRef)
			mustProgress(t, err)
			q = qs[0]
			if lost == "create" || lost == "report" {
				mustProgress(t, f.opts.StoryProgress.ApplyDelivery(t.Context(), mergeControl(q)))
			}
			got, err := f.db.GetDelivery(t.Context(), q.Key)
			mustProgress(t, err)
			if !got.Closed || forge.creates != 1 || forge.merges != 1 || forge.closes != 1 {
				t.Fatalf("recovery duplicated or incomplete %+v create=%d merge=%d close=%d", got, forge.creates, forge.merges, forge.closes)
			}
		})
	}
}

func TestDeliveryFailuresNeverFabricateIntegration(t *testing.T) {
	for _, fault := range []string{"denied", "false success", "wrong target", "wrong repository", "wrong head"} {
		t.Run(fault, func(t *testing.T) {
			f, forge := featureDeliveryFixture(t, true)
			q := prepareTestDelivery(t, f)
			switch fault {
			case "wrong target":
				forge.prs[0].Target = "release"
			case "wrong repository":
				forge.prs[0].HeadRepo = "attacker/repo"
			case "wrong head":
				forge.prs[0].Head = strings.Repeat("d", 40)
			default:
				forge.lose = fault
			}
			if err := f.opts.StoryProgress.ApplyDelivery(t.Context(), mergeControl(q)); err != nil {
				t.Logf("delivery refused: %v", err)
			}
			got, err := f.db.GetDelivery(t.Context(), q.Key)
			mustProgress(t, err)
			if got.MergeConfirmed || got.Closed || forge.parent.State != "open" || forge.closes != 0 {
				t.Fatalf("false integration %+v", got)
			}
		})
	}
}

func TestDeliveryHeadChangesRequireFreshApprovalWithoutReopeningAcceptance(t *testing.T) {
	f, forge := featureDeliveryFixture(t, true)
	q := prepareTestDelivery(t, f)
	old, err := f.db.LatestAcceptance(t.Context(), f.session.TaskRef, 1, "US-1")
	mustProgress(t, err)
	forge.head = strings.Repeat("d", 40)
	forge.prs[0].Head = forge.head
	if err := f.opts.StoryProgress.ApplyDelivery(t.Context(), mergeControl(q)); err != nil {
		t.Logf("delivery refused: %v", err)
	}
	if forge.merges != 0 || forge.parent.State != "open" {
		t.Fatal("stale head approved")
	}
	now, err := f.db.LatestAcceptance(t.Context(), f.session.TaskRef, 1, "US-1")
	mustProgress(t, err)
	if now.ID != old.ID || now.Snapshot != old.Snapshot || !now.Accepted || forge.children[200].State != "closed" {
		t.Fatal("head change invalidated historical acceptance")
	}
	mustProgress(t, f.opts.StoryProgress.ApplyDelivery(t.Context(), deliveryControl(f.session.TaskRef, "/deliver 1")))
	qs, err := f.db.ListDeliveries(t.Context(), f.session.TaskRef)
	mustProgress(t, err)
	fresh := false
	for _, n := range qs {
		if n.Key != q.Key && n.Head == forge.head && n.ApprovalComment == 0 {
			fresh = true
		}
	}
	if !fresh {
		t.Fatal("changed head lacked new unapproved proposal")
	}
}

func TestDeliveryBusySessionAndInvalidControlsNeverDispatch(t *testing.T) {
	f, forge := featureDeliveryFixture(t, true)
	mustProgress(t, f.db.SetSessionState(t.Context(), f.session.ID, store.SessionRunning))
	if err := f.opts.StoryProgress.ApplyDelivery(t.Context(), deliveryControl(f.session.TaskRef, "/deliver 1")); err != nil {
		t.Logf("busy delivery refused: %v", err)
	}
	if forge.creates != 0 {
		t.Fatal("active implementation overlapped delivery")
	}
	mustProgress(t, f.db.SetSessionState(t.Context(), f.session.ID, store.SessionIdle))
	for i, body := range []string{"/deliver 0", "/merge bad", "/deliver 999"} {
		c := deliveryControl(f.session.TaskRef, body)
		c.CommentID = int64(600 + i)
		mustProgress(t, f.opts.StoryProgress.ApplyDelivery(t.Context(), c))
	}
	c := deliveryControl(f.session.TaskRef, "/deliver 1")
	c.ActorID = 8
	if err := f.opts.StoryProgress.ApplyDelivery(t.Context(), c); err == nil {
		t.Fatal("third party authorized delivery")
	}
	_ = prepareTestDelivery(t, f)
	if forge.merges != 0 || forge.closes != 0 {
		t.Fatal("readiness request implicitly authorized merge")
	}
}

func TestDeliveryLargeReadinessScopeRemainsReviewable(t *testing.T) {
	f, forge := featureDeliveryFixture(t, false)
	for n := 300; n < 1800; n++ {
		forge.children[n] = github.Issue{ID: int64(n * 10), Number: n, State: "open"}
	}
	c := deliveryControl(f.session.TaskRef, "/deliver 1")
	mustProgress(t, f.opts.StoryProgress.ApplyDelivery(t.Context(), c))
	if forge.creates != 0 || len(forge.comments[testIssue]) == 0 {
		t.Fatal("large blocking readiness created PR or lacked visible refusal")
	}
	mustProgress(t, f.opts.StoryProgress.ApplyOwnerException(t.Context(), ownerExceptionControl(f.session.TaskRef, "/waive 1 all -- approved current attached-child exceptions with original evidence retained")))
	q := prepareTestDelivery(t, f)
	if !q.Proposed || q.PRNumber < 1 || len(q.Readiness) < github.CommentSizeLimit || len(q.Body) > github.CommentSizeLimit {
		t.Fatalf("large exact scope not retained with bounded proposal: proposed=%v readiness=%d body=%d", q.Proposed, len(q.Readiness), len(q.Body))
	}
}

func TestDeliveryEditedClosingDirectiveAndConcurrentLockRefuseMerge(t *testing.T) {
	f, forge := featureDeliveryFixture(t, true)
	q := prepareTestDelivery(t, f)
	locked, err := f.db.AcquireSessionLock(t.Context(), f.session.ID, "running-save")
	mustProgress(t, err)
	if !locked {
		t.Fatal("fixture session lock unavailable")
	}
	if err := f.opts.StoryProgress.ApplyDelivery(t.Context(), mergeControl(q)); err != nil {
		t.Logf("delivery refused: %v", err)
	}
	if forge.merges != 0 {
		t.Fatal("delivery merged while session save held lock")
	}
	mustProgress(t, f.db.ReleaseSessionLock(t.Context(), f.session.ID, "running-save"))
	forge.prs[0].Body += "\nCloses #123"
	if err := f.opts.StoryProgress.ApplyDelivery(t.Context(), mergeControl(q)); err != nil {
		t.Logf("delivery refused: %v", err)
	}
	if forge.merges != 0 || forge.parent.State != "open" {
		t.Fatal("edited PR directive bypassed closure order")
	}
}
