package doctor

import (
	"encoding/json"
	"testing"

	"github.com/cloudvane/litefactory/internal/store"
)

// These fixtures model accepted history from before typed proposal admission.
// They deliberately bypass current submission validation, as an old database
// contains the durable accepted projection already.
func retainLegacyRepairDiagnosis(t *testing.T, results *ResultService, id string, input ResultInput) {
	t.Helper()
	snapshot, err := json.Marshal(input.Diagnosis)
	if err != nil {
		t.Fatal(err)
	}
	proposal, err := json.Marshal(input.Diagnosis.Proposal)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = results.db.RecordResult(t.Context(), store.ResultRecord{Binding: results.binding, RequestID: id, Payload: payload, Snapshot: snapshot, Diagnostics: []byte(`{"valid":true,"errors":[]}`), Retired: []byte(`[]`), ExpectedRevision: input.ExpectedRevision, Accepted: true}); err != nil {
		t.Fatal(err)
	}
	if err = results.db.AdoptDoctorResult(t.Context(), results.binding, id, proposal); err != nil {
		t.Fatal(err)
	}
}

func TestTypedProposalAdmissionRejectsLegacy(t *testing.T) {
	t.Parallel()
	e := store.DoctorEvidence{Records: []store.DoctorEvidenceRecord{{Kind: "run", ID: "42", State: "running"}}}
	d := Diagnosis{Findings: []Finding{{Explanation: "Run stranded", Evidence: []string{"run:42"}, Uncertainty: "Process observation unavailable"}}, Proposal: []RepairStep{{Number: 1, Target: "run:42", Precondition: "running", Change: "stop", Reason: "approved stop", Reversibility: "backup", Rollback: "restore backup", Action: &store.DoctorRepairAction{Kind: "stop_run", Before: "running", After: "failed"}}}}
	if err := ValidateDiagnosis(d, e); err != nil {
		t.Fatal("typed proposal refused", err)
	}
	for _, kind := range []string{"poll_flag", "session_file", "worktree_file", "run_interrupt", "session_unlock", "report_completion", "watermark_rewind", "graph_fail", "graph_attempt_cancel", "graph_dispatch_cancel", "graph_wait_cancel", "execute_sql"} {
		d.Proposal[0].Action.Kind = kind
		if err := ValidateDiagnosis(d, e); err == nil {
			t.Error("new legacy/unknown proposal admitted", kind)
		}
	}
	d.Proposal[0].Action = nil
	if err := ValidateDiagnosis(d, e); err == nil {
		t.Fatal("proposal without exact action admitted")
	}
	d.Proposal[0].Action = &store.DoctorRepairAction{Kind: "run_interrupt", Before: "running", After: "failed"}
	if err := validateRetainedDiagnosis(d, e); err != nil {
		t.Fatal("retained legacy proposal unreadable", err)
	}
}
