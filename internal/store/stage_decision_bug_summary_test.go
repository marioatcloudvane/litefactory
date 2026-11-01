package store

import (
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/buganalysis"
)

func TestStageDecisionBugPreviewUsesSavedDiagnosisBeyondPublicProjection(t *testing.T) {
	db, _, instance, session, candidate, conversation := graphDiagnosisFixture(t)
	public := candidate
	public.Analysis = buganalysis.BugAnalysis{}
	parkGraphDiagnosisReview(t, db, instance, session, public, conversation, true)
	captureDecisionProfile(t, db, instance.ID, decisionResearchProfile)
	target, err := db.ResolveStageDecisionTarget(t.Context(), session.TaskRef, "analysis", "approve_stage")
	if err != nil || !strings.Contains(target.Summary, candidate.Analysis.Cause) || !strings.Contains(target.Summary, candidate.Analysis.Remedy) {
		t.Fatalf("preview lost retained diagnosis: %s %v", target.Summary, err)
	}
}
