package drift

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/cloudvane/litefactory/internal/standards"
	"github.com/cloudvane/litefactory/internal/store"
)

func publishInvocationBook(t *testing.T, s *Service) {
	t.Helper()
	book := &standards.Service{Store: s.Store, OwnerID: 17}
	proposal, err := book.Create(t.Context(), "invocation-book", 0, []standards.Operation{{Kind: "category.add", Category: &standards.Category{ID: "architecture", Name: "Architecture"}}, {Kind: "standard.add", Standard: &standards.Standard{ID: "bounds", Title: "Bound work", Body: strings.Repeat("Bound work. ", 500), CategoryIDs: []string{"architecture"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = book.Approve(t.Context(), proposal.ProposalID, proposal.Version, proposal.ChangeDigest); err != nil {
		t.Fatal(err)
	}
	if _, err = book.Publish(t.Context(), proposal.ProposalID, proposal.Version, proposal.ChangeDigest); err != nil {
		t.Fatal(err)
	}
}

func invocationCall(ctx context.Context, i *Invocation, name string, input any, dst any) (json.RawMessage, error) {
	raw, err := i.Call(ctx, name, bytesJSON(input))
	if err == nil && dst != nil {
		err = json.Unmarshal(raw, dst)
	}
	return raw, err
}

func TestInvocationConcurrentEncodedAccountingPreservesDeliveryReserve(t *testing.T) {
	s, req := serviceFixture(t)
	req.Limits.Calls = 8
	s.Runner = &controlledRunner{run: func(ctx context.Context, i *Invocation) error {
		var wg sync.WaitGroup
		type reply struct {
			raw json.RawMessage
			err error
		}
		replies := make(chan reply, 12)
		for range 12 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				raw, err := i.Call(ctx, "drift_evidence_list", json.RawMessage(`{}`))
				replies <- reply{raw, err}
			}()
		}
		wg.Wait()
		close(replies)
		successes := 0
		var charged int64
		for r := range replies {
			if r.err == nil {
				successes++
				quoted, _ := json.Marshal(string(r.raw))
				charged += int64(len(r.raw) + len(quoted) + 1024)
			} else if !errors.Is(r.err, store.ErrDriftBudget) {
				return r.err
			}
		}
		a, err := s.Store.GetDriftAnalysis(ctx, i.ID())
		if err != nil {
			return err
		}
		if successes != 4 || a.Usage.Calls != 4 || a.Usage.OutboundBytes != charged {
			return fmt.Errorf("concurrent accounting: successes=%d usage=%+v encoded=%d", successes, a.Usage, charged)
		}
		input := ResultInput{RequestID: "reserve", Report: Report{Disposition: "insufficient_evidence", Explanation: "Data allowance was exhausted.", Uncertainty: "Uninspected material remains."}}
		_, err = invocationCall(ctx, i, "communication_preflight", input, nil)
		return err
	}}
	a, err := s.Start(t.Context(), req)
	if !errors.Is(err, store.ErrDriftBudget) || a.Execution != "incomplete" {
		t.Fatalf("exhaustion state: %s %v", a.Execution, err)
	}
	if a.Usage.Calls != 5 {
		t.Fatalf("diagnostic reserve unavailable: %+v", a.Usage)
	}
}

func TestInvocationDegradedStandardsWithholdConclusions(t *testing.T) {
	s, req := serviceFixture(t)
	s.Runner = &controlledRunner{run: func(ctx context.Context, i *Invocation) error {
		for _, disposition := range []string{"no_warranted_change", "proposals"} {
			report := Report{Disposition: disposition, Explanation: "A bounded judgment.", Uncertainty: "Standards are unavailable."}
			if disposition == "proposals" {
				report.Candidates = []Candidate{{Key: "candidate"}}
			}
			_, _, _, err := i.validate(ctx, ResultInput{RequestID: disposition, Report: report})
			if err == nil || !strings.Contains(err.Error(), "fully retrieved selected standards") {
				return fmt.Errorf("degraded %s accepted: %w", disposition, err)
			}
		}
		_, _, _, err := i.validate(ctx, ResultInput{RequestID: "weak", Report: Report{Disposition: "insufficient_evidence", Explanation: "No verified standards context.", Uncertainty: "Cannot establish a conclusion."}})
		return err
	}}
	if _, err := s.Start(t.Context(), req); err != nil {
		t.Fatal(err)
	}
}

func TestInvocationUnfundedStandardsPageDoesNotBecomeOffered(t *testing.T) {
	s, req := serviceFixture(t)
	publishInvocationBook(t, s)
	s.Runner = &controlledRunner{run: func(ctx context.Context, i *Invocation) error {
		var selected standards.RuntimeStatus
		if _, err := invocationCall(ctx, i, "standards_select", map[string]any{"category_ids": []string{"architecture"}, "expected_version": 0, "reason": "Inspect bounds."}, &selected); err != nil {
			return err
		}
		a, err := s.Store.GetDriftAnalysis(ctx, i.ID())
		if err != nil {
			return err
		}
		_, err = s.Store.ChargeDriftBudget(ctx, a.ID, a.Generation, store.DriftUsage{OutboundBytes: a.Limits.OutboundBytes - 8192 - a.Usage.OutboundBytes - 512}, nil, false)
		if err != nil {
			return err
		}
		_, err = invocationCall(ctx, i, "standards_read", map[string]any{"selection_version": selected.Selection.Version, "selection_digest": selected.Selection.Digest}, nil)
		if !errors.Is(err, store.ErrDriftBudget) {
			return fmt.Errorf("unfunded page returned: %w", err)
		}
		observations, err := s.Store.ListStandardsRunObservations(ctx, a.RunID, "drift-analyst")
		if err != nil {
			return err
		}
		for _, observation := range observations {
			if observation.Kind == "offered" {
				return errors.New("unfunded standards page persisted an offered range")
			}
		}
		return nil
	}}
	if a, err := s.Start(t.Context(), req); !errors.Is(err, store.ErrDriftBudget) || a.Execution != "incomplete" {
		t.Fatalf("unfunded page outcome: %s %v", a.Execution, err)
	}
}

func TestInvocationEmptySelectionAndBoundedReceiptReplay(t *testing.T) {
	s, req := serviceFixture(t)
	publishInvocationBook(t, s)
	s.Runner = &controlledRunner{run: func(ctx context.Context, i *Invocation) error {
		var selected standards.RuntimeStatus
		if _, err := invocationCall(ctx, i, "standards_select", map[string]any{"category_ids": []string{}, "expected_version": 0, "reason": "New guidance is outside selected existing standards."}, &selected); err != nil {
			return err
		}
		if _, err := invocationCall(ctx, i, "standards_read", map[string]any{"selection_version": selected.Selection.Version, "selection_digest": selected.Selection.Digest, "acknowledge": true}, nil); err != nil {
			return err
		}
		evidence, err := s.Store.ListDriftEvidence(ctx, i.ID(), 0, 100)
		if err != nil || len(evidence) == 0 {
			return fmt.Errorf("evidence: %w", err)
		}
		item := evidence[0]
		if _, err = invocationCall(ctx, i, "drift_evidence_read", map[string]any{"evidence_id": item.ID}, nil); err != nil {
			return err
		}
		input := ResultInput{RequestID: "receipt", Report: Report{Disposition: "proposals", Explanation: "The bounded reader warrants new guidance.", Uncertainty: "Human review is required.", SelectionDigest: selected.Selection.Digest, Findings: []Finding{{ObservedDrift: "A bounded source reader was added.", Reason: "Callers need explicit limits.", Benefit: "Readers remain finite.", Uncertainty: "Workload ceilings vary.", Citations: []Citation{{EvidenceID: item.ID, Digest: item.Digest, Start: 0, End: len(item.Text)}}}}, Candidates: []Candidate{{Key: "new-guidance", Finding: 0, Operation: standards.Operation{Kind: "standard.add", Standard: &standards.Standard{ID: "reader-limits", Title: "Limit readers", Body: "Declare reader work ceilings.", CategoryIDs: []string{"architecture"}}}}}}}
		accepted, err := invocationCall(ctx, i, "communication_submit", input, nil)
		if err != nil {
			return err
		}
		replay, err := invocationCall(ctx, i, "communication_submit", input, nil)
		if err != nil || !bytes.Equal(accepted, replay) {
			return fmt.Errorf("identical replay changed receipt: %s %s %w", accepted, replay, err)
		}
		receipt, err := invocationCall(ctx, i, "communication_receipt", map[string]string{"request_id": "receipt"}, nil)
		if err != nil || !bytes.Equal(accepted, receipt) {
			return fmt.Errorf("receipt recovery changed references: %w", err)
		}
		input.Report.Explanation = "Changed bytes under the same identity."
		if _, err = invocationCall(ctx, i, "communication_submit", input, nil); !errors.Is(err, store.ErrDriftConflict) {
			return fmt.Errorf("changed replay accepted: %w", err)
		}
		if _, err = invocationCall(ctx, i, "standards_select", map[string]any{"expected_version": selected.Selection.Version, "category_ids": []string{"architecture"}, "reason": "must not mutate completed context"}, nil); !errors.Is(err, store.ErrDriftConflict) {
			return fmt.Errorf("terminal context mutation accepted: %w", err)
		}
		exhausted := false
		for range req.Limits.Calls + 1 {
			_, err = invocationCall(ctx, i, "communication_receipt", map[string]string{"request_id": "receipt"}, nil)
			if errors.Is(err, store.ErrDriftBudget) {
				exhausted = true
				break
			}
			if err != nil {
				return err
			}
		}
		if !exhausted {
			return errors.New("receipt recovery received unbounded allowance")
		}
		return nil
	}}
	a, err := s.Start(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	if a.Execution != "completed" || len(a.ProposalLinks) != 1 || a.Usage.Invocations != 1 {
		t.Fatalf("empty selection/recovery outcome: %+v", a)
	}
}

func TestInvocationCitationCandidateAndUnofferedCoverageValidation(t *testing.T) {
	s, req := serviceFixture(t)
	publishInvocationBook(t, s)
	s.Runner = &controlledRunner{run: func(ctx context.Context, i *Invocation) error {
		var selected standards.RuntimeStatus
		if _, err := invocationCall(ctx, i, "standards_select", map[string]any{"category_ids": []string{}, "expected_version": 0, "reason": "Assess possible new guidance."}, &selected); err != nil {
			return err
		}
		if _, err := invocationCall(ctx, i, "standards_read", map[string]any{"selection_version": selected.Selection.Version, "selection_digest": selected.Selection.Digest, "acknowledge": true}, nil); err != nil {
			return err
		}
		evidence, err := s.Store.ListDriftEvidence(ctx, i.ID(), 0, 100)
		if err != nil || len(evidence) == 0 {
			return fmt.Errorf("retained evidence: %w", err)
		}
		item := evidence[0]
		base := func() ResultInput {
			return ResultInput{RequestID: "validation", Report: Report{Disposition: "insufficient_evidence", Explanation: "Inspect retained coverage.", Uncertainty: "Human judgment remains necessary.", SelectionDigest: selected.Selection.Digest}}
		}
		report, _, execution, err := i.validate(ctx, base())
		if err != nil || execution != "incomplete" {
			return fmt.Errorf("retained unoffered evidence counted as complete: %s %w", execution, err)
		}
		var coverage Coverage
		if err = json.Unmarshal(report.Coverage, &coverage); err != nil {
			return err
		}
		if !strings.Contains(strings.Join(coverage.Gaps, " "), "unoffered ranges") {
			return errors.New("host report hides retained unoffered body")
		}
		noChange := base()
		noChange.Report.Disposition = "no_warranted_change"
		if _, _, _, err = i.validate(ctx, noChange); err == nil || !strings.Contains(err.Error(), "completed bounded pass") {
			return fmt.Errorf("no-change accepted missing coverage: %w", err)
		}
		cases := []struct {
			name, want string
			offer      bool
			mutate     func(*ResultInput)
		}{
			{name: "missing evidence", want: "citation evidence unavailable", mutate: func(p *ResultInput) { p.Report.Findings[0].Citations[0].EvidenceID = "absent" }},
			{name: "unoffered evidence", want: "not offered", mutate: func(*ResultInput) {}},
			{name: "invalid range", want: "invalid citation range", offer: true, mutate: func(p *ResultInput) { p.Report.Findings[0].Citations[0].End = len(item.Text) + 1 }},
			{name: "malformed candidate", want: "invalid candidate", offer: true, mutate: func(p *ResultInput) { p.Report.Candidates[0].Operation.Kind = "category.add" }},
		}
		for _, tc := range cases {
			if tc.offer {
				if _, err = invocationCall(ctx, i, "drift_evidence_read", map[string]any{"evidence_id": item.ID}, nil); err != nil {
					return err
				}
			}
			input := base()
			input.Report.Disposition = "proposals"
			input.Report.Findings = []Finding{{ObservedDrift: "Reader boundaries changed.", Reason: "Explicit limits aid review.", Benefit: "Finite work is inspectable.", Uncertainty: "Human review is required.", Citations: []Citation{{EvidenceID: item.ID, Digest: item.Digest, Start: 0, End: len(item.Text)}}}}
			input.Report.Candidates = []Candidate{{Key: "guidance", Finding: 0, Operation: standards.Operation{Kind: "standard.add", Standard: &standards.Standard{ID: "new-reader-guidance", Title: "Bound readers", Body: "Declare source reader limits.", CategoryIDs: []string{"architecture"}}}}}
			tc.mutate(&input)
			if _, candidates, _, validationErr := i.validate(ctx, input); validationErr == nil || !strings.Contains(validationErr.Error(), tc.want) || len(candidates) != 0 {
				return fmt.Errorf("%s produced candidates or wrong error: %w", tc.name, validationErr)
			}
			proposals, listErr := s.Store.ListStandardsProposals(ctx)
			if listErr != nil {
				return listErr
			}
			if len(proposals) != 1 {
				return fmt.Errorf("%s created a proposal during validation", tc.name)
			}
		}
		return nil
	}}
	a, err := s.Start(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Report) != 0 || len(a.ProposalLinks) != 0 {
		t.Fatalf("rejected validation retained a result: %+v", a)
	}
}

func TestInvocationRejectedCallsRemainBoundedAndCharged(t *testing.T) {
	s, req := serviceFixture(t)
	s.Runner = &controlledRunner{run: func(ctx context.Context, i *Invocation) error {
		for _, input := range []struct {
			name    string
			payload json.RawMessage
		}{
			{"unknown_tool", json.RawMessage(`{}`)},
			{"drift_evidence_list", bytesJSON(map[string]string{"unexpected": strings.Repeat("x", 6000)})},
			{"communication_preflight", json.RawMessage(strings.Repeat("x", req.Limits.ResultBytes+1))},
		} {
			before, err := s.Store.GetDriftAnalysis(ctx, i.ID())
			if err != nil {
				return err
			}
			_, err = i.Call(ctx, input.name, input.payload)
			if err == nil || len(err.Error()) > 600 {
				return fmt.Errorf("unbounded or successful refusal: %w", err)
			}
			after, err := s.Store.GetDriftAnalysis(ctx, i.ID())
			if err != nil {
				return err
			}
			bytes := after.Usage.OutboundBytes - before.Usage.OutboundBytes
			if after.Usage.Calls-before.Usage.Calls != 1 || bytes < 1024 || bytes > 2500 {
				return fmt.Errorf("unfunded rejection: before=%+v after=%+v", before.Usage, after.Usage)
			}
		}
		return nil
	}}
	if _, err := s.Start(t.Context(), req); err != nil {
		t.Fatal(err)
	}
}
