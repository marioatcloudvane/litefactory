package access

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/teamsource"
	"github.com/cloudvane/litefactory/internal/ticket"
)

const origin = "https://api.github.com"

func ident(id string) ticket.Identity {
	return ticket.Identity{Provider: ticket.GitHub, Origin: origin, RemoteID: id}
}

type fakeStore struct {
	mu        sync.Mutex
	grants    map[string]store.AccessGrant
	rows      []store.AccessDecision
	extra     int // checks counted beyond the stored rows
	countErr  error
	lastSince time.Time
}

func newFakeStore() *fakeStore { return &fakeStore{grants: map[string]store.AccessGrant{}} }

func (s *fakeStore) grant(actor ticket.Identity, text string) {
	s.grants[actor.RemoteID] = store.AccessGrant{ID: 7, RepoID: 1, Revision: 2, Actor: actor, Handle: "guest", Text: text}
}

func (s *fakeStore) OpenAccessGrant(_ context.Context, repoID int64, actor ticket.Identity) (store.AccessGrant, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g, ok := s.grants[actor.RemoteID]
	if !ok || g.RepoID != repoID {
		return store.AccessGrant{}, store.ErrNotFound
	}
	return g, nil
}

func (s *fakeStore) find(repoID int64, source ticket.Identity) int {
	for i, d := range s.rows {
		if d.RepoID == repoID && d.Source == source {
			return i
		}
	}
	return -1
}

func (s *fakeStore) BeginAccessDecision(_ context.Context, d store.AccessDecision) (store.AccessDecision, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if i := s.find(d.RepoID, d.Source); i >= 0 {
		return s.rows[i], false, nil
	}
	d.ID = int64(len(s.rows) + 1)
	d.State = store.AccessDecisionPending
	s.rows = append(s.rows, d)
	return d, true, nil
}

func (s *fakeStore) FinishAccessDecision(_ context.Context, id int64, out store.AccessDecisionOutcome) (store.AccessDecision, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, d := range s.rows {
		if d.ID != id {
			continue
		}
		if d.State != store.AccessDecisionPending {
			return store.AccessDecision{}, store.ErrAccessDecisionFinal
		}
		if out.Reason == "" || strings.ContainsAny(out.Reason, "\r\n") || len([]rune(out.Reason)) > store.MaxAccessDecisionReason {
			return store.AccessDecision{}, errors.New("bad reason")
		}
		d.State, d.Decision, d.Reason, d.Model, d.Classified = store.AccessDecisionFinal, out.Decision, out.Reason, out.Model, out.Classified
		s.rows[i] = d
		return d, nil
	}
	return store.AccessDecision{}, store.ErrNotFound
}

func (s *fakeStore) CountAccessChecks(_ context.Context, repoID int64, actor ticket.Identity, since time.Time, excludeID int64) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastSince = since
	if s.countErr != nil {
		return 0, s.countErr
	}
	n := s.extra
	for _, d := range s.rows {
		if d.RepoID == repoID && d.Actor == actor && d.ID != excludeID && (d.Classified || d.State == store.AccessDecisionPending) {
			n++
		}
	}
	return n, nil
}

func (s *fakeStore) AccessDecisionBySource(_ context.Context, repoID int64, source ticket.Identity) (store.AccessDecision, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if i := s.find(repoID, source); i >= 0 {
		return s.rows[i], nil
	}
	return store.AccessDecision{}, store.ErrNotFound
}

type fakeChecker struct {
	mu     sync.Mutex
	calls  int
	last   agent.AccessCheckRequest
	result agent.AccessCheckResult
	err    error
}

func (c *fakeChecker) CheckAccess(ctx context.Context, req agent.AccessCheckRequest) (agent.AccessCheckResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	c.last = req
	if _, ok := ctx.Deadline(); !ok {
		return agent.AccessCheckResult{}, errors.New("checker context has no deadline")
	}
	return c.result, c.err
}

const fixtureRole = "fixture classifier role"

func fixtureFacts(_ context.Context, _ store.Repo, target Target) (TargetFacts, error) {
	return TargetFacts{Workflow: string(target.Workflow), Entry: target.Entry, Node: "first", Action: "lf_agent",
		Nodes: []NodeFact{{Name: "first", Action: "lf_agent", Access: "read"}, {Name: "second", Action: "lf_agent", Access: "write"}}}, nil
}

func newGate(s *fakeStore, c *fakeChecker) *Gate {
	return &Gate{
		Store:   s,
		Facts:   fixtureFacts,
		Role:    func(context.Context, store.Repo) (string, error) { return fixtureRole, nil },
		Checker: c,
		Now:     func() time.Time { return time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC) },
	}
}

func request(source string) Request {
	return Request{
		Repo: store.Repo{ID: 1}, SessionID: 3, Actor: ident("guest"), Handle: "guest", Source: ident(source),
		IssueTitle: "Title", IssueBody: "Body", Text: "please do the thing",
		Target: Target{Entry: EntryStart, Workflow: agent.Workflow("alpha")},
	}
}

func setup(t *testing.T) (*fakeStore, *fakeChecker, *Gate) {
	t.Helper()
	s := newFakeStore()
	s.grant(ident("guest"), "may ask questions")
	c := &fakeChecker{result: agent.AccessCheckResult{Decision: agent.AccessAllow, Reason: "Covered.", Model: "m1"}}
	return s, c, newGate(s, c)
}

func mustCheck(t *testing.T, g *Gate, r Request) Outcome {
	t.Helper()
	out, err := g.Check(context.Background(), r)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	return out
}

func TestGateAllow(t *testing.T) {
	s, c, g := setup(t)
	out := mustCheck(t, g, request("c1"))
	if !out.Allowed() || out.Reason != "Covered." || out.Replayed {
		t.Fatalf("outcome %+v", out)
	}
	if c.calls != 1 || c.last.Role != fixtureRole || c.last.GrantText != "may ask questions" || c.last.Text != "please do the thing" {
		t.Fatalf("checker request %+v calls %d", c.last, c.calls)
	}
	var facts TargetFacts
	if err := json.Unmarshal(c.last.Target, &facts); err != nil || facts.Workflow != "alpha" || len(facts.Nodes) != 2 {
		t.Fatalf("target %s: %v", c.last.Target, err)
	}
	row := s.rows[0]
	if row.Decision != store.AccessAllow || !row.Classified || row.Model != "m1" || row.GrantID != 7 || row.GrantRevision != 2 || row.RequestDigest != Digest("please do the thing") || row.TargetJSON != string(c.last.Target) {
		t.Fatalf("row %+v", row)
	}
	if want := g.Now().Add(-time.Hour); !s.lastSince.Equal(want) {
		t.Fatalf("since %v want %v", s.lastSince, want)
	}
}

func TestGateDeny(t *testing.T) {
	s, c, g := setup(t)
	c.result = agent.AccessCheckResult{Decision: agent.AccessDeny, Reason: "Not\nin the grant.", Model: "m1"}
	out := mustCheck(t, g, request("c1"))
	if out.Allowed() || out.Refusal != RefusedDenied || out.Reason != "Not in the grant." {
		t.Fatalf("outcome %+v", out)
	}
	if s.rows[0].Decision != store.AccessDeny || !s.rows[0].Classified {
		t.Fatalf("row %+v", s.rows[0])
	}
}

func TestGateReplayMakesNoSecondCheck(t *testing.T) {
	_, c, g := setup(t)
	first := mustCheck(t, g, request("c1"))
	second := mustCheck(t, g, request("c1"))
	if c.calls != 1 || !second.Replayed || second.DecisionID != first.DecisionID || second.Refusal != first.Refusal || second.Reason != first.Reason {
		t.Fatalf("first %+v second %+v calls %d", first, second, c.calls)
	}
	edited := request("c1")
	edited.Text = "something else"
	if out := mustCheck(t, g, edited); out.Allowed() || out.Refusal != RefusedUnchecked || c.calls != 1 {
		t.Fatalf("edited replay %+v calls %d", out, c.calls)
	}
}

func TestGateReplayMapsDecisions(t *testing.T) {
	for decision, want := range map[string]Refusal{
		store.AccessAllow: RefusedNone, store.AccessDeny: RefusedDenied, store.AccessFloor: RefusedOwnerOnly,
		store.AccessError: RefusedUnchecked, store.AccessLimited: RefusedLimited, store.AccessRateLimited: RefusedLimited,
	} {
		s, c, g := setup(t)
		r := request("c1")
		s.rows = append(s.rows, store.AccessDecision{ID: 9, RepoID: 1, Source: r.Source, Actor: r.Actor, State: store.AccessDecisionFinal, Decision: decision, Reason: "stored", RequestDigest: Digest(r.Text)})
		out := mustCheck(t, g, r)
		if out.Refusal != want || out.Reason != "stored" || out.DecisionID != 9 || !out.Replayed || c.calls != 0 {
			t.Fatalf("%s: %+v calls %d", decision, out, c.calls)
		}
	}
}

func TestGatePendingRowIsRechecked(t *testing.T) {
	s, c, g := setup(t)
	r := request("c1")
	raw, _ := json.Marshal(mustFacts(t, r))
	s.rows = append(s.rows, store.AccessDecision{ID: 1, RepoID: 1, SessionID: 3, Source: r.Source, Actor: r.Actor, GrantID: 7, GrantRevision: 2, TargetJSON: string(raw), RequestDigest: Digest(r.Text), State: store.AccessDecisionPending})
	s.extra = HourlyChecks - 1
	out := mustCheck(t, g, r)
	if !out.Allowed() || out.DecisionID != 1 || out.Replayed || c.calls != 1 || len(s.rows) != 1 || s.rows[0].State != store.AccessDecisionFinal {
		t.Fatalf("outcome %+v calls %d rows %+v", out, c.calls, s.rows)
	}
}

func mustFacts(t *testing.T, r Request) TargetFacts {
	t.Helper()
	f, err := fixtureFacts(context.Background(), r.Repo, r.Target)
	if err != nil {
		t.Fatal(err)
	}
	f.Command = r.Command
	return f
}

func TestGatePendingRowWithOtherTextIsUnchecked(t *testing.T) {
	s, c, g := setup(t)
	r := request("c1")
	s.rows = append(s.rows, store.AccessDecision{ID: 1, RepoID: 1, SessionID: 3, Source: r.Source, Actor: r.Actor, GrantID: 7, GrantRevision: 2, TargetJSON: "{}", RequestDigest: Digest("older text"), State: store.AccessDecisionPending})
	out := mustCheck(t, g, r)
	if out.Refusal != RefusedUnchecked || c.calls != 0 || s.rows[0].Decision != store.AccessError {
		t.Fatalf("outcome %+v calls %d row %+v", out, c.calls, s.rows[0])
	}
}

func TestGateFloorCodeControl(t *testing.T) {
	s, c, g := setup(t)
	r := request("c1")
	r.CodeControl = true
	out := mustCheck(t, g, r)
	if out.Refusal != RefusedOwnerOnly || out.Reason != "Only the owner can do this." || c.calls != 0 || s.rows[0].Decision != store.AccessFloor || s.rows[0].Classified {
		t.Fatalf("outcome %+v calls %d row %+v", out, c.calls, s.rows[0])
	}
}

func TestGateFloorApproveReply(t *testing.T) {
	s, c, g := setup(t)
	r := request("c1")
	r.Target = Target{Entry: EntryAnswer, Workflow: "alpha", Node: "wait", ApproveReply: "ship"}
	r.Text = "Ship it!"
	out := mustCheck(t, g, r)
	if out.Refusal != RefusedOwnerOnly || c.calls != 0 || s.rows[0].Decision != store.AccessFloor {
		t.Fatalf("outcome %+v calls %d", out, c.calls)
	}
	other := request("c2")
	other.Target = r.Target
	other.Text = "please ship after fixing the typo"
	if out := mustCheck(t, g, other); !out.Allowed() || c.calls != 1 {
		t.Fatalf("non-matching reply %+v calls %d", out, c.calls)
	}
}

func TestGateSizeCap(t *testing.T) {
	s, c, g := setup(t)
	r := request("c1")
	r.IssueBody = strings.Repeat("x", MaxRequestBytes-len(r.IssueTitle)-len(r.Text))
	if out := mustCheck(t, g, r); !out.Allowed() {
		t.Fatalf("at cap %+v", out)
	}
	r = request("c2")
	r.IssueBody = strings.Repeat("x", MaxRequestBytes-len(r.IssueTitle)-len(r.Text)+1)
	out := mustCheck(t, g, r)
	if out.Refusal != RefusedLimited || c.calls != 1 || s.rows[1].Decision != store.AccessLimited {
		t.Fatalf("over cap %+v calls %d", out, c.calls)
	}
}

func TestGateHourlyCap(t *testing.T) {
	s, c, g := setup(t)
	for i := range HourlyChecks {
		if out := mustCheck(t, g, request("c"+string(rune('a'+i)))); !out.Allowed() {
			t.Fatalf("check %d: %+v", i, out)
		}
	}
	out := mustCheck(t, g, request("over"))
	if out.Refusal != RefusedLimited || c.calls != HourlyChecks || s.rows[HourlyChecks].Decision != store.AccessRateLimited {
		t.Fatalf("outcome %+v calls %d", out, c.calls)
	}
	if !strings.Contains(out.Reason, "last hour") {
		t.Fatalf("reason %q", out.Reason)
	}
}

func TestGateUncheckedRefusals(t *testing.T) {
	cases := map[string]func(*Gate, *fakeChecker){
		"facts error": func(g *Gate, _ *fakeChecker) {
			g.Facts = func(context.Context, store.Repo, Target) (TargetFacts, error) {
				return TargetFacts{}, errors.New("no graph")
			}
		},
		"role error": func(g *Gate, _ *fakeChecker) {
			g.Role = func(context.Context, store.Repo) (string, error) { return "", errors.New("no role") }
		},
		"nil facts":   func(g *Gate, _ *fakeChecker) { g.Facts = nil },
		"nil checker": func(g *Gate, _ *fakeChecker) { g.Checker = nil },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			s, c, g := setup(t)
			mutate(g, c)
			out := mustCheck(t, g, request("c1"))
			if out.Refusal != RefusedUnchecked || out.Reason != "The request could not be checked." || c.calls != 0 || s.rows[0].Decision != store.AccessError || s.rows[0].Classified {
				t.Fatalf("outcome %+v calls %d row %+v", out, c.calls, s.rows[0])
			}
		})
	}
	checkerCases := map[string]func(*fakeChecker){
		"checker error": func(c *fakeChecker) { c.err = errors.New("boom"); c.result = agent.AccessCheckResult{Model: "m2"} },
		"bad decision":  func(c *fakeChecker) { c.result = agent.AccessCheckResult{Decision: "maybe", Reason: "hm", Model: "m2"} },
	}
	for name, mutate := range checkerCases {
		t.Run(name, func(t *testing.T) {
			s, c, g := setup(t)
			mutate(c)
			out := mustCheck(t, g, request("c1"))
			row := s.rows[0]
			if out.Refusal != RefusedUnchecked || c.calls != 1 || row.Decision != store.AccessError || !row.Classified || row.Model != "m2" {
				t.Fatalf("outcome %+v calls %d row %+v", out, c.calls, row)
			}
		})
	}
}

func TestGateNoGrant(t *testing.T) {
	s, c, g := setup(t)
	r := request("c1")
	r.Actor = ident("stranger")
	if _, err := g.Check(context.Background(), r); !errors.Is(err, ErrNoGrant) {
		t.Fatalf("err %v", err)
	}
	other := ident("guest")
	other.Origin = "https://github.example.com/api/v3"
	s.grants["guest"] = store.AccessGrant{ID: 7, RepoID: 1, Revision: 1, Actor: other, Text: "x"}
	if _, err := g.Check(context.Background(), request("c2")); !errors.Is(err, ErrNoGrant) {
		t.Fatalf("other origin err %v", err)
	}
	if c.calls != 0 || len(s.rows) != 0 {
		t.Fatalf("calls %d rows %d", c.calls, len(s.rows))
	}
}

func TestGateActorMismatchOnExistingRow(t *testing.T) {
	s, c, g := setup(t)
	r := request("c1")
	s.rows = append(s.rows, store.AccessDecision{ID: 1, RepoID: 1, Source: r.Source, Actor: ident("someone"), State: store.AccessDecisionFinal, Decision: store.AccessAllow, Reason: "ok", RequestDigest: Digest(r.Text)})
	out := mustCheck(t, g, r)
	if out.Allowed() || out.Refusal != RefusedUnchecked || c.calls != 0 || s.rows[0].Decision != store.AccessAllow || len(s.rows) != 1 {
		t.Fatalf("outcome %+v calls %d", out, c.calls)
	}
}

func TestGateStorageErrorIsReturned(t *testing.T) {
	s, c, g := setup(t)
	s.countErr = errors.New("disk")
	if _, err := g.Check(context.Background(), request("c1")); err == nil || c.calls != 0 {
		t.Fatalf("err %v calls %d", err, c.calls)
	}
}

func TestGateRejectsMalformedRequest(t *testing.T) {
	_, _, g := setup(t)
	for name, mutate := range map[string]func(*Request){
		"repo":    func(r *Request) { r.Repo.ID = 0 },
		"session": func(r *Request) { r.SessionID = 0 },
		"actor":   func(r *Request) { r.Actor = ticket.Identity{} },
		"source":  func(r *Request) { r.Source = ticket.Identity{} },
	} {
		r := request("c1")
		mutate(&r)
		if _, err := g.Check(context.Background(), r); err == nil {
			t.Fatalf("%s: no error", name)
		}
	}
}

func TestDigestIsPlainSHA256(t *testing.T) {
	if got := Digest("abc"); got != "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" {
		t.Fatalf("digest %s", got)
	}
}

const fixtureDOT = `digraph {
 graph [start="ask"];
 ask [lf_action="lf_agent", lf_kind="question"];
 build [lf_action="lf_agent", lf_access="write"];
 wait [lf_action="lf_ask", lf_kind="review", lf_approve_reply="ship"];
 done [lf_action="lf_comment", lf_kind="final"];
 orphan [lf_action="lf_agent"];
 ask -> build [on="ok"];
 build -> wait [on="ok"];
 wait -> done [on="approved"];
 wait -> build [on="published"];
}`

func TestFactsFromDefinition(t *testing.T) {
	profile, err := graphexec.ParseProfile([]byte(fixtureDOT))
	if err != nil {
		t.Fatal(err)
	}
	def := &teamsource.WorkflowDefinition{Profile: profile}
	start, err := FactsFromDefinition(def, Target{Entry: EntryStart, Workflow: "alpha"})
	if err != nil {
		t.Fatal(err)
	}
	if start.Node != "ask" || start.Kind != "question" || start.Action != "lf_agent" || start.Workflow != "alpha" {
		t.Fatalf("start %+v", start)
	}
	want := []NodeFact{{Name: "ask", Kind: "question", Action: "lf_agent", Access: "read"}, {Name: "build", Action: "lf_agent", Access: "write"}}
	if len(start.Nodes) != len(want) || start.Nodes[0] != want[0] || start.Nodes[1] != want[1] {
		t.Fatalf("nodes %+v", start.Nodes)
	}
	answer, err := FactsFromDefinition(def, Target{Entry: EntryAnswer, Workflow: "alpha", Node: "wait"})
	if err != nil {
		t.Fatal(err)
	}
	if answer.Node != "wait" || answer.Action != "lf_ask" || len(answer.Nodes) != 1 || answer.Nodes[0].Name != "build" {
		t.Fatalf("answer %+v", answer)
	}
	if _, err := FactsFromDefinition(def, Target{Entry: EntryAnswer, Workflow: "alpha", Node: "missing"}); err == nil {
		t.Fatal("missing node accepted")
	}
	if _, err := FactsFromDefinition(def, Target{Entry: EntryAnswer, Workflow: "alpha"}); err == nil {
		t.Fatal("empty answer node accepted")
	}
	if got := ApproveReplyOf(def, "wait"); got != "ship" {
		t.Fatalf("approve reply %q", got)
	}
	if got := ApproveReplyOf(def, "ask"); got != "" {
		t.Fatalf("approve reply %q", got)
	}
}

func TestResolverFacts(t *testing.T) {
	profile, err := graphexec.ParseProfile([]byte(fixtureDOT))
	if err != nil {
		t.Fatal(err)
	}
	var gotWorkflow agent.Workflow
	resolve := func(_ context.Context, _ store.Repo, w agent.Workflow, _ graphexec.Registry) (*teamsource.WorkflowDefinition, error) {
		gotWorkflow = w
		return &teamsource.WorkflowDefinition{Profile: profile}, nil
	}
	facts := ResolverFacts(resolve, func(agent.Workflow) (graphexec.Registry, error) { return graphexec.Registry{}, nil })
	got, err := facts(context.Background(), store.Repo{ID: 1}, Target{Entry: EntryStart, Workflow: "alpha"})
	if err != nil || got.Node != "ask" || gotWorkflow != "alpha" {
		t.Fatalf("facts %+v err %v workflow %q", got, err, gotWorkflow)
	}
	failing := ResolverFacts(resolve, func(agent.Workflow) (graphexec.Registry, error) { return graphexec.Registry{}, errors.New("unknown") })
	if _, err := failing(context.Background(), store.Repo{ID: 1}, Target{Entry: EntryStart, Workflow: "alpha"}); err == nil {
		t.Fatal("registry error ignored")
	}
}
