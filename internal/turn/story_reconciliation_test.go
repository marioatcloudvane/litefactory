package turn

import (
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/planbrief"
)

func reconciliationPublicationFixture(t *testing.T) (*preparation, typedPublicationOptions, *typedPlanForge) {
	t.Helper()
	_, prep, o, forge := typedPublicationFixture(t)
	forge.issues[0].State = github.StateOpen
	return prep, o, forge
}

func TestStoryReconciliationPresentationApprovalRechecksCurrentBytes(t *testing.T) {
	for _, testCase := range []string{"presentation", "semantic", "race", "locked"} {
		t.Run(testCase, func(t *testing.T) {
			prep, o, forge := reconciliationPublicationFixture(t)
			original := forge.issues[0]
			forge.issues[0].Body = strings.Replace(original.Body, "String must be x", "String must be\n  x", 1)
			if testCase == "semantic" {
				forge.issues[0].Body = strings.Replace(original.Body, "String must be x", "String must be y", 1)
			}
			feature, err := o.DB.GetManagedFeature(t.Context(), o.Parent.String())
			mustProgress(t, err)
			if testCase == "locked" {
				feature.State = "implementing"
				mustProgress(t, o.DB.CompareManagedFeature(t.Context(), feature, "planning"))
				stories, err := o.DB.ListManagedStories(t.Context(), o.Parent.String(), 1)
				mustProgress(t, err)
				_, err = reconcilePresentationIssue(t.Context(), o.DB, forge, prep.ref, stories[0], forge.issues[0])
				if err == nil || !strings.Contains(err.Error(), "frozen") {
					t.Fatalf("active scope changed: %v", err)
				}
				return
			}
			var source SubIssueCreator = forge
			if testCase == "race" {
				source = &typedApprovalRaceForge{typedPlanForge: forge}
			}
			revisions, err := readScopeForApproval(t.Context(), Options{DB: o.DB, SubIssueCreator: source}, prep.ref, feature)
			if testCase != "presentation" {
				if err == nil {
					t.Fatalf("%s edit acquired approval", testCase)
				}
				if testCase == "semantic" && !strings.Contains(err.Error(), "Plan") {
					t.Fatalf("missing ordinary-language pending explanation: %v", err)
				}
				return
			}
			mustProgress(t, err)
			if len(revisions) != 1 || revisions[0].Body != forge.issues[0].Body {
				t.Fatalf("approval did not freeze current bytes: %+v", revisions)
			}
			mustProgress(t, o.DB.SetManagedFeatureReady(t.Context(), o.Parent.String(), 1, true))
			mustProgress(t, o.DB.ApproveManagedStories(t.Context(), o.Parent.String(), 1, revisions))
			frozen, err := o.DB.ListApprovedStoryRevisions(t.Context(), o.Parent.String(), 1)
			mustProgress(t, err)
			if len(frozen) != 1 || frozen[0].Body != forge.issues[0].Body || forge.updates != 0 {
				t.Fatal("presentation approval rewrote or lost source")
			}
			old, err := o.DB.FindSubmissionProjection(t.Context(), o.Parent.String(), "US-001", planbrief.StoryHash(original.Title, original.Body), 1)
			mustProgress(t, err)
			if old.Body != original.Body {
				t.Fatal("old projection lost")
			}
		})
	}
}
