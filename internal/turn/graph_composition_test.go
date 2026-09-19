package turn

import (
	"crypto/sha256"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
)

const graphCompositionDOT = `digraph {
 team=test; mode=implement; profile_version=1; start=prepare;
 prepare [lf_action=lf_implement_work,lf_outcomes=done];
 verify [lf_action=lf_implement_work,lf_outcomes=done];
 end [lf_terminal=true];
 prepare -> verify [on=done]; verify -> end [on=done];
}`

// These tests compose the real parser, dispatcher and migrated SQLite store.
// The only fake is the future activity implementation, registered once by the
// existing turn fixture. Reopening covers committed steps, not in-flight recovery.
func TestGraphCompositionTraversal(t *testing.T) {
	for _, reopen := range []bool{false, true} {
		t.Run(fmt.Sprintf("reopen=%t", reopen), func(t *testing.T) {
			f := newFixture(t, nil)
			h := &graphTestHandler{results: map[string][]ActivityResult{
				"prepare": {{Outcome: "done", Kind: store.GraphOutputText, Output: []byte("prepared")}},
				"verify":  {{Outcome: "done", Kind: store.GraphOutputJSON, Output: []byte(`{"valid":true}`)}},
			}}
			req := f.graphRequest(graphCompositionDOT, h)
			start := req.At
			instance, err := f.db.GetGraphInstance(t.Context(), req.InstanceID)
			if err != nil {
				t.Fatal(err)
			}
			definition, err := f.db.GetGraphDefinition(t.Context(), instance.DefinitionID)
			if err != nil {
				t.Fatal(err)
			}
			wantDefinition := store.GraphDefinition{ID: definition.ID, Team: "test", Mode: "implement", ProfileVersion: 1, Path: "test/implement.dot", Digest: fmt.Sprintf("%x", sha256.Sum256([]byte(graphCompositionDOT))), CreatedAt: start}
			if !reflect.DeepEqual(definition, wantDefinition) {
				t.Fatalf("definition = %+v, want %+v", definition, wantDefinition)
			}
			if req.Profile.Digest != wantDefinition.Digest {
				t.Fatal("profile digest differs from exact DOT bytes")
			}
			var wantAttempts []store.GraphNodeAttempt
			var wantTransitions []store.GraphTransition
			for i, node := range []string{"prepare", "verify", "end"} {
				req.At = start.Add(time.Duration(i) * time.Minute)
				req.NextDueAt = req.At.Add(time.Minute)
				result, err := ExecuteNodeAction(t.Context(), f.opts, req)
				if err != nil {
					t.Fatal(err)
				}
				if result.Node != node {
					t.Fatalf("step %d resumed %q, want %q", i, result.Node, node)
				}
				if node != "end" {
					next := []string{"verify", "end"}[i]
					wantAttempts = append(wantAttempts, store.GraphNodeAttempt{ID: result.AttemptID, InstanceID: req.InstanceID, Node: node, Attempt: 1, Status: store.GraphSucceeded, Outcome: "done", StartedAt: req.At, FinishedAt: req.At})
					traces, err := f.db.ListGraphTransitions(t.Context(), req.InstanceID)
					if err != nil {
						t.Fatal(err)
					}
					if len(traces) != i+1 {
						t.Fatalf("trace count = %d, want %d", len(traces), i+1)
					}
					wantTransitions = append(wantTransitions, store.GraphTransition{ID: traces[i].ID, InstanceID: req.InstanceID, FromNode: node, OnOutcome: "done", ToNode: next, At: req.At})
					if !reflect.DeepEqual(traces, wantTransitions) {
						t.Fatalf("ordered trace = %+v, want %+v", traces, wantTransitions)
					}
					outputs, err := f.db.ListGraphOutputs(t.Context(), result.AttemptID)
					if err != nil {
						t.Fatal(err)
					}
					if len(outputs) != 1 {
						t.Fatalf("outputs = %+v", outputs)
					}
					wantOutput := store.GraphOutput{ID: outputs[0].ID, NodeAttemptID: result.AttemptID, Kind: []string{store.GraphOutputText, store.GraphOutputJSON}[i], Payload: [][]byte{[]byte("prepared"), []byte(`{"valid":true}`)}[i], CreatedAt: req.At}
					if !reflect.DeepEqual(outputs[0], wantOutput) {
						t.Fatalf("output = %+v, want %+v", outputs[0], wantOutput)
					}
				} else if result.AttemptID != 0 || result.Status != store.GraphSucceeded {
					t.Fatalf("terminal = %+v", result)
				}
				attempts, err := f.db.ListGraphNodeAttempts(t.Context(), req.InstanceID)
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(attempts, wantAttempts) {
					t.Fatalf("attempts = %+v, want %+v", attempts, wantAttempts)
				}
				if reopen && i == 0 {
					before := readGraphCompositionState(t, f.db, req.InstanceID)
					path := f.db.Path()
					if err := f.db.Close(); err != nil {
						t.Fatal(err)
					}
					db, err := store.Open(t.Context(), path)
					if err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() {
						if err := db.Close(); err != nil {
							t.Error(err)
						}
					})
					f.db, f.opts.DB = db, db
					after := readGraphCompositionState(t, db, req.InstanceID)
					if !reflect.DeepEqual(after, before) {
						t.Fatalf("reopened state = %+v, want %+v", after, before)
					}
					if after.instance.CurrentNode != "verify" {
						t.Fatalf("persisted continuation = %+v", after.instance)
					}
				}
			}
			final := readGraphCompositionState(t, f.db, req.InstanceID)
			wantInstance := store.GraphInstance{ID: instance.ID, SessionID: f.session.ID, DefinitionID: definition.ID, Status: store.GraphSucceeded, CurrentNode: "end", StartedAt: start, FinishedAt: req.At}
			if !reflect.DeepEqual(final.instance, wantInstance) || !reflect.DeepEqual(final.definition, wantDefinition) || !reflect.DeepEqual(final.transitions, wantTransitions) || !reflect.DeepEqual(final.attempts, wantAttempts) {
				t.Fatalf("unexpected terminal records: %+v", final)
			}
			if len(h.calls) != 2 {
				t.Fatalf("dispatches = %d, want 2", len(h.calls))
			}
		})
	}
}

func TestGraphCompositionBoundedLoop(t *testing.T) {
	f := newFixture(t, nil)
	h := &graphTestHandler{results: map[string][]ActivityResult{
		"first":  {{Outcome: "again"}, {Outcome: "again"}},
		"second": {{Outcome: "again"}, {Outcome: "again"}},
	}}
	req := f.graphRequest(`digraph {team=test;mode=implement;profile_version=1;start=first;
 first [lf_action=lf_implement_work,lf_outcomes="again,done",lf_limit=2];
 second [lf_action=lf_implement_work,lf_outcomes=again,lf_limit=3];
 end [lf_terminal=true];first -> second [on=again];second -> first [on=again];first -> end [on=done];}`, h)
	start := req.At
	var previous graphCompositionState
	for i, node := range []string{"first", "second", "first", "second"} {
		req.At = start.Add(time.Duration(i) * time.Minute)
		result, err := ExecuteNodeAction(t.Context(), f.opts, req)
		if err != nil {
			t.Fatal(err)
		}
		current := readGraphCompositionState(t, f.db, req.InstanceID)
		if result.Node != node || len(current.attempts) != i+1 || len(current.transitions) != i+1 {
			t.Fatalf("step %d: %+v %+v", i, result, current)
		}
		if i > 0 && (!reflect.DeepEqual(current.attempts[:i], previous.attempts) || !reflect.DeepEqual(current.transitions[:i], previous.transitions)) {
			t.Fatal("loop rewrote completed history")
		}
		a := current.attempts[i]
		if a.Node != node || a.Attempt != i/2+1 || a.Status != store.GraphSucceeded || a.Outcome != "again" || !a.StartedAt.Equal(req.At) || !a.FinishedAt.Equal(req.At) {
			t.Fatalf("attempt: %+v", a)
		}
		tr := current.transitions[i]
		next := "first"
		if node == "first" {
			next = "second"
		}
		if tr.FromNode != node || tr.OnOutcome != "again" || tr.ToNode != next || !tr.At.Equal(req.At) {
			t.Fatalf("transition: %+v", tr)
		}
		previous = current
	}
	req.At = start.Add(4 * time.Minute)
	result, err := ExecuteNodeAction(t.Context(), f.opts, req)
	if err != nil {
		t.Fatal(err)
	}
	final := readGraphCompositionState(t, f.db, req.InstanceID)
	if result.Status != store.GraphFailed || result.AttemptID != 0 || !strings.Contains(result.Activity.Diagnostic, "limit 2 exhausted") || len(h.calls) != 4 {
		t.Fatalf("exhaustion result: %+v dispatches=%d", result, len(h.calls))
	}
	if final.instance.Status != store.GraphFailed || final.instance.CurrentNode != "first" || !final.instance.NextDueAt.IsZero() || !final.instance.FinishedAt.Equal(req.At) {
		t.Fatalf("exhausted instance: %+v", final.instance)
	}
	if !reflect.DeepEqual(final.attempts, previous.attempts) || !reflect.DeepEqual(final.transitions, previous.transitions) || !reflect.DeepEqual(final.outputs, previous.outputs) {
		t.Fatal("exhaustion rewrote completed history or reserved an extra attempt")
	}
}

type graphCompositionState struct {
	definition  store.GraphDefinition
	instance    store.GraphInstance
	attempts    []store.GraphNodeAttempt
	outputs     []store.GraphOutput
	transitions []store.GraphTransition
}

func readGraphCompositionState(t *testing.T, db *store.DB, id int64) graphCompositionState {
	t.Helper()
	var s graphCompositionState
	var err error
	s.instance, err = db.GetGraphInstance(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	s.definition, err = db.GetGraphDefinition(t.Context(), s.instance.DefinitionID)
	if err != nil {
		t.Fatal(err)
	}
	s.attempts, err = db.ListGraphNodeAttempts(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	for _, attempt := range s.attempts {
		outputs, err := db.ListGraphOutputs(t.Context(), attempt.ID)
		if err != nil {
			t.Fatal(err)
		}
		s.outputs = append(s.outputs, outputs...)
	}
	s.transitions, err = db.ListGraphTransitions(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
