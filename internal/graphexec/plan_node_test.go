package graphexec_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/planbrief"
)

func nodeDraft() planbrief.PlanDraft {
	return planbrief.PlanDraft{Key: "draft", Name: "Story", UserStory: planbrief.UserStoryContent{Actor: "owner", Want: "a draft", Benefit: "clear scope"}, Problem: "Unclear need", InScope: []string{"drafts"}, NotInScope: []string{}, Dependencies: planbrief.DependencyContent{StoryKeys: []string{}, IssueRefs: []string{}}}
}
func validPlanNode(t *testing.T) []byte {
	t.Helper()
	raw, err := json.Marshal(graphexec.PlanNode{Version: 1, Response: "review_ready", Comment: "Review this intent.", Questions: []string{}, Request: &graphexec.PlanStoryRequest{RequestID: "request", Operation: "create", Stage: "story", Candidate: "candidate", SharedRevision: "shared", Upstream: []string{}, Draft: nodeDraft()}})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestPlanNode_StrictBoundary(t *testing.T) {
	tests := []struct {
		name   string
		change func(string) string
	}{
		{"unsupported version", func(s string) string { return strings.Replace(s, `"version":1`, `"version":2`, 1) }},
		{"missing required questions", func(s string) string { return strings.Replace(s, `,"questions":[]}`, `}`, 1) }},
		{"null questions", func(s string) string { return strings.Replace(s, `"questions":[]`, `"questions":null`, 1) }},
		{"unknown field", func(s string) string { return strings.Replace(s, `"version":1`, `"extra":true,"version":1`, 1) }},
		{"duplicate field", func(s string) string { return strings.Replace(s, `"version":1`, `"version":1,"version":1`, 1) }},
		{"nested duplicate", func(s string) string {
			return strings.Replace(s, `"actor":"owner"`, `"actor":"owner","actor":"other"`, 1)
		}},
		{"trailing document", func(s string) string { return s + ` {}` }},
		{"approved role output", func(s string) string { return strings.Replace(s, `"review_ready"`, `"approved"`, 1) }},
		{"policy selector as role output", func(s string) string { return strings.Replace(s, `"review_ready"`, `"technical"`, 1) }},
		{"ask without questions", func(s string) string { return strings.Replace(s, `"review_ready"`, `"ask"`, 1) }},
		{"initial architecture", func(s string) string {
			return strings.Replace(s, `"frontend":false`, `"frontend":false,"architecture":{"general":[]}`, 1)
		}},
		{"foreign target", func(s string) string {
			return strings.Replace(s, `"request_id":"request"`, `"parent":999,"request_id":"request"`, 1)
		}},
		{"oversize", func(s string) string { return s + strings.Repeat(" ", 128*1024) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			raw := tt.change(string(validPlanNode(t)))
			got, err := graphexec.ParsePlanNode([]byte(raw), planbrief.ContentScope{})
			if err == nil || got.Request != nil {
				t.Fatalf("invalid node escaped: %+v %v", got, err)
			}
		})
	}
	t.Run("valid draft", func(t *testing.T) {
		got, err := graphexec.ParsePlanNode(validPlanNode(t), planbrief.ContentScope{})
		if err != nil || got.Request == nil || got.Request.Draft.Architecture != nil {
			t.Fatal(got, err)
		}
	})
	t.Run("question before draft", func(t *testing.T) {
		raw := []byte(`{"version":1,"response":"ask","request":null,"comment":"The actor is unclear.","questions":["Who needs this?"]}`)
		got, err := graphexec.ParsePlanNode(raw, planbrief.ContentScope{})
		if err != nil || got.Request != nil || len(got.Questions) != 1 {
			t.Fatal(got, err)
		}
	})
}

func TestPlanNode_RetainedSectionsDoNotGrantAuthorship(t *testing.T) {
	for _, stage := range []string{"story", "technical"} {
		t.Run(stage, func(t *testing.T) {
			var node graphexec.PlanNode
			if err := json.Unmarshal(validPlanNode(t), &node); err != nil {
				t.Fatal(err)
			}
			node.Request.Operation = "update"
			node.Request.Stage = stage
			node.Request.ExpectedRevision = strings.Repeat("a", 64)
			node.Request.ChangedSections = []string{"tasks"}
			raw, err := json.Marshal(node)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = graphexec.ParsePlanNode(raw, planbrief.ContentScope{}); err == nil {
				t.Fatal("earlier role can author tasks")
			}
		})
	}
}
