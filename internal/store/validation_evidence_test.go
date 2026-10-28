package store

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func validationRequest(t *testing.T, db *DB, source string) ValidationRequest {
	t.Helper()
	req, err := db.EnsureValidationRequest(t.Context(), ValidationRequest{Lineage: "repository:session", SourceKey: source, Policy: "full", Commit: suiteSHA})
	if err != nil {
		t.Fatal(err)
	}
	return req
}

func TestValidationRequestGenerationsAndReplay(t *testing.T) {
	db := newTestDB(t)
	first := validationRequest(t, db, "comment:1")
	replay := validationRequest(t, db, "comment:1")
	second := validationRequest(t, db, "comment:2")
	if first.ID != replay.ID || first.Generation != 1 || second.Generation != 2 {
		t.Fatalf("generations: %+v %+v %+v", first, replay, second)
	}
	changed := first
	changed.Policy = "auto"
	if _, err := db.EnsureValidationRequest(t.Context(), changed); err == nil {
		t.Fatal("changed replay accepted")
	}
	if _, err := db.sql.ExecContext(t.Context(), `DELETE FROM validation_requests WHERE id=?`, first.ID); err == nil {
		t.Fatal("request deleted")
	}
}

func TestValidationEvidenceDurableFinalization(t *testing.T) {
	db := newTestDB(t)
	request := validationRequest(t, db, "comment:1")
	evidence, err := db.AppendValidationEvidence(t.Context(), ValidationEvidence{RequestID: request.ID, Kind: "observation", Version: 1, Complete: true, Payload: json.RawMessage(`{"case":"TestA","status":"failed"}`)})
	if err != nil {
		t.Fatal(err)
	}
	assessment := ValidationEvidence{RequestID: request.ID, Version: 1, Complete: true, Payload: json.RawMessage(`{"state":"covered"}`), References: []string{evidence.ID, strings.Repeat("f", 64)}}
	if _, err := db.FinalizeValidationAssessment(t.Context(), assessment); err == nil {
		t.Fatal("dangling evidence accepted")
	}
	rows, err := db.ValidationEvidenceForRequest(t.Context(), request.ID)
	if err != nil || len(rows) != 1 {
		t.Fatalf("partial finalization: %+v %v", rows, err)
	}
	assessment.References = []string{evidence.ID}
	final, err := db.FinalizeValidationAssessment(t.Context(), assessment)
	if err != nil {
		t.Fatal(err)
	}
	again, err := db.FinalizeValidationAssessment(t.Context(), assessment)
	if err != nil || again.ID != final.ID || !again.CreatedAt.Equal(final.CreatedAt) {
		t.Fatalf("replay: %+v %v", again, err)
	}
	got, err := db.LatestValidationEvidence(t.Context(), request.Lineage, "assessment")
	if err != nil || got.ID != final.ID {
		t.Fatalf("latest: %+v %v", got, err)
	}
	if _, err := db.sql.ExecContext(t.Context(), `UPDATE validation_evidence SET complete=0 WHERE id=?`, final.ID); err == nil {
		t.Fatal("immutable evidence changed")
	}
	if _, err := db.sql.ExecContext(t.Context(), `DELETE FROM validation_evidence WHERE id=?`, evidence.ID); err == nil {
		t.Fatal("referenced evidence deleted")
	}
}

func TestValidationEvidenceIncompleteAndLineage(t *testing.T) {
	db := newTestDB(t)
	req := validationRequest(t, db, "comment:1")
	incomplete, err := db.AppendValidationEvidence(t.Context(), ValidationEvidence{RequestID: req.ID, Kind: "observation", Version: 1, Payload: json.RawMessage(`{"capture":"truncated"}`)})
	if err != nil {
		t.Fatal(err)
	}
	assessment := ValidationEvidence{RequestID: req.ID, Version: 1, Complete: true, Payload: json.RawMessage(`{}`), References: []string{incomplete.ID}}
	if _, err := db.FinalizeValidationAssessment(t.Context(), assessment); err == nil {
		t.Fatal("incomplete evidence established coverage")
	}
	assessment.Complete = false
	if _, err := db.FinalizeValidationAssessment(t.Context(), assessment); err != nil {
		t.Fatal(err)
	}
	next := validationRequest(t, db, "comment:2")
	if _, err := db.AppendValidationEvidence(t.Context(), ValidationEvidence{RequestID: next.ID, Kind: "origin", Version: 1, Payload: json.RawMessage(`{}`), References: []string{incomplete.ID}}); err != nil {
		t.Fatal(err)
	}
	other, err := db.EnsureValidationRequest(t.Context(), ValidationRequest{Lineage: "other-repository:session", SourceKey: "comment:1", Policy: "auto", Commit: suiteSHA})
	if err != nil {
		t.Fatal(err)
	}
	assessment.RequestID = other.ID
	if _, err := db.FinalizeValidationAssessment(t.Context(), assessment); err == nil {
		t.Fatal("foreign lineage accepted")
	}
}

func TestValidationEvidenceBoundsAndLegacy(t *testing.T) {
	db := newTestDB(t)
	req := validationRequest(t, db, "comment:1")
	in := ValidationEvidence{RequestID: req.ID, Kind: "manifest", Version: 1, Complete: true, Payload: json.RawMessage(`{}`)}
	in.ID = strings.Repeat("a", 64)
	if _, err := db.AppendValidationEvidence(t.Context(), in); err == nil {
		t.Fatal("wrong digest accepted")
	}
	in.ID = ""
	in.Payload = json.RawMessage(`"` + strings.Repeat("a", MaxValidationEvidenceBytes) + `"`)
	if _, err := db.AppendValidationEvidence(t.Context(), in); err == nil {
		t.Fatal("oversize accepted")
	}
	in.Payload = json.RawMessage(`{}`)
	in.Version = 0
	if _, err := db.AppendValidationEvidence(t.Context(), in); err == nil {
		t.Fatal("unknown version accepted")
	}
	run := suiteBegin(t, db, 99)
	if err := db.FinishSuiteRun(t.Context(), run.ID, SuitePassed, "t", "", graphTestTime()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.LatestValidationEvidence(t.Context(), req.Lineage, "assessment"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("legacy suite manufactured assessment: %v", err)
	}
}

func TestValidationCommandEvidenceBeforeSuiteFinalization(t *testing.T) {
	db := newTestDB(t)
	req := validationRequest(t, db, "comment:1")
	run := suiteBegin(t, db, 1)
	observation, err := db.AppendValidationEvidence(t.Context(), ValidationEvidence{RequestID: req.ID, RunID: run.ID, Kind: "observation", Version: 1, Complete: true, Payload: json.RawMessage(`{"status":"pass"}`)})
	if err != nil {
		t.Fatal(err)
	}
	assessment := ValidationEvidence{RequestID: req.ID, RunID: run.ID, Version: 1, Complete: true, Payload: json.RawMessage(`{"state":"passed"}`), References: []string{observation.ID}}
	if _, err := db.FinalizeValidationAssessment(t.Context(), assessment); err == nil {
		t.Fatal("assessment finalized before suite completion")
	}
	if err := db.FinishSuiteRun(t.Context(), run.ID, SuitePassed, "t", "", graphTestTime()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.FinalizeValidationAssessment(t.Context(), assessment); err != nil {
		t.Fatal(err)
	}
}

func TestValidationEvidenceRejectsDetachedRunCommit(t *testing.T) {
	db := newTestDB(t)
	req := validationRequest(t, db, "comment:1")
	got, err := db.GetValidationRequest(t.Context(), req.ID)
	if err != nil || got.Commit != req.Commit || got.Lineage != req.Lineage {
		t.Fatalf("request lookup: %+v %v", got, err)
	}
	run := suiteBegin(t, db, 1)
	if err := db.FinishSuiteRun(t.Context(), run.ID, SuitePassed, "t", "", graphTestTime()); err != nil {
		t.Fatal(err)
	}
	other, err := db.EnsureValidationRequest(t.Context(), ValidationRequest{Lineage: req.Lineage, SourceKey: "comment:other-commit", Policy: "auto", Commit: strings.Repeat("b", 40)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.AppendValidationEvidence(t.Context(), ValidationEvidence{RequestID: other.ID, RunID: run.ID, Kind: "observation", Version: 1, Complete: true, Payload: json.RawMessage(`{}`)}); err == nil {
		t.Fatal("complete artifact attached to wrong commit")
	}
}

func TestValidationEvidenceLegacyReuseExcludesCoverageRuns(t *testing.T) {
	for _, legacyAvailable := range []bool{false, true} {
		t.Run(map[bool]string{false: "only targeted raw pass", true: "legacy full pass remains reusable"}[legacyAvailable], func(t *testing.T) {
			db := newTestDB(t)
			if legacyAvailable {
				legacy := suiteBegin(t, db, 1)
				if err := db.FinishSuiteRun(t.Context(), legacy.ID, SuitePassed, "legacy-full-tree", "", graphTestTime()); err != nil {
					t.Fatal(err)
				}
			}
			request := validationRequest(t, db, "coverage-request")
			targeted := suiteBegin(t, db, 2)
			if _, err := db.AppendValidationEvidence(t.Context(), ValidationEvidence{RequestID: request.ID, RunID: targeted.ID, Kind: "manifest", Version: 1, Complete: false, Payload: json.RawMessage(`{"state":"started"}`)}); err != nil {
				t.Fatal(err)
			}
			if err := db.FinishSuiteRun(t.Context(), targeted.ID, SuitePassed, "targeted-tree", "", graphTestTime()); err != nil {
				t.Fatal(err)
			}
			reused, err := db.ReuseSuiteRun(t.Context(), 3, "suite", suiteSHA, graphTestTime())
			if legacyAvailable {
				if err != nil || reused.TreeAfter != "legacy-full-tree" {
					t.Fatalf("legacy reuse chose partial coverage: %+v %v", reused, err)
				}
			} else if !errors.Is(err, ErrNotFound) {
				t.Fatalf("targeted raw pass reused as full: %+v %v", reused, err)
			}
		})
	}
}
