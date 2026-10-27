package turn

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/coverage"
	"github.com/cloudvane/litefactory/internal/store"
)

func TestValidateCoveredAssessmentPreservesDeliveryGates(t *testing.T) {
	for _, fault := range []string{"", "missing assessment", "invalid digest", "unmatched failure", "unrelated gate"} {
		t.Run(fault, func(t *testing.T) {
			s := vsBase()
			s.Suite.Outcome = "covered"
			s.Suite.AssessmentID = "immutable-assessment"
			s.Suite.AssessmentDigest = strings.Repeat("a", 64)
			switch fault {
			case "missing assessment":
				s.Suite.AssessmentID = ""
			case "invalid digest":
				s.Suite.AssessmentDigest = "corrupt"
			case "unmatched failure":
				s.Suite.Outcome = store.SuiteBlocked
			case "unrelated gate":
				s.Findings = []ValidateFinding{{ID: "s:code", State: findingOpen, Blocking: true}}
			}
			s.computeReadiness()
			if got := s.Ready; got != (fault == "") {
				t.Fatalf("ready=%v blockers=%v", got, s.Open)
			}
			end, _, _ := decideValidateEnd(s.Suite, nil)
			if (end == store.ValidateAssessed) != (fault == "" || fault == "unrelated gate") {
				t.Fatalf("end %s", end)
			}
		})
	}
}

func TestValidateRetainedAssessmentRejectsDetachedProof(t *testing.T) {
	f := newSuiteFixture(t)
	request, err := f.db.EnsureValidationRequest(t.Context(), store.ValidationRequest{Lineage: "repo:session", SourceKey: "comment:1", Policy: "auto", Commit: suiteCommit})
	if err != nil {
		t.Fatal(err)
	}
	observation, err := f.db.AppendValidationEvidence(t.Context(), store.ValidationEvidence{RequestID: request.ID, Kind: "observation", Version: 1, Complete: true, Payload: json.RawMessage(`{"status":"pass"}`)})
	if err != nil {
		t.Fatal(err)
	}
	m := coverage.Manifest{Version: coverage.Version, Commit: suiteCommit, Complete: true, Outcome: "covered"}
	payload, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := f.db.FinalizeValidationAssessment(t.Context(), store.ValidationEvidence{RequestID: request.ID, Version: 1, Complete: true, Payload: payload, References: []string{observation.ID}})
	if err != nil {
		t.Fatal(err)
	}
	output := validateSuiteOutput{Commit: suiteCommit, Outcome: "covered", AssessmentID: evidence.ID, AssessmentDigest: coverage.Digest(m)}
	for _, fault := range []string{"", "wrong commit", "wrong digest", "wrong outcome", "wrong run"} {
		t.Run(fault, func(t *testing.T) {
			out := output
			switch fault {
			case "wrong commit":
				out.Commit = strings.Repeat("f", 40)
			case "wrong digest":
				out.AssessmentDigest = strings.Repeat("0", 64)
			case "wrong outcome":
				out.Outcome = "passed"
			case "wrong run":
				out.RunID = 10
			}
			_, err := validateRetainedAssessment(t.Context(), Options{DB: f.db}, out)
			if (err == nil) != (fault == "") {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

func TestValidateOpaqueFullEvidenceRetainsExecutedChecks(t *testing.T) {
	f := newSuiteFixture(t)
	_, out, err := f.exec(912)
	if err != nil {
		t.Fatal(err)
	}
	request, err := f.db.EnsureValidationRequest(t.Context(), store.ValidationRequest{Lineage: "opaque", SourceKey: "request", Policy: "full", Commit: suiteCommit})
	if err != nil {
		t.Fatal(err)
	}
	m := coverage.Manifest{Version: coverage.Version, Commit: suiteCommit, Complete: false, Outcome: store.SuitePassed, FullReason: "unsupported runner retains unchanged full execution"}
	retained, err := f.db.FinalizeValidationAssessment(t.Context(), store.ValidationEvidence{RequestID: request.ID, RunID: out.RunID, Version: 1, Complete: false, Payload: mustMatrixJSON(t, m)})
	if err != nil {
		t.Fatal(err)
	}
	out.AssessmentID = retained.ID
	out.AssessmentDigest = coverage.Digest(m)
	evidence, err := (ValidateReviewActivity{}).evidence(t.Context(), Options{DB: f.db}, validateSuiteView{Output: out})
	if err != nil {
		t.Fatal(err)
	}
	if len(evidence) != 3 {
		t.Fatalf("factual full commands lost: %+v", evidence)
	}
	for _, e := range evidence {
		if e.ProofKind != coverage.Executed || e.State != store.SuitePassed || e.AssessmentVerified {
			t.Fatalf("opaque full invented inherited evidence: %+v", e)
		}
	}
}
