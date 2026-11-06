package linear

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/ticket"
)

const bugUUID = "573796c5-4a0c-4fe7-a5c8-260be55f17b0"

func labelledIssueJSON(id string, labelIDs []string) string {
	nodes := make([]string, 0, len(labelIDs))
	for _, l := range labelIDs {
		nodes = append(nodes, fmt.Sprintf(`{"id":%q,"name":"raw-%s"}`, l, l))
	}
	return strings.Replace(issueJSON(id, "project"), `"nodes":[]`, `"nodes":[`+strings.Join(nodes, ",")+`]`, 1)
}

func TestCreateLabelledWithID(t *testing.T) {
	var inputs []map[string]any
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Query     string
			Variables map[string]json.RawMessage
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		var input map[string]any
		if err := json.Unmarshal(req.Variables["input"], &input); err != nil {
			t.Fatal(err)
		}
		inputs = append(inputs, input)
		_, _ = fmt.Fprintf(w, `{"data":{"issueCreate":{"success":true,"issue":%s}}}`, labelledIssueJSON(bugUUID, []string{"bug-id"}))
	})
	c.config.BugLabelID = "bug-id"
	title, body := mustBody(t, "Title"), mustBody(t, "Body")
	for _, label := range []string{"", "other"} {
		if _, err := c.CreateLabelledWithID(context.Background(), c.Container(), bugUUID, label, title, body); err == nil {
			t.Fatalf("label %q accepted", label)
		}
	}
	if len(inputs) != 0 {
		t.Fatal("refused label sent a request")
	}
	got, err := c.CreateLabelledWithID(context.Background(), c.Container(), bugUUID, "bug-id", title, body)
	if err != nil {
		t.Fatal(err)
	}
	in := inputs[0]
	labels, _ := in["labelIds"].([]any)
	if in["id"] != bugUUID || in["teamId"] != "team" || in["projectId"] != "project" || len(labels) != 1 || labels[0] != "bug-id" {
		t.Fatalf("input %+v", in)
	}
	if len(got.Labels) != 1 || got.Labels[0] != "bug" {
		t.Fatalf("labels %v", got.Labels)
	}
	if _, err := c.CreateWithID(context.Background(), c.Container(), bugUUID, title, body); err != nil {
		t.Fatal(err)
	}
	if _, ok := inputs[1]["labelIds"]; ok {
		t.Fatal("plain create carried labelIds")
	}
	unset := testClient(t, func(http.ResponseWriter, *http.Request) { t.Error("request without bug label") })
	if _, err := unset.CreateLabelledWithID(context.Background(), unset.Container(), bugUUID, "bug-id", title, body); err == nil {
		t.Fatal("create without configured bug label accepted")
	}
}

func TestAddBugLabel(t *testing.T) {
	stored := []string{"other"}
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Query     string
			Variables map[string]json.RawMessage
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(req.Query, "issueUpdate") {
			var input struct {
				LabelIDs []string `json:"labelIds"`
			}
			if err := json.Unmarshal(req.Variables["input"], &input); err != nil {
				t.Fatal(err)
			}
			stored = input.LabelIDs
			_, _ = fmt.Fprintf(w, `{"data":{"issueUpdate":{"success":true,"issue":%s}}}`, labelledIssueJSON("ticket", stored))
			return
		}
		_, _ = fmt.Fprintf(w, `{"data":{"issue":%s}}`, labelledIssueJSON("ticket", stored))
	})
	ref := ticket.Ref{Identity: c.identity("ticket"), Locator: c.locator("ticket")}
	err := c.AddLabel(context.Background(), ref, "bug")
	var failed *ticket.Failure
	if !errors.As(err, &failed) || failed.Kind != ticket.Refused {
		t.Fatalf("unmapped bug accepted: %v", err)
	}
	c.config.BugLabelID = "bug-id"
	if err := c.AddLabel(context.Background(), ref, "bug"); err != nil {
		t.Fatal(err)
	}
	if len(stored) != 2 || stored[0] != "other" || stored[1] != "bug-id" {
		t.Fatalf("stored %v", stored)
	}
	snap, err := c.Read(context.Background(), ref)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Labels) != 2 || snap.Labels[1] != "bug" || snap.Labels[0] != "raw-other" {
		t.Fatalf("labels %v", snap.Labels)
	}
}
