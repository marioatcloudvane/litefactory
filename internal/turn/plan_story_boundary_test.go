package turn

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
)

type planBoundaryForge struct {
	mu                                             sync.Mutex
	child                                          map[string]any
	attached                                       bool
	creates, patches                               int
	failCreate, failAttach, failUpdate, failRetire bool
	raceUpdate, hidden, reparent, deleted          bool
}

func (f *planBoundaryForge) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	parent := map[string]any{"id": int64(3620), "number": 362, "state": "open"}
	var out any
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/app/issues/362":
		out = parent
	case r.Method == http.MethodPost && r.URL.Path == "/repos/acme/app/issues":
		f.creates++
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		body["id"] = int64(4000)
		body["number"] = 400
		body["state"] = "open"
		f.child = body
		if f.failCreate {
			f.failCreate = false
			http.Error(w, `{"message":"lost response"}`, http.StatusInternalServerError)
			return
		}
		out = f.child
	case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/app/issues":
		out = []any{}
		if f.child != nil && !f.hidden {
			out = []any{f.child}
		}
	case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/app/issues/400/parent":
		if !f.attached {
			http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
			return
		}
		out = parent
		if f.reparent {
			out = map[string]any{"id": 999, "number": 999, "state": "open"}
		}
	case r.Method == http.MethodPost && r.URL.Path == "/repos/acme/app/issues/362/sub_issues":
		if f.failAttach {
			f.attached = true
			f.failAttach = false
			http.Error(w, `{"message":"attach failed"}`, http.StatusInternalServerError)
			return
		}
		f.attached = true
		out = f.child
	case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/app/issues/400":
		if f.deleted {
			http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
			return
		}
		out = f.child
	case r.Method == http.MethodPatch && r.URL.Path == "/repos/acme/app/issues/400":
		f.patches++
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		for k, v := range body {
			f.child[k] = v
		}
		if f.raceUpdate {
			prior, _ := f.child["body"].(string)
			f.child["body"] = prior + "\nConcurrent owner edit"
		}
		out = f.child
		if f.failUpdate || f.failRetire {
			f.failUpdate = false
			f.failRetire = false
			http.Error(w, `{"message":"lost mutation response"}`, http.StatusInternalServerError)
			return
		}
	default:
		http.Error(w, `{"message":"unexpected path"}`, http.StatusBadRequest)
		return
	}
	_ = json.NewEncoder(w).Encode(out)
}
func planBoundarySetup(t *testing.T) (*PlanStoryActivity, *planBoundaryForge, PlanStoryAuthority, graphexec.PlanStoryRequest, func()) {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "plan.db")
	open := func() *store.DB {
		db, err := store.Open(ctx, path)
		if err != nil {
			t.Fatal(err)
		}
		if err = db.Migrate(ctx); err != nil {
			t.Fatal(err)
		}
		return db
	}
	f := &planBoundaryForge{}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	client, err := github.New(github.Options{APIBase: srv.URL + "/", Token: secrets.NewToken("fixture"), HTTPClient: srv.Client()})
	if err != nil {
		t.Fatal(err)
	}
	db := open()
	repo, err := db.UpsertRepo(ctx, store.Repo{Path: t.TempDir(), Forge: store.ForgeGitHub, Owner: "acme", Name: "app", RemoteURL: "https://github.com/acme/app.git", BaseBranch: "main", FeaturePrefix: "lf/", WorktreeRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.GetOrCreateSession(ctx, repo, 362); err != nil {
		t.Fatal(err)
	}
	h := &PlanStoryActivity{DB: db, Forge: client, Redactor: redact.New(redact.Options{}), LockDir: t.TempDir()}
	t.Cleanup(func() { _ = h.DB.Close() })
	a := PlanStoryAuthority{Parent: "github:acme/app#362", Repo: github.Repo{Owner: "acme", Name: "app"}, ParentNumber: 362, ParentID: 3620, Stage: "story", SelectedKey: "story-one", Candidate: "candidate-one"}
	r := graphexec.PlanStoryRequest{RequestID: "request-one", Operation: "create", Stage: "story", Candidate: a.Candidate, SharedRevision: "shared-one", Draft: planbrief.PlanDraft{Key: a.SelectedKey, Name: "A usable story", UserStory: planbrief.UserStoryContent{Actor: "owner", Want: "shape a story", Benefit: "retain intent"}, Problem: "Plans need discussion", InScope: []string{"Drafts"}, NotInScope: []string{"Implementation"}}}
	restart := func() {
		if err := h.DB.Close(); err != nil {
			t.Fatal(err)
		}
		h.DB = open()
	}
	return h, f, a, r, restart
}
func planApply(t *testing.T, h *PlanStoryActivity, a PlanStoryAuthority, r graphexec.PlanStoryRequest, want string) PlanStoryReceipt {
	t.Helper()
	got, err := h.Apply(t.Context(), a, r)
	if err != nil || got.Outcome != want {
		t.Fatalf("Apply outcome=%+v err=%v want=%s", got, err, want)
	}
	return got
}
func TestPlanBoundaryCreateRecovery(t *testing.T) {
	for _, phase := range []string{"create", "attach", "unobservable"} {
		t.Run(phase, func(t *testing.T) {
			h, f, a, r, restart := planBoundarySetup(t)
			f.failCreate = phase != "attach"
			f.failAttach = phase == "attach"
			planApplySharedFailure(t, h, a, r)
			restart()
			if phase == "unobservable" {
				f.hidden = true
				planApply(t, h, a, r, "uncertain")
				if f.creates != 1 {
					t.Fatal("blind recreation")
				}
				f.hidden = false
			}
			got := planApply(t, h, a, r, "applied")
			if got.IssueID != 4000 || got.IssueNumber != 400 || f.creates != 1 || !f.attached {
				t.Fatalf("lost identity %+v creates=%d", got, f.creates)
			}
			restart()
			planApply(t, h, a, r, "applied")
			if f.creates != 1 {
				t.Fatal("replay created duplicate")
			}
			if f.child["title"] != "362-001: A usable story" {
				t.Fatalf("title %v", f.child["title"])
			}
			body, ok := f.child["body"].(string)
			if !ok {
				t.Fatal("remote body was not a string")
			}
			for _, heading := range planScaffoldHeadings(t) {
				if !strings.Contains(body, heading) {
					t.Fatal("missing " + heading)
				}
			}
		})
	}
}
func TestPlanBoundaryUpdateAndRetirementRecovery(t *testing.T) {
	h, f, a, r, restart := planBoundarySetup(t)
	created := planApply(t, h, a, r, "applied")
	r.RequestID = "update-one"
	r.Operation = "update"
	r.ExpectedRevision = created.Revision
	r.Candidate = "candidate-two"
	a.Candidate = r.Candidate
	r.ChangedSections = []string{"intent"}
	r.Draft.Problem = "An improved explanation"
	f.failUpdate = true
	planApplySharedFailure(t, h, a, r)
	restart()
	updated := planApply(t, h, a, r, "applied")
	if updated.IssueID != created.IssueID || f.patches != 1 {
		t.Fatal("update replay repeated mutation")
	}
	r.RequestID = "retire-one"
	r.Operation = "retire"
	r.ExpectedRevision = updated.Revision
	r.Candidate = "candidate-three"
	a.Candidate = r.Candidate
	r.ChangedSections = nil
	if _, err := h.Apply(t.Context(), a, r); err == nil {
		t.Fatal("retirement without owner accepted")
	}
	if f.patches != 1 {
		t.Fatal("unauthorized retirement mutated remote")
	}
	a.OwnerID = 10
	a.ActorID = 10
	a.RetirementCandidate = "candidate-two"
	a.RetirementRevision = updated.Revision
	planAuthorizeRetirement(t, h, &a, r)
	f.failRetire = true
	planApplySharedFailure(t, h, a, r)
	restart()
	planApply(t, h, a, r, "applied")
	if f.child["state_reason"] != "not_planned" || f.patches != 2 {
		t.Fatal("retirement lost semantics or repeated effect")
	}
	d, err := h.DB.GetPlanStoryDraft(t.Context(), a.Parent, a.SelectedKey)
	if err != nil || !d.Retired || d.Ordinal != 1 {
		t.Fatalf("retirement not retained %+v %v", d, err)
	}
	next, err := h.DB.ReservePlanStory(t.Context(), a.Parent, "next")
	if err != nil || next.Ordinal != 2 {
		t.Fatalf("retired ordinal reused %+v %v", next, err)
	}
}
func TestPlanBoundaryRejectsDriftAndUnauthorized(t *testing.T) {
	for _, kind := range []string{"foreign", "malformed", "owner-edit", "closed", "reparented", "deleted", "stale", "changed-request"} {
		t.Run(kind, func(t *testing.T) {
			h, f, a, r, _ := planBoundarySetup(t)
			if kind == "foreign" || kind == "malformed" {
				if kind == "foreign" {
					a.SelectedKey = "foreign"
				} else {
					r.Draft.UserStory.Actor = ""
				}
				if _, err := h.Apply(t.Context(), a, r); err == nil {
					t.Fatal("invalid input accepted")
				}
				if f.creates != 0 {
					t.Fatal("invalid input reached remote")
				}
				return
			}
			created := planApply(t, h, a, r, "applied")
			if kind == "changed-request" {
				r.Draft.Name = "Changed"
				if _, err := h.Apply(t.Context(), a, r); err == nil {
					t.Fatal("request identity reused")
				}
				return
			}
			r.RequestID = "update"
			r.Operation = "update"
			r.ExpectedRevision = created.Revision
			r.Candidate = "updated"
			a.Candidate = r.Candidate
			r.ChangedSections = []string{"intent"}
			want := "conflict"
			switch kind {
			case "owner-edit":
				prior, _ := f.child["body"].(string)
				f.child["body"] = prior + "\nOwner edit"
			case "closed":
				f.child["state"] = "closed"
			case "reparented":
				f.reparent = true
			case "deleted":
				f.deleted = true
				want = "uncertain"
			case "stale":
				r.ExpectedRevision = strings.Repeat("0", 64)
				if _, err := h.Apply(t.Context(), a, r); err == nil {
					t.Fatal("stale revision accepted")
				}
				return
			}
			planApply(t, h, a, r, want)
			if f.patches != 0 {
				t.Fatal("drift overwritten")
			}
		})
	}
}

func TestPlanBoundaryScopedArchitecturePersistsAcrossCycles(t *testing.T) {
	for _, scope := range []string{"agent", "go", "frontend"} {
		t.Run(scope, func(t *testing.T) {
			h, f, a, r, restart := planBoundarySetup(t)
			original := r
			first := planApply(t, h, a, r, "applied")
			r.RequestID = "technical-one"
			r.Candidate = "tech-one"
			r.Operation = "update"
			r.Stage = "technical"
			r.ExpectedRevision = first.Revision
			r.ChangedSections = []string{"architecture", "guidance"}
			a.Candidate = r.Candidate
			a.Stage = r.Stage
			r.Draft.Architecture = &planbrief.PlanArchitecture{General: []planbrief.GeneralDecision{{Area: "Ownership", Decision: "Reuse existing boundary", Rationale: "Keep one owner"}}}
			r.Draft.Guidance = []string{"agent-setup"}
			if scope == "go" {
				r.Draft.Guidance = []string{"go"}
				r.Draft.Architecture.ServiceDesign = []planbrief.ServiceDefinition{{Component: "Repository", Responsibility: "Persist drafts", Access: "Store only"}}
			}
			if scope == "frontend" {
				r.Draft.Frontend = true
				r.Draft.Design = []string{"https://example.test/design"}
				r.ChangedSections = append(r.ChangedSections, "intent", "design")
			}
			second := planApply(t, h, a, r, "applied")
			restart()
			r.RequestID = "technical-two"
			r.Candidate = "tech-two"
			r.ExpectedRevision = second.Revision
			a.Candidate = r.Candidate
			r.Draft = planbrief.PlanDraft{Key: a.SelectedKey, Decisions: []string{"Reuse settled architecture"}}
			r.ChangedSections = []string{"conversation"}
			third := planApply(t, h, a, r, "applied")
			if third.IssueID != first.IssueID {
				t.Fatal("progression changed identity")
			}
			d, err := h.DB.GetPlanStoryDraft(t.Context(), a.Parent, a.SelectedKey)
			if err != nil {
				t.Fatal(err)
			}
			var got planbrief.PlanDraft
			if err = json.Unmarshal([]byte(d.DraftJSON), &got); err != nil {
				t.Fatal(err)
			}
			if got.Architecture == nil || len(got.Architecture.General) != 1 || len(got.Guidance) != 1 {
				t.Fatalf("settled architecture lost %+v", got)
			}
			body, ok := f.child["body"].(string)
			if !ok {
				t.Fatal("remote body was not a string")
			}
			if !strings.Contains(body, "## Architecture - General") {
				t.Fatal("General missing")
			}
			if scope == "agent" && strings.Contains(body, "## Service Design") {
				t.Fatal("irrelevant layers invented")
			}
			if scope == "frontend" && (len(got.Design) != 1 || !strings.Contains(body, "# Design")) {
				t.Fatal("frontend references lost")
			}
			a.Candidate = original.Candidate
			a.Stage = original.Stage
			replay := planApply(t, h, a, original, "applied")
			if replay.Revision != first.Revision {
				t.Fatalf("old request replay changed revision %s != %s", replay.Revision, first.Revision)
			}
		})
	}
}
func TestPlanBoundaryConcurrentUpdateIsUncertain(t *testing.T) {
	h, f, a, r, _ := planBoundarySetup(t)
	created := planApply(t, h, a, r, "applied")
	r.Operation = "update"
	r.RequestID = "race"
	r.Candidate = "race-candidate"
	a.Candidate = r.Candidate
	r.ExpectedRevision = created.Revision
	r.ChangedSections = []string{"intent"}
	r.Draft.Problem = "Changed"
	f.raceUpdate = true
	receipt := planApply(t, h, a, r, "uncertain")
	if !strings.Contains(receipt.Detail, "atomic CAS") {
		t.Fatalf("dishonest race diagnosis %+v", receipt)
	}
	d, err := h.DB.GetPlanStoryDraft(t.Context(), a.Parent, a.SelectedKey)
	if err != nil || d.Revision != created.Revision {
		t.Fatalf("unconfirmed revision persisted %+v %v", d, err)
	}
}

// planScaffoldHeadings preserves the owner-specified visible spelling independently
// of the production renderer constants.
func planScaffoldHeadings(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile("testdata/plan_scaffold_headings.golden")
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(raw)), "\n")
}

func planApplySharedFailure(t *testing.T, h *PlanStoryActivity, a PlanStoryAuthority, r graphexec.PlanStoryRequest) {
	t.Helper()
	got, err := h.Apply(t.Context(), a, r)
	if err == nil {
		t.Fatalf("shared HTTP failure concealed: %+v", got)
	}
	effect, loadErr := h.DB.LatestPlanStoryEffect(t.Context(), a.Parent, r.RequestID)
	if loadErr != nil || effect.Outcome != "uncertain" {
		t.Fatalf("uncertainty lost: %+v %v", effect, loadErr)
	}
}
func planAuthorizeRetirement(t *testing.T, h *PlanStoryActivity, a *PlanStoryAuthority, r graphexec.PlanStoryRequest) {
	t.Helper()
	state, revision, err := h.DB.GetPlanWorkflowState(t.Context(), a.Parent)
	if err != nil {
		t.Fatal(err)
	}
	review, err := state.ReviewRevision(r.Draft.Key, r.Stage)
	if err != nil {
		t.Fatal(err)
	}
	for i := range state.Stories {
		if state.Stories[i].Key == r.Draft.Key {
			state.Stories[i].Wait = &planbrief.PlanWait{ID: "retire-wait", Kind: "retire", IssueID: 4000, Stage: r.Stage, Candidate: a.RetirementCandidate, Revision: review}
		}
	}
	if _, err = h.DB.SavePlanWorkflowState(t.Context(), a.Parent, revision, state); err != nil {
		t.Fatal(err)
	}
	a.Confirmation = &planbrief.PlanOwnerReply{ID: 99, OwnerID: a.OwnerID, ActorID: a.ActorID, IssueID: 4000, Key: r.Draft.Key, Stage: r.Stage, Candidate: a.RetirementCandidate, WaitID: "retire-wait", Body: "/retire-plan " + r.Draft.Key + " " + a.RetirementCandidate}
}

// A draft within the raw budget can still render too large once the hidden
// metadata repeats it. That request can never publish, so it fails without a
// remote mutation and recovery settles it instead of retrying it every turn.
func TestPlanBoundaryOversizedRenderFailsAndSettles(t *testing.T) {
	h, f, a, r, restart := planBoundarySetup(t)
	r.Draft.Problem = strings.Repeat("x", 40000)
	got := planApply(t, h, a, r, "failed")
	if f.creates != 0 || f.patches != 0 || !strings.Contains(got.Detail, "exceeds byte budget") {
		t.Fatalf("oversized draft reached the forge or lost its cause: %+v creates=%d patches=%d", got, f.creates, f.patches)
	}
	restart()
	if err := h.Reconcile(t.Context(), a, 8); err != nil {
		t.Fatalf("recovery failed instead of settling: %v", err)
	}
	debt, err := h.DB.PlanStoryRecoveryDebt(t.Context(), a.Parent, 8)
	if err != nil || len(debt) != 0 {
		t.Fatalf("unpublishable request still owed: %+v err=%v", debt, err)
	}
	if err = h.Reconcile(t.Context(), a, 8); err != nil || f.creates != 0 {
		t.Fatalf("settled request replayed: err=%v creates=%d", err, f.creates)
	}
}
