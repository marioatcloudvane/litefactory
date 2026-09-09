package meter

import (
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
)

func observation() agent.UsageObservation {
	model := "verified-model"
	return agent.UsageObservation{AttemptID: "a", SourceID: "s", EventKey: "event-1", InvocationID: "root", ActualModel: &model, ModelProvenance: "response", Mode: agent.CounterCumulative, Scope: agent.UsageExclusive, CounterKey: "tokens", CounterEpoch: "epoch-1", Tokens: agent.TokenUsage{Output: count(120)}}
}

func TestResumeReplayAndRepeatedWork(t *testing.T) {
	t.Parallel()
	o := observation()
	baseline := &CounterSnapshot{Identity: CounterIdentity(o), Tokens: agent.TokenUsage{Output: count(100)}}
	first, err := Reconcile(o, baseline, "")
	if err != nil {
		t.Fatal(err)
	}
	if !first.Additive || *first.Delta.Output != 20 {
		t.Fatal("charged lifetime counter")
	}
	// Replay lookup is keyed by durable event identity, even after newer events.
	second := o.Clone()
	second.EventKey = "event-2"
	second.Tokens.Output = count(150)
	next, err := Reconcile(second, first.Next, "")
	if err != nil || *next.Delta.Output != 30 {
		t.Fatalf("next: %v", err)
	}
	replay, err := Reconcile(o, next.Next, first.Digest)
	if err != nil || !replay.Duplicate || replay.Additive || replay.Next != nil {
		t.Fatalf("replay: %+v %v", replay, err)
	}
	conflict := o.Clone()
	conflict.Tokens.Output = count(121)
	if _, err := Reconcile(conflict, next.Next, first.Digest); err == nil {
		t.Fatal("accepted conflicting event")
	}
	// A new event with equal cumulative value contributes measured zero.
	second.EventKey = "event-3"
	same, err := Reconcile(second, next.Next, "")
	if err != nil || *same.Delta.Output != 0 {
		t.Fatalf("same snapshot: %v", err)
	}
	o.Mode = agent.CounterIncremental
	o.Tokens.Output = count(20)
	a, err := Reconcile(o, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	o.EventKey = "repeated-work"
	b, err := Reconcile(o, nil, "")
	if err != nil || *a.Delta.Output+*b.Delta.Output != 40 || a.Identity == b.Identity {
		t.Fatal("discarded genuinely repeated work")
	}
}

func TestReconcileScopeAndOwnership(t *testing.T) {
	t.Parallel()
	o := observation()
	if _, err := Reconcile(o, nil, ""); err == nil {
		t.Fatal("missing baseline accepted")
	}
	baseline := &CounterSnapshot{Identity: CounterIdentity(o), Tokens: agent.TokenUsage{Output: count(100)}}
	o.AttemptID = "other"
	if _, err := Reconcile(o, baseline, ""); err == nil {
		t.Fatal("cross-attempt baseline accepted")
	}
	o.Scope = agent.UsageInclusive
	o.Mode = agent.CounterIncremental
	r, err := Reconcile(o, nil, "")
	if err != nil || r.Additive || r.Delta.Output != nil {
		t.Fatal("inclusive evidence added")
	}
	o.Scope = agent.UsageExclusive
	r, err = Reconcile(o, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	*r.Delta.Output = 1
	if *o.Tokens.Output != 120 {
		t.Fatal("delta aliases source")
	}
}

func TestIdentityTupleCannotCollide(t *testing.T) {
	t.Parallel()
	a := observation()
	b := a.Clone()
	a.AttemptID = "a\x00b"
	a.SourceID = "c"
	b.AttemptID = "a"
	b.SourceID = "b\x00c"
	if ObservationIdentity(a) == ObservationIdentity(b) {
		t.Fatal("tuple collision")
	}
	a = observation()
	b = a.Clone()
	b.ActualModel = nil
	if CounterIdentity(a) == CounterIdentity(b) {
		t.Fatal("model identity collision")
	}
}

func TestCapabilityCoverage(t *testing.T) {
	t.Parallel()
	e := CapabilityEvidence{Version: "1", SourceFormat: "fixture-v1"}
	if got := e.Coverage(); got.Status != agent.CollectionUnknown || got.Diagnostics[0] != "unsupported_source_version" {
		t.Fatal(got)
	}
	e.Supported = true
	if got := e.Coverage(); got.Diagnostics[0] != "missing_authentic_evidence" {
		t.Fatal(got)
	}
	e.Authentic = true
	e.Provenance = "external-capture"
	e.Root = true
	if got := e.Coverage(); got.Status != agent.CollectionPartial {
		t.Fatal(got)
	}
	e.Parallel = true
	e.Nested = true
	e.ActualModels = true
	e.ParentLinkage = true
	if got := e.Coverage(); got.Status != agent.CollectionComplete || len(got.Diagnostics) != 0 {
		t.Fatal(got)
	}
}
