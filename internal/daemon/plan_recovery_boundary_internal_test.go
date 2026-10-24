package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

// Exercises daemon admission, child observation and retained-effect recovery with
// no owner input. HTTP is only the external forge; storage and policy are real.
func TestPlanRecoveryDaemonBoundaryNoCommentRestart(t *testing.T) {
	for _, kind := range []string{"manual-close", "retire-gap", "unknown-create", "shared-failure"} {
		t.Run(kind, func(t *testing.T) {
			f := newPublicationIO(t)
			children := map[int]map[string]any{}
			closedLost := false
			failParent := false
			mutations := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if failParent {
					http.Error(w, `{"message":"service unavailable"}`, http.StatusServiceUnavailable)
					return
				}
				parent := map[string]any{"id": 2780, "number": 278, "state": "open"}
				base := "/repos/owner/api/issues"
				switch {
				case r.URL.Path == base+"/278":
					if failParent {
						http.Error(w, `{"message":"service unavailable"}`, http.StatusServiceUnavailable)
						return
					}
					_ = json.NewEncoder(w).Encode(parent)
				case r.URL.Path == base && r.Method == http.MethodPost:
					mutations++
					var child map[string]any
					_ = json.NewDecoder(r.Body).Decode(&child)
					n := 400 + len(children)
					child["id"] = n * 10
					child["number"] = n
					child["state"] = "open"
					children[n] = child
					if kind == "unknown-create" && n == 400 {
						http.Error(w, `{"message":"lost create"}`, http.StatusInternalServerError)
						return
					}
					_ = json.NewEncoder(w).Encode(child)
				case r.URL.Path == base:
					list := []any{}
					for n, child := range children {
						if kind != "unknown-create" || n != 400 {
							list = append(list, child)
						}
					}
					_ = json.NewEncoder(w).Encode(list)
				case strings.HasSuffix(r.URL.Path, "/sub_issues"):
					_, _ = w.Write([]byte(`{}`))
				case strings.HasSuffix(r.URL.Path, "/parent"):
					_ = json.NewEncoder(w).Encode(parent)
				case strings.HasSuffix(r.URL.Path, "/comments"):
					_, _ = w.Write([]byte(`[]`))
				default:
					n, _ := strconv.Atoi(strings.TrimPrefix(r.URL.Path, base+"/"))
					child := children[n]
					if child == nil {
						http.NotFound(w, r)
						return
					}
					if r.Method == http.MethodPatch {
						mutations++
						var patch map[string]any
						_ = json.NewDecoder(r.Body).Decode(&patch)
						for k, v := range patch {
							child[k] = v
						}
						if closedLost {
							closedLost = false
							http.Error(w, `{"message":"lost close"}`, http.StatusInternalServerError)
							return
						}
					}
					_ = json.NewEncoder(w).Encode(child)
				}
			}))
			defer srv.Close()
			client, err := github.New(github.Options{APIBase: srv.URL + "/", HTTPClient: srv.Client(), Token: secrets.NewToken("fixture")})
			publicationMust(t, err)
			f.d.forge = client
			f.d.discovery.tick(f.now)
			f.d.discovery.observe(github.RequestObservation{CoreRateValid: true, Limit: 5000, Remaining: 5000, Reset: f.now.Add(time.Hour), StatusCode: 200})
			publicationMust(t, os.MkdirAll(f.runtime.Paths.StoryProgressLocksDir(), 0700))
			activity := turn.PlanStoryActivity{DB: f.db, Forge: client, Redactor: f.runtime.Redactor, LockDir: t.TempDir()}
			authority := turn.PlanStoryAuthority{Parent: f.session.TaskRef, Repo: github.Repo{Owner: "owner", Name: "api"}, ParentNumber: 278, ParentID: 2780, OwnerID: 10, Stage: "story"}
			var firstRevision string
			for _, key := range []string{"affected", "healthy"} {
				authority.SelectedKey = key
				authority.Candidate = key + "-candidate"
				request := graphexec.PlanStoryRequest{RequestID: key + "-create", Operation: "create", Stage: "story", Candidate: authority.Candidate, SharedRevision: "shared", Draft: planbrief.PlanDraft{Key: key, Name: key, UserStory: planbrief.UserStoryContent{Actor: "owner", Want: "usable stories", Benefit: "independent progress"}, Problem: "Plan availability", InScope: []string{"shaping"}, NotInScope: []string{"deployment"}}}
				receipt, applyErr := activity.Apply(t.Context(), authority, request)
				if kind == "unknown-create" && key == "affected" {
					if applyErr == nil {
						t.Fatal("lost response hidden")
					}
				} else {
					publicationMust(t, applyErr)
				}
				if key == "affected" {
					firstRevision = receipt.Revision
				}
			}
			if kind == "manual-close" {
				children[400]["state"] = "closed"
				children[400]["state_reason"] = "completed"
			}
			if kind == "retire-gap" {
				state, revision, loadErr := f.db.GetPlanWorkflowState(t.Context(), authority.Parent)
				publicationMust(t, loadErr)
				publicationMust(t, state.OpenPlanWait("affected", "retire-wait", "retire", 4000))
				_, loadErr = f.db.SavePlanWorkflowState(t.Context(), authority.Parent, revision, state)
				publicationMust(t, loadErr)
				authority.SelectedKey = "affected"
				authority.Candidate = "retire-candidate"
				authority.ActorID = 10
				authority.RetirementCandidate = "affected-candidate"
				authority.RetirementRevision = firstRevision
				authority.Confirmation = &planbrief.PlanOwnerReply{ID: 99, OwnerID: 10, ActorID: 10, IssueID: 4000, Key: "affected", Stage: "story", Candidate: "affected-candidate", WaitID: "retire-wait", Body: "/retire-plan affected affected-candidate"}
				closedLost = true
				_, applyErr := activity.Apply(t.Context(), authority, graphexec.PlanStoryRequest{RequestID: "retire", Operation: "retire", Stage: "story", Candidate: authority.Candidate, SharedRevision: "shared", ExpectedRevision: firstRevision, Draft: planbrief.PlanDraft{Key: "affected"}})
				if applyErr == nil {
					t.Fatal("lost retirement response hidden")
				}
			}
			_, err = f.db.AdmitPlanWorkflowPath(t.Context(), f.session.ID, "graph")
			publicationMust(t, err)
			// Reopen the database to remove any dependence on process memory.
			publicationMust(t, f.runtime.Authority.Close(t.Context()))
			publicationMust(t, f.db.Close())
			f.db, err = store.Open(t.Context(), f.path)
			publicationMust(t, err)
			f.d.db = f.db
			f.reopenRuntime(t)
			filter, err := turn.NewFilter(f.db, turn.Identities{OwnerUserID: 10, BotUserID: 20}, f.d.log)
			publicationMust(t, err)
			f.d.filter = filter
			q := &repoQueue{repo: store.Repo{ID: f.session.RepoID}, forgeRepo: authority.Repo, log: f.d.log}
			publicationMust(t, f.d.graphPlanParents(t.Context(), map[int64]*repoQueue{f.session.RepoID: q}, map[int64]*candidate{}))
			if len(q.candidates) != 1 {
				t.Fatalf("no-comment recovery admission=%d", len(q.candidates))
			}
			ref, err := naming.ParseRef(f.session.TaskRef)
			publicationMust(t, err)
			task := &task{ref: ref, session: f.session, issue: github.Issue{ID: 2780, Number: 278, State: "open"}, forgeRepo: authority.Repo, log: f.d.log}
			failParent = kind == "shared-failure"
			before := mutations
			err = f.d.recoverPlanStories(t.Context(), task)
			if failParent {
				// Applied completed requests need no API call, so force discovery to observe outage.
				_, err = f.d.refinePlanChildren(t.Context(), task)
				if err == nil {
					t.Fatal("shared API failure concealed")
				}
				return
			}
			publicationMust(t, err)
			_, err = f.d.refinePlanChildren(t.Context(), task)
			publicationMust(t, err)
			state, _, err := f.db.GetPlanWorkflowState(t.Context(), authority.Parent)
			publicationMust(t, err)
			if !slices.Contains(state.EligiblePlanStories(), "healthy") {
				t.Fatalf("healthy story blocked: %+v", state)
			}
			if slices.Contains(state.EligiblePlanStories(), "affected") {
				t.Fatalf("affected story incorrectly eligible: %+v", state)
			}
			publicationMust(t, f.d.recoverPlanStories(t.Context(), task))
			if mutations != before {
				t.Fatalf("recovery repeated remote mutation %d -> %d", before, mutations)
			}
			// After real daemon recovery/discovery, advance the eligible healthy story
			// through the production activity, with no new comment for the held story.
			activity.DB = f.db
			healthy, err := f.db.GetPlanStoryDraft(t.Context(), authority.Parent, "healthy")
			publicationMust(t, err)
			var draft planbrief.PlanDraft
			publicationMust(t, json.Unmarshal([]byte(healthy.DraftJSON), &draft))
			draft.Problem = "Healthy shaping advances while another story is inactive or held"
			next := authority
			next.SelectedKey = "healthy"
			next.Candidate = "healthy-next"
			next.Confirmation = nil
			receipt, err := activity.Apply(t.Context(), next, graphexec.PlanStoryRequest{RequestID: "healthy-update", Operation: "update", Stage: "story", Candidate: next.Candidate, SharedRevision: "shared", ExpectedRevision: healthy.Revision, ChangedSections: []string{"intent"}, Draft: draft})
			publicationMust(t, err)
			if receipt.Outcome != "applied" || receipt.Revision == healthy.Revision || mutations != before+1 {
				t.Fatalf("healthy work did not advance: %+v mutations=%d", receipt, mutations)
			}
		})
	}
}

func TestPlanRecoveryCandidateLessShell(t *testing.T) {
	for _, failure := range []string{"none", "lookup", "readback", "wrong-parent", "ambiguous"} {
		t.Run(failure, func(t *testing.T) {
			f := newPublicationIO(t)
			ctx := t.Context()
			ref, err := naming.ParseRef(f.session.TaskRef)
			publicationMust(t, err)
			state := planbrief.PlanState{SharedRevision: "shared"}
			rev, _, err := state.ProposeStorySet([]planbrief.PlanStorySetEntry{{ID: "one", Name: "First", Intent: "First story"}, {ID: "two", Name: "Second", Intent: "Second story"}})
			publicationMust(t, err)
			_, err = state.PromoteStorySet(rev)
			publicationMust(t, err)
			healthy := approvedPlanState(t, "implementation-engineer", true).Stories[0]
			healthy.Key, healthy.Draft.Key = "story-001", "story-001"
			state.Stories[0] = healthy
			for _, stage := range []string{"story", "technical", "acceptance"} {
				current, e := state.ReviewRevision(healthy.Key, stage)
				publicationMust(t, e)
				healthy.Approvals[stage] = current
			}
			_, err = f.db.SavePlanWorkflowState(ctx, ref.String(), 0, state)
			publicationMust(t, err)
			for _, key := range []string{"story-001", "story-002"} {
				_, err = f.db.ReservePlanStory(ctx, ref.String(), key)
				publicationMust(t, err)
			}
			publicationMust(t, f.db.BindPlanStoryShell(ctx, ref.String(), "story-001", 4000, 400, "healthy-shell"))
			_, err = f.db.AdmitPlanWorkflowPath(ctx, f.session.ID, "graph")
			publicationMust(t, err)
			// Existing sibling is already a draft: shell recovery must not touch it.
			sibling, err := f.db.GetPlanStoryDraft(ctx, ref.String(), "story-001")
			publicationMust(t, err)
			sibling.Candidate = healthy.Candidate
			publicationMust(t, f.db.PutPlanStoryCandidate(ctx, store.PlanStoryCandidate{Parent: ref.String(), RequestID: "healthy", Key: sibling.Key, Candidate: healthy.Candidate, RequestJSON: `{}`}))
			publicationMust(t, f.db.ApplyPlanStoryDraft(ctx, sibling, sibling.Revision, store.PlanStoryEffect{Parent: ref.String(), RequestID: "healthy", Phase: "draft", Outcome: "applied"}))
			saved, version, err := f.db.GetPlanWorkflowState(ctx, ref.String())
			publicationMust(t, err)
			publicationMust(t, f.db.SettlePlanStoryDebt(ctx, ref.String(), "healthy", saved, version, true))
			body := "<!-- lf:plan-story parent=2780 key=story-002 -->\nShell"
			child := map[string]any{"id": 4010, "number": 401, "title": "Second", "body": body, "state": "open"}
			failing := failure != "none"
			mutations := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.Method != http.MethodGet {
					mutations++
					http.Error(w, "unexpected mutation", http.StatusInternalServerError)
					return
				}
				switch r.URL.Path {
				case "/repos/owner/api/issues":
					if failing && failure == "lookup" {
						http.Error(w, "lookup unavailable", http.StatusServiceUnavailable)
						return
					}
					list := []any{child}
					if failing && failure == "ambiguous" {
						list = append(list, child)
					}
					_ = json.NewEncoder(w).Encode(list)
				case "/repos/owner/api/issues/401":
					if failing && failure == "readback" {
						http.Error(w, "readback unavailable", http.StatusServiceUnavailable)
						return
					}
					_ = json.NewEncoder(w).Encode(child)
				case "/repos/owner/api/issues/401/parent":
					number := 278
					if failing && failure == "wrong-parent" {
						number = 999
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"id": number * 10, "number": number, "state": "open"})
				default:
					http.NotFound(w, r)
				}
			}))
			defer srv.Close()
			client, err := github.New(github.Options{APIBase: srv.URL + "/", HTTPClient: srv.Client(), Token: secrets.NewToken("fixture")})
			publicationMust(t, err)
			f.d.forge = client
			publicationMust(t, os.MkdirAll(f.runtime.Paths.StoryProgressLocksDir(), 0700))
			work := &task{ref: ref, session: f.session, issue: github.Issue{ID: 2780, Number: 278, State: "open"}, forgeRepo: github.RepoFromRef(ref), log: f.d.log}
			err = f.d.recoverPlanStories(ctx, work)
			if failing {
				if err == nil {
					t.Fatal("unsafe recovery accepted")
				}
				draft, readErr := f.db.GetPlanStoryDraft(ctx, ref.String(), "story-002")
				publicationMust(t, readErr)
				if draft.IssueID != 0 || mutations != 0 {
					t.Fatal("failed lookup adopted or created shell")
				}
			} else {
				publicationMust(t, err)
			}
			publicationMust(t, f.runtime.Authority.Close(ctx))
			publicationMust(t, f.db.Close())
			f.db, err = store.Open(ctx, f.path)
			publicationMust(t, err)
			f.d.db = f.db
			f.reopenRuntime(t)
			failing = false
			for range 2 {
				publicationMust(t, f.d.recoverPlanStories(ctx, work))
			}
			draft, err := f.db.GetPlanStoryDraft(ctx, ref.String(), "story-002")
			publicationMust(t, err)
			if draft.IssueID != 4010 || draft.IssueNumber != 401 || draft.Revision != turn.PlanIssueRevision("Second", body) || mutations != 0 {
				t.Fatalf("binding=%+v mutations=%d", draft, mutations)
			}
			retained, _, err := f.db.GetPlanWorkflowState(ctx, ref.String())
			publicationMust(t, err)
			if !reflect.DeepEqual(retained.Stories[0], healthy) {
				t.Fatal("sibling authority changed")
			}
			if _, ok := storyWorkID(retained, "story-002"); !ok {
				t.Fatal("recovered child has no work")
			}
			if _, due := parentWorkIDWithScope(retained, planbrief.ReleaseScope([]string{"implementation-engineer"}, []string{"agent-setup"})); due {
				t.Fatal("assembly bypassed child review")
			}
			next := approvedPlanState(t, "implementation-engineer", false).Stories[0].Draft
			next.Key = "story-002"
			publicationMust(t, retained.PutCandidate(next, "story", "recovered-draft"))
			scope := planbrief.ReleaseScope([]string{"implementation-engineer"}, []string{"agent-setup"})
			for i, stage := range []string{"story", "technical", "acceptance"} {
				if _, due := parentWorkIDWithScope(retained, scope); due {
					t.Fatalf("assembly bypassed %s approval", stage)
				}
				publicationMust(t, retained.OpenPlanWait(next.Key, "review-"+stage, "review", 4010))
				applied, e := retained.ApplyOwnerReply(planbrief.PlanOwnerReply{ID: int64(100 + i), OwnerID: 7, ActorID: 7, IssueID: 4010, Key: next.Key, Stage: stage, Candidate: "recovered-draft", WaitID: "review-" + stage, Body: "/approve-plan " + next.Key + " " + stage + " recovered-draft"})
				publicationMust(t, e)
				if !applied {
					t.Fatal("current approval not applied")
				}
			}
			if _, due := parentWorkIDWithScope(retained, scope); !due {
				t.Fatal("reviewed recovered set cannot assemble")
			}

		})
	}
}

// A busy parent story-progress lock seen by daemon recovery or Plan admission is
// durable work for a later tick, not a failed turn (#997).
func TestPlanRecoveryBusyParentDefers(t *testing.T) {
	f := newPublicationIO(t)
	ctx := t.Context()
	ref, err := naming.ParseRef(f.session.TaskRef)
	publicationMust(t, err)
	state := planbrief.PlanState{SharedRevision: "shared"}
	rev, _, err := state.ProposeStorySet([]planbrief.PlanStorySetEntry{{ID: "one", Name: "First", Intent: "First story"}, {ID: "two", Name: "Second", Intent: "Second story"}})
	publicationMust(t, err)
	_, err = state.PromoteStorySet(rev)
	publicationMust(t, err)
	_, err = f.db.SavePlanWorkflowState(ctx, ref.String(), 0, state)
	publicationMust(t, err)
	for _, key := range []string{"story-001", "story-002"} {
		_, err = f.db.ReservePlanStory(ctx, ref.String(), key)
		publicationMust(t, err)
	}
	_, err = f.db.AdmitPlanWorkflowPath(ctx, f.session.ID, "graph")
	publicationMust(t, err)
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		http.Error(w, "unexpected request", http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	client, err := github.New(github.Options{APIBase: srv.URL + "/", HTTPClient: srv.Client(), Token: secrets.NewToken("fixture")})
	publicationMust(t, err)
	f.d.forge = client
	lockDir := f.runtime.Paths.StoryProgressLocksDir()
	publicationMust(t, os.MkdirAll(lockDir, 0700))
	release, err := f.db.LockStoryProgress(ctx, ref.String(), lockDir)
	publicationMust(t, err)
	defer func() { publicationMust(t, release()) }()
	newTask := func() *task {
		return &task{ref: ref, session: f.session, issue: github.Issue{ID: 2780, Number: 278, State: "open"}, forgeRepo: github.RepoFromRef(ref), log: f.d.log, worker: &workerState{}}
	}

	work := newTask()
	err = f.d.recoverPlanStories(ctx, work)
	if !errors.Is(err, store.ErrStoryProgressBusy) || requests != 0 {
		t.Fatalf("shell recovery under a busy parent: err=%v requests=%d", err, requests)
	}
	f.d.stopped(ctx, work, "could not reconcile Plan stories", err)
	if work.worker.status != workerDeferred || work.prepareFailureRecorded || f.d.prepFailures.items[f.session.ID].Attempts != 0 {
		t.Fatalf("busy parent counted as failure: status=%v recorded=%v failures=%+v", work.worker.status, work.prepareFailureRecorded, f.d.prepFailures.items[f.session.ID])
	}

	admission := fmt.Errorf("admit plan execution: %w", err)
	owner := newTask()
	owner.commentUnderWay = 5001
	if got := f.d.leave(ctx, owner, f.d.log, "could not prepare Plan", admission); got != nil {
		t.Fatalf("owner comment leave=%v", got)
	}
	if owner.worker.status != workerDeferred || owner.prepareFailureRecorded || f.d.prepFailures.items[f.session.ID].Attempts != 0 {
		t.Fatalf("busy Plan admission counted as failure: status=%v failures=%+v", owner.worker.status, f.d.prepFailures.items[f.session.ID])
	}
	graph := newTask()
	graph.commentUnderWay = store.GraphDispatch{ID: 9}.TriggerID()
	// A graph dispatch still sees the cause, so it is requeued for a later tick.
	if got := f.d.leave(ctx, graph, f.d.log, "could not prepare Plan", admission); !errors.Is(got, store.ErrStoryProgressBusy) {
		t.Fatalf("graph dispatch leave=%v", got)
	}
	if graph.worker.status != workerDeferred || f.d.prepFailures.items[f.session.ID].Attempts != 0 {
		t.Fatalf("busy graph dispatch counted as failure: status=%v failures=%+v", graph.worker.status, f.d.prepFailures.items[f.session.ID])
	}

	// Any other error keeps today's failure accounting.
	other := newTask()
	f.d.stopped(ctx, other, "could not reconcile Plan stories", errors.New("transport unavailable"))
	if other.worker.status != workerFailed || f.d.prepFailures.items[f.session.ID].Attempts != 1 {
		t.Fatalf("ordinary failure not counted: status=%v failures=%+v", other.worker.status, f.d.prepFailures.items[f.session.ID])
	}
}

// C6 (#1138): on a non-listing tick of a webhook-healthy repository a graph
// Plan parent is still offered when it has due Plan work: an assembled review
// due and not attempted, recovery debt, an owed story shell, or a story that
// owes a comment sweep because a webhook hint named it. A parent with none of
// these waits for its new input or the listing.
func TestPlanParentsWebhookHealthyOfferedWhenDue(t *testing.T) {
	w := newOwnerWaitFleet(t, 0, true, true)
	ctx := t.Context()
	db := w.d.db
	assembly := w.planParent(t, 60)
	debt := w.planParent(t, 61)
	shell := w.planParent(t, 62)
	swept := w.planParent(t, 63)
	const child = 640
	publicationMust(t, db.PutManagedFeature(ctx, store.ManagedFeature{ParentTask: swept.TaskRef, Generation: 1, State: "planning"}))
	publicationMust(t, db.PutManagedStory(ctx, store.ManagedStory{ParentTask: swept.TaskRef, Generation: 1, Key: "story-001", IssueNumber: child, IssueID: child * 10, BodyHash: "fleet", State: "planning"}))
	publicationMust(t, db.AdvanceStoryDiscovery(ctx, swept.ID, child, 1))
	for range 2 {
		w.heartbeat(t)
	}
	all := []store.Session{w.parent, assembly, debt, shell, swept}
	for _, s := range all {
		if w.needsPoll(t, s) {
			t.Fatalf("parent %s not observed", s.TaskRef)
		}
	}
	if _, parents := w.sources(t, false); len(parents) != 0 {
		t.Fatalf("parents without due work offered between listings: %v", parents)
	}

	// Assembly is due and not attempted.
	_, err := db.SavePlanWorkflowState(ctx, assembly.TaskRef, 0, approvedPlanState(t, "go-implementation-engineer", true))
	publicationMust(t, err)
	// Recovery debt: a recorded request without its completion.
	publicationMust(t, db.PutPlanStoryCandidate(ctx, store.PlanStoryCandidate{Parent: debt.TaskRef, RequestID: "lost", Key: "story-001", Candidate: "c", RequestJSON: `{}`}))
	// A promoted story whose draft has no candidate is owed its issue shell.
	state := planbrief.PlanState{SharedRevision: "shared"}
	rev, _, err := state.ProposeStorySet([]planbrief.PlanStorySetEntry{{ID: "one", Name: "First", Intent: "First story"}})
	publicationMust(t, err)
	_, err = state.PromoteStorySet(rev)
	publicationMust(t, err)
	_, err = db.SavePlanWorkflowState(ctx, shell.TaskRef, 0, state)
	publicationMust(t, err)
	// A webhook hint names the swept parent's story on this non-listing tick.
	w.d.webhook.Wake(w.repo.ID, child)
	q := &repoQueue{repo: w.repo, forgeRepo: github.Repo{Owner: w.repo.Owner, Name: w.repo.Name}, log: w.d.log}
	publicationMust(t, w.d.webhookHints(ctx, map[int64]*repoQueue{w.repo.ID: q}, map[int64]*candidate{}, &TickResult{}))
	if owed, err := db.ListStoriesAwaitingDiscovery(ctx, swept.ID, swept.TaskRef, 1); err != nil || !slices.Equal(owed, []int{child}) {
		t.Fatalf("hint did not owe the story a sweep: %v %v\n%s", owed, err, w.f.logs.String())
	}

	_, parents := w.sources(t, false)
	for _, s := range []store.Session{w.parent, assembly, debt, shell, swept} {
		// Each answer is read, not assumed: a read failure would count as due.
		due, err := w.d.planParentDueWork(ctx, q, s, mustRef(t, s.TaskRef))
		if err != nil || due != (s.ID != w.parent.ID) {
			t.Errorf("parent %s: due=%t err=%v", s.TaskRef, due, err)
		}
	}
	for _, s := range []store.Session{assembly, debt, shell, swept} {
		if !parents[s.ID] {
			t.Errorf("parent %s with due Plan work not offered between listings", s.TaskRef)
		}
	}
	if parents[w.parent.ID] {
		t.Errorf("parent without due work offered between listings")
	}
	if _, parents := w.sources(t, true); !parents[w.parent.ID] {
		t.Errorf("parent without due work not offered on the listing tick: %v", parents)
	}
}
