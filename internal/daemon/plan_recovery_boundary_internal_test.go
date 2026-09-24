package daemon

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
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
