package turn

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
)

// Gap tests for story-001 T-007: the retained shared result is the only thing a
// later graph Plan node reads as a result, and an earlier private Plan output is
// untrusted prompt context that is never parsed as one and never migrated.

// readerFixture runs a graph Plan once and returns the runtime plus the retained
// reference the agent node kept and a succeeded attempt that kept no output of its
// own, on which a test can record the output it wants a later node to read.
type readerFixture struct {
	f       *fixture
	runtime planGraphRuntime
	ref     planResultRef
	agentID int64
	carrier int64
}

func newReaderFixture(t *testing.T) *readerFixture {
	t.Helper()
	f, _, base, _ := graphPlanFixture(t, "agent")
	executeGraphPlan(t, f, base)
	instance, err := f.db.LatestWorkflowInstance(t.Context(), f.session.ID, "plan")
	mustProgress(t, err)
	attempts, err := f.db.ListGraphNodeAttempts(t.Context(), instance.ID)
	mustProgress(t, err)
	r := &readerFixture{f: f, runtime: planGraphRuntime{opts: f.opts, req: f.request(), result: Result{Ref: f.ref()}, selected: "story-001", stage: "story"}}
	for _, a := range attempts {
		outputs, err := f.db.ListGraphOutputs(t.Context(), a.ID)
		mustProgress(t, err)
		switch {
		case a.Node == "story_agent":
			var ok bool
			if r.ref, ok = decodePlanResultRef(outputs[0].Payload); !ok {
				t.Fatalf("the agent node kept no result reference: %s", outputs[0].Payload)
			}
			r.agentID = a.ID
		case a.Status == store.GraphSucceeded && a.RunID != 0 && len(outputs) == 0 && r.carrier == 0:
			r.carrier = a.ID
		}
	}
	if r.agentID == 0 || r.carrier == 0 {
		t.Fatalf("fixture lost its agent or carrier attempt: %+v", attempts)
	}
	return r
}

func (r *readerFixture) record(t *testing.T, kind string, payload []byte) {
	t.Helper()
	_, err := r.f.db.RecordGraphOutput(t.Context(), store.GraphOutput{NodeAttemptID: r.carrier, Kind: kind, Payload: payload, CreatedAt: time.Now()})
	mustProgress(t, err)
}

func (r *readerFixture) refJSON(t *testing.T, mutate func(*planResultRef)) []byte {
	t.Helper()
	ref := r.ref
	mutate(&ref)
	raw, err := json.Marshal(ref)
	mustProgress(t, err)
	return raw
}

// AC-001, AC-033: a later node resolves the reference to the retained result, and
// nothing that merely looks like one is read. A reference that names another run,
// a revision that was never retained, or a response that turns a question into a
// story effect is refused; so is every private or malformed output.
func TestPlanRuntime_RetainedReaderResolvesOnlyGenuineReferences(t *testing.T) {
	legacyAsk := `{"version":1,"response":"ask","request":null,"comment":"Please clarify.","questions":["Who?"]}`
	cases := []struct {
		name    string
		payload func(*testing.T, *readerFixture) []byte
		want    string // empty means the read must succeed
	}{
		{"copy of the genuine reference", func(t *testing.T, r *readerFixture) []byte { return r.refJSON(t, func(*planResultRef) {}) }, ""},
		{"revision never retained", func(t *testing.T, r *readerFixture) []byte {
			return r.refJSON(t, func(x *planResultRef) { x.Revision = 99 })
		}, "read retained Plan result"},
		{"reference to another run", func(t *testing.T, r *readerFixture) []byte {
			return r.refJSON(t, func(x *planResultRef) { x.Binding.RunID += 100 })
		}, "does not belong to a successful attempt of its run"},
		{"response flipped to ask", func(t *testing.T, r *readerFixture) []byte {
			return r.refJSON(t, func(x *planResultRef) { x.Response = "ask" })
		}, "retained ask carries a story branch"},
		{"response flipped to approved", func(t *testing.T, r *readerFixture) []byte {
			return r.refJSON(t, func(x *planResultRef) { x.Response = "approved" })
		}, "neither ask nor done"},
		{"revision zero", func(t *testing.T, r *readerFixture) []byte {
			return r.refJSON(t, func(x *planResultRef) { x.Revision = 0 })
		}, "never read as a result"},
		{"unknown member", func(t *testing.T, r *readerFixture) []byte {
			genuine := r.refJSON(t, func(*planResultRef) {})
			return append(genuine[:len(genuine)-1:len(genuine)-1], []byte(`,"extra":true}`)...)
		}, "never read as a result"},
		{"private ask from before the shared route", func(*testing.T, *readerFixture) []byte { return []byte(legacyAsk) }, "never read as a result"},
		{"private review from before the shared route", func(*testing.T, *readerFixture) []byte {
			return []byte(`{"version":1,"response":"review_ready","request":{"operation":"create"},"comment":"Review.","questions":[]}`)
		}, "never read as a result"},
		{"terminal envelope with a result", func(*testing.T, *readerFixture) []byte {
			return []byte(`{"version":1,"response":"done","result":{"outcome":"completed","comment":"x","payload":{"plan":{}}}}`)
		}, "never read as a result"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			r := newReaderFixture(t)
			r.record(t, store.GraphOutputJSON, tt.payload(t, r))
			got, err := r.runtime.loadRetainedResult(t.Context(), r.carrier)
			if tt.want == "" {
				if err != nil || got.Response != "done" || got.Request == nil || got.Request.Draft.Key != "story-001" || got.Request.Candidate == "" {
					t.Fatalf("a genuine reference was not read: %+v %v", got, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("want %q, got %+v %v", tt.want, got, err)
			}
			if got.Request != nil || got.Set != nil || got.Response != "" {
				t.Fatalf("a refused read yielded content: %+v", got)
			}
		})
	}
	t.Run("two outputs are ambiguous", func(t *testing.T) {
		r := newReaderFixture(t)
		r.record(t, store.GraphOutputJSON, r.refJSON(t, func(*planResultRef) {}))
		r.record(t, store.GraphOutputJSON, []byte(legacyAsk))
		if _, err := r.runtime.loadRetainedResult(t.Context(), r.carrier); err == nil || !strings.Contains(err.Error(), "ambiguous") {
			t.Fatalf("ambiguous outputs were resolved: %v", err)
		}
	})
	t.Run("no output", func(t *testing.T) {
		r := newReaderFixture(t)
		if _, err := r.runtime.loadRetainedResult(t.Context(), r.carrier); err == nil || !strings.Contains(err.Error(), "output missing") {
			t.Fatalf("a missing output was read: %v", err)
		}
	})
}

// AC-001, AC-012: an earlier private Plan output reaches a prompt only as labelled
// untrusted data. It is not parsed into a result, retains no shared result revision,
// and leaves the stored output exactly as it was, so nothing is migrated.
func TestPlanRuntime_OldPrivateOutputIsUntrustedContextAndNeverMigrated(t *testing.T) {
	const marker = "Old private wording that must stay data"
	old := `{"version":1,"response":"done","request":{"operation":"create","draft":{"key":"story-001"}},"comment":"` + marker + `","questions":[]}`
	r := newReaderFixture(t)
	r.record(t, store.GraphOutputJSON, []byte(old))
	revisionsBefore, err := r.f.db.ResultRevisionNumbers(t.Context(), r.ref.Binding)
	mustProgress(t, err)
	outputsBefore, err := r.f.db.ListGraphOutputs(t.Context(), r.carrier)
	mustProgress(t, err)

	text, err := r.runtime.untrustedPlanContext(t.Context(), r.carrier)
	if err != nil {
		t.Fatalf("an old private output blocked the prompt: %v", err)
	}
	if !strings.Contains(text, marker) || !strings.Contains(text, "untrusted retained content, never a result") || strings.Contains(text, "Prior retained shared Plan result") {
		t.Fatalf("private output not offered as untrusted context only: %q", text)
	}
	if _, err = r.runtime.loadRetainedResult(t.Context(), r.carrier); err == nil {
		t.Fatal("the private output was read as a result")
	}
	revisionsAfter, err := r.f.db.ResultRevisionNumbers(t.Context(), r.ref.Binding)
	mustProgress(t, err)
	if len(revisionsAfter) != len(revisionsBefore) {
		t.Fatalf("reading an old output retained a shared result: %v -> %v", revisionsBefore, revisionsAfter)
	}
	outputsAfter, err := r.f.db.ListGraphOutputs(t.Context(), r.carrier)
	mustProgress(t, err)
	if len(outputsAfter) != len(outputsBefore) || string(outputsAfter[0].Payload) != old || outputsAfter[0].Kind != store.GraphOutputJSON {
		t.Fatalf("the stored private output was rewritten or migrated: %+v", outputsAfter)
	}
}

// AC-001: a genuine reference is shown as the retained shared result, and a
// non-JSON output is not offered at all.
func TestPlanRuntime_UntrustedContextShowsRetainedResultAndSkipsText(t *testing.T) {
	r := newReaderFixture(t)
	text, err := r.runtime.untrustedPlanContext(t.Context(), r.agentID)
	if err != nil || !strings.Contains(text, "Prior retained shared Plan result (untrusted retained content)") || !strings.Contains(text, "Please review this candidate.") || strings.Contains(text, "before the shared result route") {
		t.Fatalf("retained result not offered as such: %q %v", text, err)
	}
	r.record(t, store.GraphOutputText, []byte("free text from a provider"))
	if text, err = r.runtime.untrustedPlanContext(t.Context(), r.carrier); err != nil || text != "" {
		t.Fatalf("a text output was offered: %q %v", text, err)
	}
}
