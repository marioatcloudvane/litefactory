package agentresult_test

import (
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
	"testing"

	ar "github.com/cloudvane/litefactory/internal/agentresult"
)

func researchNodeFixture(t *testing.T, response string) (ar.ResearchNode, ar.Context, []byte) {
	t.Helper()
	outcome := "completed"
	if response == "ask" {
		outcome = "incomplete"
	}
	r, ctx := createFixture("research", outcome)
	if response == "ask" {
		r.Questions = []ar.Question{{ID: "Q-1", Audience: "business", Text: "Which scope should be investigated?"}}
	}
	node := ar.ResearchNode{Version: 1, Response: response, Result: r}
	data, err := json.Marshal(node)
	if err != nil {
		t.Fatal(err)
	}
	return node, ctx, data
}

func TestDecodeResearchNode_ValidResponses(t *testing.T) {
	for _, response := range []string{"ask", "done"} {
		t.Run(response+"/retains authored result", func(t *testing.T) {
			t.Parallel()
			want, ctx, data := researchNodeFixture(t, response)
			got, d := ar.DecodeResearchNode(data, ctx)
			if !d.Valid || !d.Complete || !reflect.DeepEqual(got, want) {
				t.Fatalf("got %+v diagnostics %+v; want %+v", got, d, want)
			}
		})
	}
}

func TestDecodeResearchNode_StrictEnvelope(t *testing.T) {
	cases := []struct {
		name   string
		change func(string) string
	}{
		{"duplicate version", func(s string) string { return strings.Replace(s, `"version":1`, `"version":1,"version":1`, 1) }},
		{"escaped duplicate response", func(s string) string {
			return strings.Replace(s, `"response":"done"`, `"response":"done","respon\u0073e":"ask"`, 1)
		}},
		{"duplicate nested member", func(s string) string {
			return strings.Replace(s, `"outcome":"completed"`, `"outcome":"completed","outcome":"completed"`, 1)
		}},
		{"unknown envelope member", func(s string) string { return strings.Replace(s, `"version":1`, `"version":1,"run_id":7`, 1) }},
		{"unknown result member", func(s string) string {
			return strings.Replace(s, `"outcome":"completed"`, `"outcome":"completed","publish":true`, 1)
		}},
		{"extra object", func(s string) string { return s + ` {}` }},
		{"extra scalar", func(s string) string { return s + ` true` }},
		{"missing version", func(s string) string { return strings.Replace(s, `"version":1,`, "", 1) }},
		{"null version", func(s string) string { return strings.Replace(s, `"version":1`, `"version":null`, 1) }},
		{"string version", func(s string) string { return strings.Replace(s, `"version":1`, `"version":"1"`, 1) }},
		{"fractional version", func(s string) string { return strings.Replace(s, `"version":1`, `"version":1.5`, 1) }},
		{"unknown version", func(s string) string { return strings.Replace(s, `"version":1`, `"version":2`, 1) }},
		{"missing response", func(s string) string { return strings.Replace(s, `"response":"done",`, "", 1) }},
		{"null response", func(s string) string { return strings.Replace(s, `"response":"done"`, `"response":null`, 1) }},
		{"numeric response", func(s string) string { return strings.Replace(s, `"response":"done"`, `"response":1`, 1) }},
		{"unknown response", func(s string) string { return strings.Replace(s, `"response":"done"`, `"response":"continue"`, 1) }},
		{"wrong response case", func(s string) string { return strings.Replace(s, `"response":"done"`, `"response":"Done"`, 1) }},
		{"null result", func(string) string { return `{"version":1,"response":"done","result":null}` }},
		{"missing result", func(string) string { return `{"version":1,"response":"done"}` }},
		{"array result", func(string) string { return `{"version":1,"response":"done","result":[]}` }},
		{"string result", func(string) string { return `{"version":1,"response":"done","result":"{}"}` }},
		{"null outcome", func(s string) string { return strings.Replace(s, `"outcome":"completed"`, `"outcome":null`, 1) }},
		{"null research request", func(s string) string {
			return strings.Replace(s, `"request":"Describe the existing boundary"`, `"request":null`, 1)
		}},
		{"unknown finding member", func(s string) string {
			return strings.Replace(s, `"id":"F-1"`, `"id":"F-1","publish":true`, 1)
		}},
		{"null questions", func(s string) string {
			return strings.Replace(s, `"outcome":"completed"`, `"outcome":"completed","questions":null`, 1)
		}},
		{"empty root", func(string) string { return `{}` }},
		{"null root", func(string) string { return `null` }},
		{"array root", func(s string) string { return `[` + s + `]` }},
		{"empty input", func(string) string { return "" }},
		{"invalid UTF8", func(s string) string { return strings.Replace(s, "Recorded", string([]byte{0xff}), 1) }},
	}
	for _, tc := range cases {
		t.Run(tc.name+"/rejects without candidate", func(t *testing.T) {
			t.Parallel()
			_, ctx, data := researchNodeFixture(t, "done")
			got, d := ar.DecodeResearchNode([]byte(tc.change(string(data))), ctx)
			if d.Valid || len(d.Diagnostics) == 0 || !reflect.DeepEqual(got, ar.ResearchNode{}) {
				t.Fatalf("invalid input escaped: node=%+v diagnostics=%+v", got, d)
			}
		})
	}
}

func TestDecodeResearchNode_ApplicationConsistency(t *testing.T) {
	cases := []struct {
		name, response string
		change         func(*ar.ResearchNode, *ar.Context)
	}{
		{"ask completed", "ask", func(n *ar.ResearchNode, _ *ar.Context) { n.Result.Outcome = "completed"; n.Result.Reason = "" }},
		{"ask no questions", "ask", func(n *ar.ResearchNode, _ *ar.Context) { n.Result.Questions = nil }},
		{"ask empty question", "ask", func(n *ar.ResearchNode, _ *ar.Context) { n.Result.Questions[0].Text = " " }},
		{"oversized question identity", "ask", func(n *ar.ResearchNode, _ *ar.Context) { n.Result.Questions[0].ID = strings.Repeat("Q", 129) }},
		{"ask forbidden questions", "ask", func(_ *ar.ResearchNode, c *ar.Context) { c.QuestionsAllowed = false }},
		{"ask missing reason", "ask", func(n *ar.ResearchNode, _ *ar.Context) { n.Result.Reason = "" }},
		{"done incomplete", "done", func(n *ar.ResearchNode, _ *ar.Context) {
			n.Result.Outcome = "incomplete"
			n.Result.Reason = "Remaining work"
		}},
		{"done with question", "done", func(n *ar.ResearchNode, _ *ar.Context) {
			n.Result.Questions = []ar.Question{{ID: "Q-1", Audience: "business", Text: "Scope?"}}
		}},
		{"done with reason", "done", func(n *ar.ResearchNode, _ *ar.Context) { n.Result.Reason = "Unfinished" }},
		{"empty comment", "done", func(n *ar.ResearchNode, _ *ar.Context) { n.Result.Comment = "" }},
		{"empty research request", "done", func(n *ar.ResearchNode, _ *ar.Context) { n.Result.Payload.Research.Request = "" }},
		{"wrong payload", "done", func(n *ar.ResearchNode, _ *ar.Context) {
			n.Result.Payload.Research = nil
			n.Result.Payload.Spike = &ar.Spike{Request: "Try"}
		}},
		{"wrong factory mode", "done", func(_ *ar.ResearchNode, c *ar.Context) { c.Mode = "implement" }},
	}
	for _, tc := range cases {
		t.Run(tc.name+"/rejects without candidate", func(t *testing.T) {
			t.Parallel()
			n, ctx, _ := researchNodeFixture(t, tc.response)
			tc.change(&n, &ctx)
			data, err := json.Marshal(n)
			if err != nil {
				t.Fatal(err)
			}
			got, d := ar.DecodeResearchNode(data, ctx)
			if d.Valid || len(d.Diagnostics) == 0 || !reflect.DeepEqual(got, ar.ResearchNode{}) {
				t.Fatalf("node=%+v diagnostics=%+v", got, d)
			}
		})
	}
}

func TestDecodeResearchNode_ByteLimit(t *testing.T) {
	for _, size := range []int{ar.MaxResearchNodeBytes, ar.MaxResearchNodeBytes + 1} {
		t.Run(strconv.Itoa(size)+"/bounded envelope", func(t *testing.T) {
			t.Parallel()
			want, ctx, data := researchNodeFixture(t, "done")
			data = append(data, []byte(strings.Repeat(" ", size-len(data)))...)
			got, d := ar.DecodeResearchNode(data, ctx)
			if size == ar.MaxResearchNodeBytes {
				if !d.Valid || !d.Complete || !reflect.DeepEqual(got, want) {
					t.Fatalf("boundary rejected: %+v", d)
				}
			} else if d.Valid || d.Complete || len(d.UncheckedPaths) == 0 || !reflect.DeepEqual(got, ar.ResearchNode{}) {
				t.Fatalf("oversized input not refused before inspection: %+v", d)
			}
		})
	}
}
