package store

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestPreventionLocalEvidenceBoundsAndRepairPrefix(t *testing.T) {
	db, _, inst, session, _, attempt := graphDiagnosisFixture(t)
	ctx := t.Context()
	if session.TaskRef != "github:acme/api#457" {
		t.Fatal(session.TaskRef)
	}
	records, err := db.ReadPreventionBugEvidence(ctx, "acme/api", 457, 1, 65536)
	if !errors.Is(err, ErrPreventionEvidenceUnavailable) || len(records) != 1 || records[0].Kind != "diagnosis" {
		t.Fatal(records, err)
	}
	diagnosisBytes := len(records[0].Body)
	if _, err = db.RecordGraphOutput(ctx, GraphOutput{NodeAttemptID: attempt.ID, Kind: GraphOutputJSON, Payload: []byte(fmt.Sprintf(`{"record":"repair_eligibility","instance_id":%d,"attempt_id":2,"result_id":3,"result_revision":1,"repair":{"summary":"Added missing validation","checks":[{"command":"go test","outcome":"passed"}]}}`, inst.ID)), CreatedAt: graphTestTime()}); err != nil {
		t.Fatal(err)
	}
	records, err = db.ReadPreventionBugEvidence(ctx, "acme/api", 457, 1, 65536)
	if !errors.Is(err, ErrPreventionEvidenceLimited) || len(records) != 2 || records[1].Kind != "repair_eligibility" || records[1].InstanceID != inst.ID {
		t.Fatal(records, err)
	}
	if !strings.Contains(string(records[1].Body), "Added missing validation") {
		t.Fatal("repair observation lost")
	}
	prefix, err := db.ReadPreventionBugEvidence(ctx, "acme/api", 457, 1, diagnosisBytes+32)
	if !errors.Is(err, ErrPreventionEvidenceLimited) || len(prefix) != 1 || len(prefix[0].Body) > diagnosisBytes+32 {
		t.Fatal(prefix, err)
	}
	tiny, err := db.ReadPreventionBugEvidence(ctx, "acme/api", 457, 1, 64)
	if !errors.Is(err, ErrPreventionEvidenceLimited) || len(tiny) != 0 {
		t.Fatal(tiny, err)
	}
	bound, boundErr := db.ReadPreventionRepositoryEvidence(ctx, session.RepoID, "ACME/API", 457, 1, 65536)
	if !errors.Is(boundErr, ErrPreventionEvidenceLimited) || len(bound) != 2 {
		t.Fatal("selected registration or case-folded identity lost", bound, boundErr)
	}
	foreign, foreignErr := db.ReadPreventionRepositoryEvidence(ctx, session.RepoID+100, "acme/api", 457, 1, 65536)
	if !errors.Is(foreignErr, ErrPreventionEvidenceUnavailable) || len(foreign) != 0 {
		t.Fatal("foreign registration evidence admitted", foreign, foreignErr)
	}
	outside, err := db.ReadPreventionBugEvidence(ctx, "other/repo", 457, 1, 65536)
	if !errors.Is(err, ErrPreventionEvidenceUnavailable) || len(outside) != 0 {
		t.Fatal(outside, err)
	}
}
