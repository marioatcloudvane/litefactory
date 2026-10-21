package prevention

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/standards"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
)

func sessionFixture(t *testing.T, published ...bool) *Session {
	t.Helper()
	ctx := t.Context()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "analysis.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if len(published) > 0 && published[0] {
		svc := &standards.Service{Store: db, OwnerID: 1}
		p, e := svc.Create(ctx, "fixture", 0, []standards.Operation{{Kind: "category.add", Category: &standards.Category{ID: "correctness", Name: "Correctness"}}, {Kind: "standard.add", Standard: &standards.Standard{ID: "existing", Title: "Check input", Body: strings.Repeat("Validate state. ", 100), CategoryIDs: []string{"correctness"}}}})
		if e != nil {
			t.Fatal(e)
		}
		if _, e = svc.Approve(ctx, p.ProposalID, p.Version, p.ChangeDigest); e != nil {
			t.Fatal(e)
		}
		if _, e = svc.Publish(ctx, p.ProposalID, p.Version, p.ChangeDigest); e != nil {
			t.Fatal(e)
		}
	}
	scope := Scope{RequestKey: "request", Repository: "owner/repo", Issues: []int{1}, Limits: DefaultLimits()}
	a, _, err := db.CreatePreventionAnalysis(ctx, store.PreventionAnalysis{ID: "analysis", AttemptID: "attempt", RequestKey: scope.RequestKey, Repository: scope.Repository, ScopeDigest: hash(scope), Scope: marshal(scope), Limits: marshal(scope.Limits)})
	if err != nil {
		t.Fatal(err)
	}
	manifest := Manifest{Evidence: []Evidence{{ID: "bug", Issue: 1, Kind: "issue", Excerpt: "Expected successful completion; observed failure."}, {ID: "cause", Issue: 1, Kind: "diagnosis", Excerpt: "Recorded cause is missing state validation."}}}
	if err = db.FreezePreventionAnalysis(ctx, a.ID, a.AttemptID, marshal(manifest)); err != nil {
		t.Fatal(err)
	}
	a, err = db.GetPreventionAnalysis(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	r := runtime(db, a.ID, a.AttemptID)
	if _, err = r.Bind(ctx, true); err != nil {
		t.Fatal(err)
	}
	return newSession(&Service{DB: db, Redactor: redact.New(redact.Options{Environ: []string{}})}, a, scope, manifest, r)
}

func TestSessionInvalidDraftAndDegradedGuidance(t *testing.T) {
	s := sessionFixture(t)
	ctx := t.Context()
	for _, id := range []string{"bug", "cause"} {
		if _, err := s.Evidence(ctx, id, 0); err != nil {
			t.Fatal(err)
		}
	}
	result := Result{SchemaVersion: 1, Findings: []Finding{{Issue: 1, Expected: "Completion", Actual: "Failure", Cause: "State validation missing", CauseBasis: "recorded", Uncertainty: "Diagnosis remains a recorded judgment", PreventionRationale: "Validate state before mutation", BugEvidence: []string{"bug"}, CauseEvidence: []string{"cause"}, Outcome: "proposed", Proposals: []standards.Operation{{Kind: "standard.add", Standard: &standards.Standard{ID: "new", Title: "Validate state", Body: "Check state before mutation", CategoryIDs: []string{"correctness"}}}}}}}
	ds, _, err := s.validate(ctx, result)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(ds, ";"), "degraded or partially delivered") {
		t.Fatalf("unavailable standards accepted: %v", ds)
	}
	if _, err = s.Submit(ctx, "invalid", result); err == nil {
		t.Fatal("invalid proposal accepted")
	}
	a, err := s.service.DB.GetPreventionAnalysis(ctx, s.analysis.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Draft) == 0 || len(a.Diagnostics) == 0 || len(a.Result) > 0 {
		t.Fatalf("invalid draft not inspectable: %+v", a)
	}
	ps, err := s.service.DB.ListStandardsProposals(ctx)
	if err != nil || len(ps) != 0 {
		t.Fatal("partial proposals", ps, err)
	}
	result.Findings[0].Outcome = "insufficient_evidence"
	result.Findings[0].Proposals = nil
	result.Findings[0].MissingEvidence = []string{"Verified standards unavailable"}
	receipt, err := s.Submit(ctx, "accepted", result)
	if err != nil {
		t.Fatal(err)
	}
	beforeRetry := s.used
	if _, err = s.Submit(ctx, "accepted", result); err != nil {
		t.Fatal(err)
	}
	if s.used <= beforeRetry {
		t.Fatal("finalized retry escaped cumulative output accounting")
	}
	got, err := s.Receipt(ctx, "accepted")
	if err != nil || got.ResultDigest != receipt.ResultDigest {
		t.Fatal(got, err)
	}
	if _, err = s.Receipt(ctx, "other"); !errors.Is(err, store.ErrNotFound) {
		t.Fatal(err)
	}
	result.Findings[0].Actual = "changed"
	if _, err = s.Submit(ctx, "accepted", result); !errors.Is(err, store.ErrPreventionConflict) {
		t.Fatal(err)
	}
}

func TestSessionOutputCountsDuplicateRepresentationsAndRetries(t *testing.T) {
	s := sessionFixture(t)
	ctx := t.Context()
	first, err := s.Evidence(ctx, "bug", 0)
	if err != nil {
		t.Fatal(err)
	}
	n := encodedSize(first)
	if n <= 2*len(marshal(first)) {
		t.Fatal("encoded duplicate and envelope not counted")
	}
	if _, err = s.Evidence(ctx, "bug", 0); err != nil {
		t.Fatal(err)
	}
	if s.used != 2*n {
		t.Fatalf("retry uncounted: %d != %d", s.used, 2*n)
	}
	for i := 0; i < 200; i++ {
		_, err = s.Evidence(ctx, "bug", 0)
		if err != nil {
			break
		}
	}
	if err == nil || !s.degraded || s.used > (128<<10)-(2<<10) {
		t.Fatal("shared allowance not enforced", s.used, err)
	}
	if err = s.account(ctx, map[string]string{"large": strings.Repeat("<", 5000)}); err == nil {
		t.Fatal("encoded page ceiling ignored")
	}
}

func TestSessionCancellationCloseAndCitationMembership(t *testing.T) {
	s := sessionFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := s.Evidence(ctx, "bug", 0); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := s.Evidence(t.Context(), "arbitrary", 0); err == nil {
		t.Fatal("outside manifest evidence read")
	}
	if _, err := s.Evidence(t.Context(), "bug", 1); err == nil {
		t.Fatal("skipped evidence continuation")
	}
	result := Result{SchemaVersion: 1, Findings: []Finding{{Issue: 1, Expected: "Expected", Actual: "Actual", Uncertainty: "Unknown", Outcome: "insufficient_evidence", MissingEvidence: []string{"cause missing"}, BugEvidence: []string{"bug"}}}}
	ds, _, err := s.validate(t.Context(), result)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(ds, ";"), "delivered") {
		t.Fatal("undelivered citation accepted", ds)
	}
	s.close()
	if _, err = s.Evidence(t.Context(), "bug", 0); err == nil {
		t.Fatal("closed broker delivered evidence")
	}
}

func TestSessionPartialGuidanceCannotValidateProposal(t *testing.T) {
	s := sessionFixture(t, true)
	ctx := t.Context()
	for _, id := range []string{"bug", "cause"} {
		if _, err := s.Evidence(ctx, id, 0); err != nil {
			t.Fatal(err)
		}
	}
	selected, err := s.Select(ctx, 0, []string{"correctness"}, "Relevant cause prevention")
	if err != nil {
		t.Fatal(err)
	}
	if selected.Selection == nil {
		t.Fatal("selection missing")
	}
	if _, err = s.ReadStandards(ctx, selected.Selection.Version, selected.Selection.Digest, "", false); err != nil {
		t.Fatal(err)
	}
	status, err := s.runtime.Status(ctx)
	if err != nil || status.Integrity != "verified" || status.RetrievalStatus != "partial" {
		t.Fatal(status, err)
	}
	result := Result{SchemaVersion: 1, Findings: []Finding{{Issue: 1, Expected: "Completion", Actual: "Failure", Cause: "Validation missing", CauseBasis: "recorded", Uncertainty: "Recorded judgment", PreventionRationale: "Validate state", BugEvidence: []string{"bug"}, CauseEvidence: []string{"cause"}, Outcome: "proposed", Proposals: []standards.Operation{{Kind: "standard.add", Standard: &standards.Standard{ID: "new", Title: "Check state", Body: "Validate before mutation", CategoryIDs: []string{"correctness"}}}}}}}
	ds, _, err := s.validate(ctx, result)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(ds, ";"), "degraded or partially delivered") {
		t.Fatal("partial guidance validated proposal", ds)
	}
}

func TestSessionBrokerExhaustionRevokesWithoutUnchargedOutput(t *testing.T) {
	s := sessionFixture(t)
	ctx := t.Context()
	revoked := false
	s.revoke = func() { revoked = true }
	s.used = 128 << 10
	out, err := s.handle(ctx, submission.Request{Method: "unknown", Payload: marshal(map[string]string{})})
	if !errors.Is(err, context.Canceled) || len(out) > 0 || !s.closed || !revoked {
		t.Fatal("exhausted broker emitted output", string(out), err, s.closed, revoked)
	}
}

func TestSessionRedactsDraftsAndSelectionBeforeRetention(t *testing.T) {
	ctx := t.Context()
	const secret = "fixture\"known-secret\nvalue"
	s := sessionFixture(t, true)
	s.service.Redactor = redact.New(redact.Options{Token: secrets.NewToken(secret), Environ: []string{}})
	if _, err := s.Select(ctx, 0, []string{"correctness"}, "Review "+secret); err != nil {
		t.Fatal(err)
	}
	selections, err := s.service.DB.ListStandardsRunSelections(ctx, s.runtime.RootRunID, s.runtime.ConsumerKey)
	if err != nil || len(selections) != 1 || strings.Contains(selections[0].Reason, secret) || !strings.Contains(selections[0].Reason, "[redacted]") {
		t.Fatal("selection reason retained sensitive text", err)
	}
	result := Result{SchemaVersion: 1, Findings: []Finding{{Issue: 1, Expected: "Completion " + secret, Actual: "Failure", Uncertainty: "Cause unknown", MissingEvidence: []string{"No cause"}, Outcome: "insufficient_evidence"}}}
	ds, err := s.Preflight(ctx, result)
	if err != nil || len(ds) != 1 || !strings.Contains(ds[0], "redacted content") {
		t.Fatal("sensitive result passed preflight", ds, err)
	}
	if _, err = s.Submit(ctx, "redacted-draft", result); err == nil {
		t.Fatal("sensitive result finalized")
	}
	saved, err := s.service.DB.GetPreventionAnalysis(ctx, s.analysis.ID)
	if err != nil {
		t.Fatal(err)
	}
	var draft Result
	if err = json.Unmarshal(saved.Draft, &draft); err != nil {
		t.Fatal(err)
	}
	if len(saved.Result) != 0 || len(saved.Proposals) != 0 || strings.Contains(draft.Findings[0].Expected, secret) || !strings.Contains(draft.Findings[0].Expected, "[redacted]") {
		t.Fatal("sensitive draft not withheld from finalization")
	}
	// A later corrected result retains the exact submitted bytes and identity.
	result.Findings[0].Expected = "Completion"
	receipt, err := s.Submit(ctx, "clean-result", result)
	if err != nil || receipt.ResultDigest == "" {
		t.Fatal("corrected safe result rejected", err)
	}
}

func TestSessionWithholdsUnredactableDraft(t *testing.T) {
	s := sessionFixture(t)
	result := Result{SchemaVersion: 1, Findings: []Finding{{Issue: 1, Expected: strings.Repeat("large", 16000), Actual: "Failure", Uncertainty: "Unknown", Outcome: "insufficient_evidence", MissingEvidence: []string{"No cause"}}}}
	if _, err := s.Submit(t.Context(), "oversized", result); err == nil {
		t.Fatal("oversized result accepted")
	}
	saved, err := s.service.DB.GetPreventionAnalysis(t.Context(), s.analysis.ID)
	if err != nil || string(saved.Draft) != `{"withheld":true}` || len(saved.Result) != 0 {
		t.Fatal("unredactable result was retained", err)
	}
}

func TestBrokerChargesAllEarlyRefusalsAndWithholdsCallerText(t *testing.T) {
	s := sessionFixture(t)
	requests := []submission.Request{
		{Method: "tools/call", Payload: json.RawMessage(`{"secret-field":"private-caller-value"}`)},
		{Method: "tools/call", Payload: json.RawMessage(`{"name":`)},
		{Method: "evidence_read", Payload: json.RawMessage(strings.Repeat("x", (64<<10)+1))},
	}
	for _, q := range requests {
		before := s.used
		out, err := s.handle(t.Context(), q)
		if err != nil || !strings.Contains(string(out), "refused") || strings.Contains(string(out), "private-caller-value") || s.used <= before {
			t.Fatal("early refusal escaped safe shared accounting", string(out), err, s.used, before)
		}
		saved, err := s.service.DB.GetPreventionAnalysis(t.Context(), s.analysis.ID)
		if err != nil || saved.OutputBytes != s.used {
			t.Fatal("early refusal reservation was not durable", err)
		}
	}
	// Decoding errors consume the same finite terminal reserve as other errors.
	s.used = 128 << 10
	out, err := s.handle(t.Context(), requests[0])
	if !errors.Is(err, context.Canceled) || len(out) != 0 || !s.closed {
		t.Fatal("early refusal escaped exhausted reserve", string(out), err)
	}
	saved, readErr := s.service.DB.GetPreventionAnalysis(t.Context(), s.analysis.ID)
	if readErr != nil || !saved.OutputLimited {
		t.Fatal("exhausted diagnostics allowance was hidden", readErr)
	}
}

func TestReceiptContainsOnlyExactProposalReferences(t *testing.T) {
	p := store.StandardsProposalVersion{ProposalID: "proposal", Version: 2, BaseRevision: 4, ChangeDigest: "change", ResultDigest: "result", Operations: []store.StandardsOperation{{Kind: "standard.add", Standard: &store.StandardsStandard{ID: "large", Body: strings.Repeat("unnecessary proposal content", 5000)}}}}
	got := receipt(store.PreventionAnalysis{ID: "analysis", ResultDigest: "accepted", Proposals: []store.StandardsProposalVersion{p}}, "request")
	raw := marshal(got)
	if encodedSize(got) > 8<<10 || strings.Contains(string(raw), "unnecessary proposal content") || len(got.Proposals) != 1 || got.Proposals[0] != proposalReference(p) {
		t.Fatal("receipt does not preserve bounded exact proposal references")
	}
}
