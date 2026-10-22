package drift

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
)

func TestInspectPreservesLiveAnalysisOwnership(t *testing.T) {
	s, req := serviceFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	entered := make(chan string, 1)
	finished := make(chan struct{})
	s.Runner = &controlledRunner{run: func(ctx context.Context, i *Invocation) error {
		entered <- i.ID()
		<-ctx.Done()
		return ctx.Err()
	}}
	// One test worker lives only until cancellation; cleanup joins it before
	// the fixture closes storage.
	go func() {
		defer close(finished)
		_, _ = s.Start(ctx, req)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-finished:
		case <-time.After(30 * time.Second):
			t.Error("timed out joining live analysis worker")
		}
	})
	var id string
	select {
	case id = <-entered:
	case <-time.After(30 * time.Second):
		t.Fatal("timed out waiting for live analyst entry")
	}
	view, err := s.Inspect(t.Context(), id)
	if err != nil || view.Analysis.Execution != "running" || view.Analysis.Generation != 1 {
		t.Fatalf("inspection interrupted active owner: %#v %v", view.Analysis, err)
	}
}

func ownershipAnalysis(t *testing.T, s *Service, req Request) store.DriftAnalysis {
	t.Helper()
	raw := bytesJSON(req)
	a, _, err := s.Store.CreateDriftAnalysis(t.Context(), store.DriftAnalysis{ID: "orphan", RunID: "orphan-run", Repository: req.Repository, RequestKey: req.Key, RequestDigest: digest(raw), Scope: raw, Limits: req.Limits})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeFor(s.Store, a).Bind(t.Context(), true); err != nil {
		t.Fatal(err)
	}
	return a
}

func TestInspectFencesOrphanWithoutRelaunchOrFreshAllowance(t *testing.T) {
	s, req := serviceFixture(t)
	runner := &controlledRunner{}
	s.Runner = runner
	a := ownershipAnalysis(t, s, req)
	ctx := t.Context()
	if err := s.Store.AppendDriftEvidence(ctx, a.ID, a.Generation, store.DriftEvidence{ID: "retained", Text: "retained evidence"}); err != nil {
		t.Fatal(err)
	}
	charged, err := s.Store.ChargeDriftBudget(ctx, a.ID, a.Generation, store.DriftUsage{Calls: 1, OutboundBytes: 16, Invocations: 1}, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	view, err := s.Inspect(ctx, a.ID)
	if err != nil || view.Analysis.Execution != "incomplete" || view.Analysis.Generation <= a.Generation || view.Analysis.Usage != charged.Usage {
		t.Fatalf("orphan recovery: %#v %v", view.Analysis, err)
	}
	if _, err := s.Store.ChargeDriftBudget(ctx, a.ID, a.Generation, store.DriftUsage{Calls: 1}, nil, false); !errors.Is(err, store.ErrDriftConflict) {
		t.Fatalf("abandoned writer retained authority: %v", err)
	}
	again, err := s.Start(ctx, req)
	if err != nil || again.ID != a.ID || again.Execution != "incomplete" || again.Usage != charged.Usage || runner.isolationCalls != 0 || runner.runCalls != 0 {
		t.Fatalf("orphan replay relaunched or refilled: %#v %v", again, err)
	}
	if _, err := s.Store.GetDriftEvidence(ctx, a.ID, "retained"); err != nil {
		t.Fatalf("orphan evidence lost: %v", err)
	}
}

func TestInspectPreservesCommittedReceiptBeforeInterruption(t *testing.T) {
	s, req := serviceFixture(t)
	a := ownershipAnalysis(t, s, req)
	report := bytesJSON(map[string]string{"explanation": "retained bounded result"})
	receipt, err := s.Store.FinalizeDriftAnalysis(t.Context(), a.ID, a.Generation, a.Version, "accepted-request", "accepted-digest", report, "completed", "insufficient_evidence", nil)
	if err != nil {
		t.Fatal(err)
	}
	view, err := s.Inspect(t.Context(), a.ID)
	if err != nil || view.Analysis.Execution != "completed" || string(view.Analysis.Report) != string(report) || view.Analysis.Reason != "" {
		t.Fatalf("committed result treated as orphan: %#v %v", view.Analysis, err)
	}
	retained, err := s.Store.GetDriftReceipt(t.Context(), a.ID, receipt.RequestID)
	if err != nil || retained.Digest != receipt.Digest || string(retained.Report) != string(receipt.Report) {
		t.Fatalf("committed receipt changed: %#v %v", retained, err)
	}
}
