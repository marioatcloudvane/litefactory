package doctor

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/store"
)

func TestDoctorRejectedDiagnosisRetainedWithoutAdoption(t *testing.T) {
	s := doctorIOService(t)
	ctx := t.Context()
	ref, err := Target("acme/api", 406)
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.DB.BeginDoctorAttempt(ctx, ref.String(), store.DoctorAttempt{ID: "review-owner", SourceID: "review-source", OwnerText: "Explain", PromptVersion: PromptVersion, GuideVersion: GuideVersion})
	if err != nil {
		t.Fatal(err)
	}
	evidence := store.DoctorEvidence{TaskRef: ref.String(), Records: []store.DoctorEvidenceRecord{{Kind: "run", ID: "42"}}, Gaps: []string{"Collector could not observe provider"}}
	result, err := BindResults(ctx, s.DB, "review-owner", evidence, s.Redactor)
	if err != nil {
		t.Fatal(err)
	}
	input := ResultInput{Diagnosis: Diagnosis{Findings: []Finding{{Explanation: "Useful finding doctor-secret-canary-001 <script>bad</script>", Evidence: []string{"run:42", "run:invented"}, Uncertainty: "May be stale"}}, Questions: []string{"Which process owns this?"}}}
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	raw = bytes.ReplaceAll(raw, []byte("doctor-secret-canary-001"), []byte(`\u0064octor-secret-canary-001`))
	receipt, err := result.Submit(ctx, "rejected-review", raw)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.InputState != "rejected" || receipt.AppliedRevision != 0 {
		t.Fatalf("rejected receipt = %+v", receipt)
	}
	replay, err := result.Submit(ctx, "rejected-review", raw)
	if err != nil || replay.ID != receipt.ID {
		t.Fatalf("replay = %+v %v", replay, err)
	}
	if _, err = result.Submit(ctx, "rejected-review", append(append([]byte(nil), raw...), ' ')); !errors.Is(err, store.ErrResultCorrelation) {
		t.Fatalf("changed request = %v", err)
	}
	if bytes.Contains(receipt.Payload, []byte("doctor-secret-canary-001")) {
		t.Fatal("raw secret retained")
	}
	if err = result.Adopt(ctx, "rejected-review"); !errors.Is(err, store.ErrResultAuthority) {
		t.Fatalf("review adopted: %v", err)
	}
	accepted, err := s.DB.DoctorCurrentResult(ctx, c.ID)
	if err != nil || len(accepted) > 0 {
		t.Fatalf("accepted result changed: %s %v", accepted, err)
	}
	proposal, err := s.DB.DoctorCurrentProposal(ctx, c.ID)
	if err != nil || len(proposal) > 0 {
		t.Fatalf("proposal changed: %s %v", proposal, err)
	}
	review, err := s.currentReview(ctx, c.ID)
	if err != nil || review == nil {
		t.Fatalf("read review: %+v %v", review, err)
	}
	body := RenderRejectedDiagnosisReview(*review)
	for _, want := range []string{"Unaccepted Doctor diagnosis", "Useful finding", "May be stale", "Which process owns this?", "Evidence unverified:", "run:42", "run:invented", "Factory collector context", "Collector could not observe provider", `\u003cscript\u003e`} {
		if !strings.Contains(body, want) {
			t.Errorf("review missing %q: %s", want, body)
		}
	}
	if strings.Contains(body, "doctor-secret-canary-001") || strings.Contains(body, `\u0064octor-secret-canary-001`) || strings.Contains(body, "<script>") {
		t.Fatal("unsafe review display")
	}
}

func TestDoctorRejectedReviewBoundsAndAmbiguity(t *testing.T) {
	s := doctorIOService(t)
	ctx := t.Context()
	ref, err := Target("acme/api", 406)
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.DB.BeginDoctorAttempt(ctx, ref.String(), store.DoctorAttempt{ID: "bounded-review", SourceID: "bounded-review-source", OwnerText: "Explain", PromptVersion: PromptVersion, GuideVersion: GuideVersion})
	if err != nil {
		t.Fatal(err)
	}
	result, err := BindResults(ctx, s.DB, "bounded-review", store.DoctorEvidence{TaskRef: ref.String()}, s.Redactor)
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{"diagnosis":{"findings":[{"explanation":"<script>first</script>","explanation":"second","evidence":["run:invented"]}]}}`)
	receipt, err := result.Submit(ctx, "ambiguous", raw)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.InputState != "rejected" {
		t.Fatal("ambiguous source accepted")
	}
	review, err := s.currentReview(ctx, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	body := RenderRejectedDiagnosisReview(*review)
	if strings.Contains(body, "<script>") || !strings.Contains(body, "&lt;script&gt;first&lt;/script&gt;") || !strings.Contains(body, "second") || !strings.Contains(body, "Evidence references in submitted content are unverified") {
		t.Fatalf("ambiguous source not fully escaped: %s", body)
	}
	if _, err = result.Submit(ctx, "oversize", []byte(strings.Repeat("x", 65537))); err == nil {
		t.Fatal("oversize retained")
	}
	if _, err = result.Receipt(ctx, "oversize"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("oversize receipt: %v", err)
	}
	if _, err = result.Submit(ctx, "unsafe-quote", []byte(`{"diagnosis":"unterminated\u0064`)); err == nil {
		t.Fatal("unsafe quoted value retained")
	}
	if _, err = result.Receipt(ctx, "unsafe-quote"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unsafe quote receipt: %v", err)
	}
	after, err := s.currentReview(ctx, c.ID)
	if err != nil || after.RequestID != review.RequestID {
		t.Fatalf("oversize replaced last good review: %+v %v", after, err)
	}
}

func TestDoctorRejectedSourceRecoveryDoesNotAdopt(t *testing.T) {
	s := doctorIOService(t)
	ctx := t.Context()
	ref, err := Target("acme/api", 406)
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.DB.BeginDoctorAttempt(ctx, ref.String(), store.DoctorAttempt{ID: "recover-review", SourceID: "source-rejected", OwnerText: "Explain", PromptVersion: PromptVersion, GuideVersion: GuideVersion})
	if err != nil {
		t.Fatal(err)
	}
	results, err := BindResults(ctx, s.DB, "recover-review", store.DoctorEvidence{TaskRef: ref.String()}, s.Redactor)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = results.Submit(ctx, "recover-review-request", []byte(`{"unknown":"useful retained source"}`)); err != nil {
		t.Fatal(err)
	}
	recovered, err := s.RecoverSource(ctx, ref, "source-rejected")
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Review == nil || recovered.Review.RequestID != "recover-review-request" || recovered.Diagnosis != nil || recovered.ProposalID != 0 || recovered.ApplyAvailable {
		t.Fatalf("recovery adopted review: %+v", recovered)
	}
	accepted, err := s.DB.DoctorCurrentResult(ctx, c.ID)
	if err != nil || len(accepted) != 0 {
		t.Fatalf("review became accepted: %s %v", accepted, err)
	}
	if err = s.Stop(ctx, ref); err != nil {
		t.Fatal(err)
	}
	if replay, e := results.Submit(ctx, "recover-review-request", []byte(`{"unknown":"useful retained source"}`)); e != nil || replay.RequestID != "recover-review-request" {
		t.Fatalf("fenced exact replay: %+v %v", replay, e)
	}
	if _, err = results.Receipt(ctx, "recover-review-request"); err != nil {
		t.Fatalf("recovery lost receipt: %v", err)
	}
	if _, err = s.RecoverSource(ctx, ref, "other-source"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("foreign source: %v", err)
	}
}

func TestDoctorStaleRevisionRetainedWithoutAcceptedRevision(t *testing.T) {
	s := doctorIOService(t)
	ctx := t.Context()
	ref, err := Target("acme/api", 406)
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.DB.BeginDoctorAttempt(ctx, ref.String(), store.DoctorAttempt{ID: "stale-review", SourceID: "stale-source", OwnerText: "Explain", PromptVersion: PromptVersion, GuideVersion: GuideVersion})
	if err != nil {
		t.Fatal(err)
	}
	results, err := BindResults(ctx, s.DB, "stale-review", store.DoctorEvidence{TaskRef: ref.String(), Records: []store.DoctorEvidenceRecord{{Kind: "run", ID: "42"}}}, s.Redactor)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(ResultInput{ExpectedRevision: 9, Diagnosis: Diagnosis{Findings: []Finding{{Explanation: "Useful stale result", Evidence: []string{"run:42"}, Uncertainty: "None material."}}}})
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := results.Submit(ctx, "stale-review-request", raw)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.InputState != "rejected" || !bytes.Contains(receipt.Diagnostics, []byte("stale expected_revision")) {
		t.Fatalf("stale input not rejected for review: %+v", receipt)
	}
	accepted, err := s.DB.DoctorCurrentResult(ctx, c.ID)
	if err != nil || len(accepted) > 0 {
		t.Fatalf("stale accepted: %s %v", accepted, err)
	}
}
