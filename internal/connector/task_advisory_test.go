package connector

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/store"
)

func TestTaskAdvisoryArgumentBoundary(t *testing.T) {
	summary := `{"question":"Why?","conclusion":"Keep it private.","alternatives_or_uncertainty":"No alternative agreed.","evidence_basis":"Saved review.","attribution":"Agent recommendation."}`
	for _, tc := range []struct{ name, raw string }{
		{"ask_task_agent", `{"task_ref":"github:acme/api#7","question":"Why?","client_request_key":"one"}`},
		{"judge_task", `{"task_ref":"github:acme/api#7","client_request_key":"two"}`},
		{"advisory_job_get", `{"job_ref":"job-one","cursor":"page-two"}`},
		{"advisory_job_cancel", `{"job_ref":"job-one"}`},
		{"discussion_open", `{"task_ref":"github:acme/api#7","client_request_key":"chat-one"}`},
		{"discussion_summary_publish", `{"task_ref":"github:acme/api#7","conversation_ref":"chat-one","conclusion_id":"settled-one","summary":` + summary + `}`},
		{"discussion_summary_get", `{"publication_ref":"pub-one","cancel_pending":true}`},
	} {
		if _, err := parseAdvisoryArguments(tc.name, json.RawMessage(tc.raw)); err != nil {
			t.Errorf("%s rejected valid contract: %v", tc.name, err)
		}
	}
	for _, tc := range []struct{ name, raw string }{
		{"ask_task_agent", `{}`},
		{"ask_task_agent", `{"task_ref":"a","question":"why","client_request_key":"one","role":"plan-technical"}`},
		{"ask_task_agent", `{"task_ref":"a","task_ref":"b","question":"why","client_request_key":"one"}`},
		{"ask_task_agent", `{"task_ref":"a","question":null,"client_request_key":"one"}`},
		{"ask_task_agent", `{"task_ref":"a","question":"why","client_request_key":"one","conversation_revision":1}`},
		{"ask_task_agent", `{"task_ref":"a","question":"why","client_request_key":"one","conversation_ref":"c","conversation_revision":-1}`},
		{"ask_task_agent", `{"task_ref":"a","question":"` + strings.Repeat("界", 10923) + `","client_request_key":"one"}`},
		{"judge_task", `{"task_ref":"a","client_request_key":"one","question":"ask instead"}`},
		{"discussion_summary_publish", `{"task_ref":"a","conversation_ref":"c","conclusion_id":"one","summary":` + strings.Replace(summary, `"question":"Why?"`, `"question":"Why?","question":"Again?"`, 1) + `}`},
		{"discussion_summary_publish", `{"task_ref":"a","conversation_ref":"c","conclusion_id":"one","summary":` + strings.Replace(summary, `"attribution":"Agent recommendation."`, `"attribution":"Agent recommendation.","approval":true`, 1) + `}`},
		{"discussion_summary_publish", `{"task_ref":"a","conversation_ref":"c","conclusion_id":"one","summary":` + strings.Replace(summary, `"evidence_basis":"Saved review."`, `"evidence_basis":""`, 1) + `}`},
		{"discussion_summary_get", `{"publication_ref":"one","cancel_pending":null}`},
		{"advisory_job_cancel", `{"job_ref":"one","cancel_pending":true}`},
		{"advisory_job_get", `{"job_ref":"one"} {}`},
	} {
		if _, err := parseAdvisoryArguments(tc.name, json.RawMessage(tc.raw)); err == nil {
			t.Errorf("%s accepted invalid contract: %s", tc.name, tc.raw)
		}
	}
}

func TestTaskAdvisoryScopeSeparation(t *testing.T) {
	for _, tc := range []struct {
		scope    string
		expected []string
	}{
		{DiscussionScope, nil},
		{AdvisoryScope, []string{"ask_task_agent", "judge_task", "advisory_job_get", "advisory_job_cancel", "discussion_open"}},
		{DiscussionPublishScope, []string{"discussion_open", "discussion_summary_publish", "discussion_summary_get"}},
	} {
		var actual []string
		for _, name := range advisoryToolNames {
			if advisoryToolAllowed(store.ConnectorGrant{Scope: tc.scope}, name) {
				actual = append(actual, name)
			}
		}
		if strings.Join(actual, ",") != strings.Join(tc.expected, ",") {
			t.Errorf("scope %s exposes %v, want %v", tc.scope, actual, tc.expected)
		}
	}
}
