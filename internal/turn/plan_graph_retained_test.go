package turn

import (
	"context"
	"errors"
	"strings"
	"testing"

	ar "github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/store"
)

// Gap tests for story-001 T-006 and T-007 at the service and runtime boundary.
// They take an accepted shared result through the real service and store, then
// judge terminal output against it exactly as AgentActivity.acceptRetained does.

const (
	retainedResearchResult = `{"outcome":"completed","comment":"Investigation finished.","payload":{"research":{"request":"Inspect boundaries"}}}`
	retainedPlanResult     = `{"outcome":"completed","comment":"Proposed.","payload":{"plan":{"storyrequest":{"operation":"create","draft":` + graphBindingDraft + `}}}}`
)

type retainedFixture struct {
	db      *store.DB
	session store.Session
	opts    AgentResultServiceOptions
	service *AgentResultService
	binding *ar.PlanGraphBinding // nil for Research
	policy  WorkflowPolicy
	result  string
}

func newRetainedFixture(t *testing.T, mode string) *retainedFixture {
	t.Helper()
	db, session, o := agentResultFixture(t, mode)
	f := &retainedFixture{db: db, session: session, opts: o}
	if mode == "plan" {
		f.binding = &ar.PlanGraphBinding{Story: "story-001", Stage: "story", Candidate: "plan-1-1", RequestID: "plan-request-1-1", Upstream: []string{"a", "b"}, SharedRevision: "initial"}
		o.Resolve = func(context.Context) (ar.Context, error) {
			return ar.Context{SchemaVersion: 1, WorkflowVersion: 2, Mode: "plan", QuestionsAllowed: true, PlanScope: planbrief.ContentScope{Roles: []string{"engineer"}}, PlanGraph: f.binding.Clone()}, nil
		}
		o.ReadTargets, o.EditTargets = planGraphResultTargets(), planGraphResultTargets()
		f.policy, f.result = PlanWorkflowPolicy(), retainedPlanResult
	} else {
		f.policy, f.result = ResearchWorkflowPolicy(), retainedResearchResult
	}
	f.opts = o
	service, err := BindAgentResultService(t.Context(), o)
	mustProgress(t, err)
	f.service = service
	return f
}

func (f *retainedFixture) submit(t *testing.T, result string) {
	t.Helper()
	got, err := f.service.Submit(t.Context(), "req-1", []byte(`{"result":`+result+`}`))
	if err != nil || !got.Validation.Valid || got.Snapshot == nil {
		t.Fatalf("submit refused: %+v %v", got.Validation, err)
	}
}

func envelope(response, result string) string {
	return `{"version":1,"response":"` + response + `","result":` + result + `}`
}

// AC-033: matching terminal output is judged against, and yields, the retained
// accepted result. What it returns is the retained content, which for Plan carries
// the factory-bound values the terminal envelope could never supply.
func TestAcceptRetained_MatchingTerminalYieldsRetainedContent(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"research", "plan"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			f := newRetainedFixture(t, mode)
			f.submit(t, f.result)
			node, revision, err := AgentActivity{Policy: f.policy}.acceptRetained(t.Context(), f.service, envelope("done", f.result))
			if err != nil {
				t.Fatalf("matching terminal output refused: %v", err)
			}
			if revision != 1 || node.Response != "done" || node.Version != 1 {
				t.Fatalf("unexpected node: revision=%d %+v", revision, node)
			}
			retained, _, err := f.service.retainedCurrent(t.Context())
			mustProgress(t, err)
			if !ar.SameResult(retained.Result, node.Result) || node.Result.Comment != retained.Result.Comment {
				t.Fatal("the node result is not the retained one")
			}
			if mode == "plan" {
				q := node.Result.Payload.Plan.StoryRequest
				if q == nil || q.Candidate != "plan-1-1" || q.RequestID != "plan-request-1-1" || q.Stage != "story" || q.SharedRevision != "initial" || len(q.Upstream) != 2 {
					t.Fatalf("the retained factory binding was lost: %+v", q)
				}
			}
		})
	}
}

// AC-033, AC-003: terminal output can never supply the result. With no accepted
// submission the attempt is refused with its own cause and yields no node,
// whatever the terminal text claims.
func TestAcceptRetained_TerminalOutputCannotSupplyTheResult(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"research", "plan"} {
		base := retainedResearchResult
		if mode == "plan" {
			base = retainedPlanResult
		}
		other := strings.Replace(base, `"comment":"`, `"comment":"Not the retained result. `, 1)
		for name, terminal := range map[string]string{
			"terminal only":                   envelope("done", base),
			"terminal only, different result": envelope("done", other),
			"nothing at all":                  ``,
		} {
			t.Run(mode+"/"+name, func(t *testing.T) {
				t.Parallel()
				f := newRetainedFixture(t, mode)
				node, revision, err := AgentActivity{Policy: f.policy}.acceptRetained(t.Context(), f.service, terminal)
				if err == nil || !strings.Contains(err.Error(), "submit it through the shared result route") || !errors.Is(err, errNoRetainedResult) {
					t.Fatalf("terminal output stood in for a missing result: %v", err)
				}
				if revision != 0 || node.Result.Comment != "" || node.Response != "" {
					t.Fatalf("a refused attempt yielded content: revision=%d %+v", revision, node)
				}
			})
		}
	}
}

// C1 (#410): once a valid result was submitted, the step succeeds on it whatever
// the terminal text is. A copy missing its closing brace (#351 run 627), valid
// JSON that is not an envelope (#384), a differing or inconsistent envelope, an
// approval claim, prose and empty output all yield the retained result, with the
// response derived from it.
func TestAcceptRetained_SubmittedResultStandsWhateverIsPrinted(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"research", "plan"} {
		base := retainedResearchResult
		if mode == "plan" {
			base = retainedPlanResult
		}
		other := strings.Replace(base, `"comment":"`, `"comment":"Not the retained result. `, 1)
		confirming := envelope("done", base)
		for name, terminal := range map[string]string{
			"missing closing brace":              confirming[:len(confirming)-1],
			"valid json, not an envelope":        `{"outcome":"completed","unknown":true}`,
			"envelope with a different result":   envelope("done", other),
			"ask claimed for a completed result": envelope("ask", base),
			"unsupported response":               envelope("review_ready", base),
			"approval claimed":                   envelope("approved", base),
			"prose":                              "The result is submitted and its receipt read.",
			"empty":                              ``,
			"matching envelope":                  confirming,
		} {
			t.Run(mode+"/"+name, func(t *testing.T) {
				t.Parallel()
				f := newRetainedFixture(t, mode)
				f.submit(t, f.result)
				node, revision, err := AgentActivity{Policy: f.policy}.acceptRetained(t.Context(), f.service, terminal)
				if err != nil {
					t.Fatalf("a submitted result failed on its printed copy: %v", err)
				}
				retained, _, err := f.service.retainedCurrent(t.Context())
				mustProgress(t, err)
				if revision != 1 || node.Version != 1 || node.Response != "done" || !ar.SameResult(retained.Result, node.Result) || node.Result.Comment != retainedCommentOf(f.result) {
					t.Fatalf("node is not the retained result: revision=%d %+v", revision, node)
				}
			})
		}
	}
}

// AC-033: the guard is the runtime's, not the decoder's. A policy whose decoder
// accepts anything still cannot complete from terminal output alone, and the
// decoder is never consulted on terminal text: the node is the retained result.
func TestAcceptRetained_NominalDecoderIsNotTheGuard(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"research", "plan"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			f := newRetainedFixture(t, mode)
			nominal := f.policy
			decoded := 0
			nominal.Decode = func(raw []byte, _ ar.Context) (ar.ResearchNode, ar.Diagnostics) {
				decoded++
				var node ar.ResearchNode
				_ = ar.Decode(raw, &node) // shape only; the verdict below is unconditional
				return node, ar.Diagnostics{Valid: true, Complete: true}
			}
			h := AgentActivity{Policy: nominal}
			if err := nominal.Admit(); err != nil {
				t.Fatalf("a nominal decoder must satisfy admission: %v", err)
			}
			if _, _, err := h.acceptRetained(t.Context(), f.service, envelope("done", f.result)); !errors.Is(err, errNoRetainedResult) {
				t.Fatalf("terminal-only output completed under a nominal decoder: %v", err)
			}
			f.submit(t, f.result)
			other := strings.Replace(f.result, `"comment":"`, `"comment":"Changed. `, 1)
			node, _, err := h.acceptRetained(t.Context(), f.service, envelope("done", other))
			if err != nil || node.Result.Comment != retainedCommentOf(f.result) {
				t.Fatalf("differing output replaced or failed the retained content: %+v %v", node.Result, err)
			}
			if decoded != 0 {
				t.Fatalf("terminal text was decoded %d time(s); it must never be read as a result", decoded)
			}
		})
	}
}

// C2 (#410): the response is derived from the retained result. Incomplete with
// questions is ask, completed without questions is done, and any other shape
// fails with a diagnostic naming the retained result.
func TestRetainedRoute_DerivesAskAndDoneAndRefusesOtherShapes(t *testing.T) {
	t.Parallel()
	question := []ar.Question{{ID: "q1", Audience: "business", Text: "Which?"}}
	for _, tt := range []struct {
		name   string
		result ar.Result
		want   string
	}{
		{"incomplete with questions", ar.Result{Outcome: "incomplete", Questions: question}, "ask"},
		{"completed without questions", ar.Result{Outcome: "completed"}, "done"},
		{"completed with questions", ar.Result{Outcome: "completed", Questions: question}, ""},
		{"incomplete without questions", ar.Result{Outcome: "incomplete"}, ""},
		{"blocked", ar.Result{Outcome: "blocked", Questions: question}, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := retainedRoute(tt.result)
			if tt.want == "" {
				if err == nil || !strings.Contains(err.Error(), "neither ask") {
					t.Fatalf("shape %q routed to %q: %v", tt.name, got, err)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("got %q %v, want %q", got, err, tt.want)
			}
		})
	}
	for _, mode := range []string{"research", "plan"} {
		t.Run(mode+"/ask through the runtime", func(t *testing.T) {
			t.Parallel()
			f := newRetainedFixture(t, mode)
			payload := `{"research":{"request":"Inspect boundaries"}}`
			if mode == "plan" {
				payload = `{"plan":{}}`
			}
			f.submit(t, `{"outcome":"incomplete","reason":"Need input.","comment":"Asking.","questions":[{"id":"q1","audience":"business","text":"Which?"}],"payload":`+payload+`}`)
			node, _, err := AgentActivity{Policy: f.policy}.acceptRetained(t.Context(), f.service, "")
			if err != nil || node.Response != "ask" {
				t.Fatalf("incomplete result with questions did not route to ask: %+v %v", node, err)
			}
		})
	}
	// A valid retained Research result that is neither shape (blocked, no
	// questions) fails the step, naming the retained result.
	t.Run("research/other shape through the runtime", func(t *testing.T) {
		t.Parallel()
		f := newRetainedFixture(t, "research")
		f.submit(t, `{"outcome":"blocked","reason":"No access.","comment":"Stuck.","payload":{"research":{"request":"Inspect boundaries"}}}`)
		node, revision, err := AgentActivity{Policy: f.policy}.acceptRetained(t.Context(), f.service, envelope("done", retainedResearchResult))
		if err == nil || !strings.Contains(err.Error(), "retained application result result-") || !strings.Contains(err.Error(), "revision 1") || !strings.Contains(err.Error(), `"blocked"`) {
			t.Fatalf("a result that is neither ask nor done was routed: %+v %v", node, err)
		}
		if revision != 0 || node.Response != "" {
			t.Fatalf("a refused shape yielded a node: %d %+v", revision, node)
		}
	})
}

// Bug keeps its own finalization (#410 exclusion): the Respond derivation is not
// applied to it, so its terminal confirmation rules are unchanged.
func TestWorkflowPolicy_RoutesRetainedExcludesBug(t *testing.T) {
	t.Parallel()
	if BugWorkflowPolicy().routesRetained() {
		t.Fatal("Bug finalization must not derive its response from the shared mapping")
	}
	for _, p := range []WorkflowPolicy{ResearchWorkflowPolicy(), PlanWorkflowPolicy(), workflowReusePolicy()} {
		if !p.routesRetained() {
			t.Fatalf("%s policy does not derive its response from the retained result", p.Mode)
		}
	}
}

func retainedCommentOf(result string) string {
	_, rest, _ := strings.Cut(result, `"comment":"`)
	comment, _, _ := strings.Cut(rest, `"`)
	return comment
}

// AC-003: an earlier attempt's revision is a baseline to update, never this
// attempt's result, so a fresh attempt that has submitted nothing cannot complete
// on what its predecessor retained.
func TestAcceptRetained_EarlierAttemptResultIsNotThisAttemptsResult(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"research", "plan"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			f := newRetainedFixture(t, mode)
			f.submit(t, f.result)
			next := f.opts
			next.Attempt = agentResultRun(t, f.db, f.session, mode, "attempt-2", 2)
			fresh, err := BindAgentResultService(t.Context(), next)
			mustProgress(t, err)
			_, _, err = AgentActivity{Policy: f.policy}.acceptRetained(t.Context(), fresh, envelope("done", f.result))
			if !errors.Is(err, errNoRetainedResult) {
				t.Fatalf("an earlier attempt's result completed this attempt: %v", err)
			}
		})
	}
}

// AC-037: a retained result is judged against current authority when the attempt
// ends. If the factory's binding moved, or this attempt's writer was revoked, the
// attempt is refused rather than completed from a stale result.
func TestAcceptRetained_ChangedAuthorityRefusesTheRetainedResult(t *testing.T) {
	t.Parallel()
	t.Run("plan binding moved", func(t *testing.T) {
		t.Parallel()
		f := newRetainedFixture(t, "plan")
		f.submit(t, f.result)
		f.binding.SharedRevision = "moved-on"
		_, _, err := AgentActivity{Policy: f.policy}.acceptRetained(t.Context(), f.service, envelope("done", f.result))
		if err == nil || !strings.Contains(err.Error(), "retained application result is not complete") || !strings.Contains(err.Error(), "shared_revision") {
			t.Fatalf("stale retained result completed: %v", err)
		}
	})
	t.Run("plan story selection moved", func(t *testing.T) {
		t.Parallel()
		f := newRetainedFixture(t, "plan")
		f.submit(t, f.result)
		f.binding.ExistingRevision = strings.Repeat("d", 64)
		_, _, err := AgentActivity{Policy: f.policy}.acceptRetained(t.Context(), f.service, envelope("done", f.result))
		if err == nil || !strings.Contains(err.Error(), "retained application result is not complete") {
			t.Fatalf("result completed although the story now has a draft: %v", err)
		}
	})
	for _, mode := range []string{"research", "plan"} {
		t.Run(mode+"/writer revoked", func(t *testing.T) {
			t.Parallel()
			f := newRetainedFixture(t, mode)
			f.submit(t, f.result)
			next := f.opts
			next.Attempt = agentResultRun(t, f.db, f.session, mode, "attempt-2", 2)
			_, err := BindAgentResultService(t.Context(), next)
			mustProgress(t, err)
			if _, _, err = (AgentActivity{Policy: f.policy}).acceptRetained(t.Context(), f.service, envelope("done", f.result)); !errors.Is(err, store.ErrResultAuthority) {
				t.Fatalf("a revoked attempt completed: %v", err)
			}
		})
	}
}

// AC-037, AC-008: preflight records nothing; submit applies the same validation
// against current state. Unchanged state gives identical bytes the same outcome, and
// a change to the factory-selected story or stage refuses those bytes at submit
// without retaining a revision.
func TestAgentResultService_GraphPlanChangedBindingRefusedAtSubmitWithoutRetention(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		change func(*ar.PlanGraphBinding)
		code   string
	}{
		{"story selection moved", func(b *ar.PlanGraphBinding) { b.Story = "story-002" }, "binding"},
		{"story gained a draft", func(b *ar.PlanGraphBinding) { b.ExistingRevision = strings.Repeat("c", 64) }, "existing_story"},
		{"stage advanced", func(b *ar.PlanGraphBinding) { b.Stage = "acceptance" }, "story_request"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newRetainedFixture(t, "plan")
			data := []byte(`{"result":` + f.result + `}`)
			first, err := f.service.Preflight(t.Context(), data)
			if err != nil || !first.Valid {
				t.Fatalf("preflight refused: %+v %v", first, err)
			}
			second, err := f.service.Preflight(t.Context(), data)
			if err != nil || second.Valid != first.Valid || len(second.Diagnostics.Diagnostics) != len(first.Diagnostics.Diagnostics) {
				t.Fatal("identical bytes changed outcome with unchanged state")
			}
			tt.change(f.binding)
			pre, err := f.service.Preflight(t.Context(), data)
			if err != nil || pre.Valid {
				t.Fatalf("preflight accepted stale bytes: %+v %v", pre, err)
			}
			refused, err := f.service.Submit(t.Context(), "req-changed", data)
			if err != nil || refused.Validation.Valid || refused.Snapshot != nil {
				t.Fatalf("stale bytes accepted at submit: %+v %v", refused.Validation, err)
			}
			found := false
			for _, d := range refused.Validation.Diagnostics.Diagnostics {
				found = found || d.Code == tt.code
			}
			if !found {
				t.Fatalf("submit refusal lacks %q: %v", tt.code, refused.Validation.Diagnostics.Diagnostics)
			}
			if len(pre.Diagnostics.Diagnostics) != len(refused.Validation.Diagnostics.Diagnostics) {
				t.Fatal("preflight and submit disagreed about the same changed state")
			}
			if _, _, err := f.service.retainedCurrent(t.Context()); !errors.Is(err, errNoRetainedResult) {
				t.Fatalf("a refused submission retained a result: %v", err)
			}
			revisions, err := f.db.ResultRevisionNumbers(t.Context(), f.service.Binding())
			if err != nil || len(revisions) != 0 {
				t.Fatalf("revisions retained after refusal: %v %v", revisions, err)
			}
		})
	}
}

// AC-037: authority changes surface as errors at both preflight and submit and
// retain nothing: a revoked writer, a resolver that can no longer read evidence,
// and a trusted context that conflicts with the run's own binding.
func TestAgentResultService_GraphPlanAuthorityChangeRetainsNothing(t *testing.T) {
	t.Parallel()
	data := func(f *retainedFixture) []byte { return []byte(`{"result":` + f.result + `}`) }
	t.Run("writer revoked", func(t *testing.T) {
		t.Parallel()
		f := newRetainedFixture(t, "plan")
		next := f.opts
		next.Attempt = agentResultRun(t, f.db, f.session, "plan", "attempt-2", 2)
		_, err := BindAgentResultService(t.Context(), next)
		mustProgress(t, err)
		if _, err = f.service.Preflight(t.Context(), data(f)); !errors.Is(err, store.ErrResultAuthority) {
			t.Fatalf("preflight by a revoked writer: %v", err)
		}
		if _, err = f.service.Submit(t.Context(), "late", data(f)); !errors.Is(err, store.ErrResultAuthority) {
			t.Fatalf("submit by a revoked writer: %v", err)
		}
		if revisions, err := f.db.ResultRevisionNumbers(t.Context(), f.service.Binding()); err != nil || len(revisions) != 0 {
			t.Fatalf("revoked writer retained a revision: %v %v", revisions, err)
		}
	})
	t.Run("resolver fails", func(t *testing.T) {
		t.Parallel()
		f := newRetainedFixture(t, "plan")
		failing := errors.New("selected story state is unreadable")
		f.service.resolve = func(context.Context) (ar.Context, error) { return ar.Context{}, failing }
		if _, err := f.service.Preflight(t.Context(), data(f)); !errors.Is(err, failing) {
			t.Fatalf("preflight lost the cause: %v", err)
		}
		if _, err := f.service.Submit(t.Context(), "blind", data(f)); !errors.Is(err, failing) {
			t.Fatalf("submit lost the cause: %v", err)
		}
		if revisions, err := f.db.ResultRevisionNumbers(t.Context(), f.service.Binding()); err != nil || len(revisions) != 0 {
			t.Fatalf("a failed resolution retained a revision: %v %v", revisions, err)
		}
	})
	t.Run("context conflicts with the run", func(t *testing.T) {
		t.Parallel()
		f := newRetainedFixture(t, "plan")
		f.service.resolve = func(context.Context) (ar.Context, error) {
			return ar.Context{SchemaVersion: 1, WorkflowVersion: 2, Mode: "research"}, nil
		}
		if _, err := f.service.Submit(t.Context(), "wrong-mode", data(f)); err == nil || !strings.Contains(err.Error(), "conflicts with trusted run binding") {
			t.Fatalf("a foreign-mode context was trusted: %v", err)
		}
		if revisions, err := f.db.ResultRevisionNumbers(t.Context(), f.service.Binding()); err != nil || len(revisions) != 0 {
			t.Fatalf("a conflicting context retained a revision: %v %v", revisions, err)
		}
	})
}

// AC-034, AC-006, AC-036: every graph Plan branch travels the shared service and is
// retained under the factory's binding, including a story set and an ask; a
// corrected result after a refusal is a new request and is accepted, and a set that
// is refused retains nothing.
func TestAgentResultService_GraphPlanBranchesAndCorrection(t *testing.T) {
	t.Parallel()
	set := `{"outcome":"completed","comment":"Set.","payload":{"plan":{"storyset":{"entries":[{"id":"s1","name":"One","intent":"A."},{"id":"s2","name":"Two","intent":"B.","depends_on":["s1"]}]}}}}`
	cyclic := strings.Replace(set, `{"id":"s1","name":"One","intent":"A."}`, `{"id":"s1","name":"One","intent":"A.","depends_on":["s2"]}`, 1)
	ask := `{"outcome":"incomplete","reason":"Need input.","comment":"Asking.","questions":[{"id":"q1","audience":"business","text":"Which?"}],"payload":{"plan":{}}}`
	restricted := context.WithValue(t.Context(), mcpCommunicationKey{}, true)

	f := newRetainedFixture(t, "plan")
	refused, err := f.service.Submit(restricted, "req-cyclic", []byte(`{"result":`+cyclic+`}`))
	if err != nil || refused.Validation.Valid || refused.Snapshot != nil {
		t.Fatalf("cyclic set accepted: %+v %v", refused.Validation, err)
	}
	if revisions, err := f.db.ResultRevisionNumbers(t.Context(), f.service.Binding()); err != nil || len(revisions) != 0 {
		t.Fatalf("a refused set was retained: %v %v", revisions, err)
	}
	accepted, err := f.service.Submit(restricted, "req-fixed", []byte(`{"result":`+set+`}`))
	if err != nil || !accepted.Validation.Valid || accepted.Snapshot == nil {
		t.Fatalf("corrected set refused: %+v %v", accepted.Validation, err)
	}
	got := accepted.Snapshot.Result.Payload.Plan.StorySet
	if got == nil || len(got.Entries) != 2 || got.Entries[0].ID != "s1" || got.Entries[1].DependsOn[0] != "s1" {
		t.Fatalf("set not retained in order: %+v", got)
	}
	if accepted.Snapshot.Result.Payload.Plan.StoryRequest != nil {
		t.Fatal("a story request was fabricated for a set")
	}
	// An ask is retained as questions only, with no branch.
	g := newRetainedFixture(t, "plan")
	answered, err := g.service.Submit(restricted, "req-ask", []byte(`{"result":`+ask+`}`))
	if err != nil || !answered.Validation.Valid || answered.Snapshot == nil {
		t.Fatalf("ask refused: %+v %v", answered.Validation, err)
	}
	plan := answered.Snapshot.Result.Payload.Plan
	if plan.StoryRequest != nil || plan.StorySet != nil || len(answered.Snapshot.Result.Questions) != 1 {
		t.Fatalf("an ask fabricated a draft: %+v", plan)
	}
	// Legacy Plan, bound without a graph binding, refuses both graph branches.
	legacy, _, lo := agentResultFixture(t, "plan")
	service, err := BindAgentResultService(t.Context(), lo)
	mustProgress(t, err)
	for name, result := range map[string]string{"story request": retainedPlanResult, "story set": set} {
		d, err := service.Preflight(t.Context(), []byte(`{"result":`+result+`}`))
		if err != nil || d.Valid {
			t.Fatalf("legacy Plan accepted a %s: %+v %v", name, d, err)
		}
	}
	if revisions, err := legacy.ResultRevisionNumbers(t.Context(), service.Binding()); err != nil || len(revisions) != 0 {
		t.Fatalf("legacy retained a graph branch: %v %v", revisions, err)
	}
}
