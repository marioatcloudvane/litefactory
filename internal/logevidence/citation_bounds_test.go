package logevidence

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/mcpadapter"
	"github.com/cloudvane/litefactory/internal/submission"
)

func TestEscapedExcerptCitationRecoveryUsesDeliveredLength(t *testing.T) {
	f := newEvidenceFixture(t)
	event, err := json.Marshal(map[string]string{"type": "assistant", "text": "fixture-secret-value " + strings.Repeat("\"<>&\\\n", 1200)})
	if err != nil {
		t.Fatal(err)
	}
	run := f.run(t, f.repo, 1, string(event)+"\n", false)
	scope := Scope{RepositoryIDs: []int64{f.repo.ID}}
	broker := evidenceBroker(t, f.service(t, scope, nil))
	first, wire, err := evidenceCall(t.Context(), broker, "run_logs_read", ReadRequest{RunID: run.ID})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := mcpadapter.EncodeToolResult(wire)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) > pageMaximum || first.Status != "ok" || len(first.Records) != 1 {
		t.Fatalf("bounded excerpt: encoded bytes %d, result %+v", len(encoded), first)
	}
	item := first.Records[0]
	if !item.Truncated || item.Redactions < 1 || !first.Coverage.Incomplete || item.Citation.ExcerptBytes != len(item.Text) || item.Citation.Digest != digestText(item.Text) {
		t.Fatalf("missing sanitized excerpt identity: %+v", item)
	}
	if bytes.Contains(encoded, []byte("fixture-secret-value")) {
		t.Fatal("credential present in encoded response")
	}
	// This fixture expands strongly in both JSON representations, forcing the
	// adapter-size bound to shorten the already sanitized excerpt further.
	if item.Citation.ExcerptBytes >= 700 {
		t.Fatalf("escaped excerpt did not exercise encoded-size shortening: %d", item.Citation.ExcerptBytes)
	}
	replacement := evidenceBroker(t, f.service(t, scope, nil))
	recovered := requireEvidenceCall(t, replacement, "run_logs_read", ReadRequest{RunID: run.ID, Citation: &item.Citation})
	if recovered.Status != "ok" || len(recovered.Records) != 1 || recovered.Records[0].Text != item.Text || !citationsEqual(recovered.Records[0].Citation, item.Citation) {
		t.Fatalf("reread changed delivered excerpt: %+v", recovered)
	}
}

func TestCitationCannotStartInsideMalformedRecord(t *testing.T) {
	f := newEvidenceFixture(t)
	const valid = `{"type":"assistant","text":"inner valid evidence"}` + "\n"
	good := f.run(t, f.repo, 1, valid, false)
	bad := f.run(t, f.repo, 2, "bad"+valid, false)
	broker := evidenceBroker(t, f.service(t, Scope{RepositoryIDs: []int64{f.repo.ID}}, nil))
	original := requireEvidenceCall(t, broker, "run_logs_read", ReadRequest{RunID: good.ID})
	if len(original.Records) != 1 {
		t.Fatalf("valid fixture: %+v", original)
	}
	citation := original.Records[0].Citation
	citation.RunID = bad.ID
	citation.Ordinal = bad.Ordinal
	citation.StartedAt = bad.StartedAt
	citation.TaskRef = "github:allowed/repo#2"
	citation.Start = 3
	citation.End = int64(3 + len(valid))
	result := requireEvidenceCall(t, broker, "run_logs_read", ReadRequest{RunID: bad.ID, Citation: &citation})
	if result.Status != "source_changed" || len(result.Records) != 0 {
		t.Fatalf("mid-record citation bypassed malformed prefix: %+v", result)
	}
}

func TestAlteredCursorDoesNotMoveSourceRange(t *testing.T) {
	f := newEvidenceFixture(t)
	source := "{\"type\":\"result\",\"result\":\"one\"}\n{\"type\":\"result\",\"result\":\"two\"}\n"
	run := f.run(t, f.repo, 1, source, false)
	broker := evidenceBroker(t, f.service(t, Scope{RepositoryIDs: []int64{f.repo.ID}}, nil))
	first := requireEvidenceCall(t, broker, "run_logs_read", ReadRequest{RunID: run.ID, Limit: 1})
	if first.NextCursor == "" {
		t.Fatal("fixture did not produce cursor")
	}
	// Change a signed payload byte, retaining the original signature.
	token := []byte(first.NextCursor)
	if token[0] == 'A' {
		token[0] = 'B'
	} else {
		token[0] = 'A'
	}
	result := requireEvidenceCall(t, broker, "run_logs_read", ReadRequest{RunID: run.ID, Cursor: string(token)})
	if result.Status != "expired_cursor" || len(result.Records) != 0 || result.NextCursor != "" {
		t.Fatalf("altered cursor accepted: %+v", result)
	}
}

func TestExplicitInvalidLimitsAreRejected(t *testing.T) {
	f := newEvidenceFixture(t)
	run := f.run(t, f.repo, 1, "{\"type\":\"result\",\"result\":\"done\"}\n", false)
	for _, tool := range []string{"runs_list", "run_logs_read"} {
		t.Run(tool, func(t *testing.T) {
			broker := evidenceBroker(t, f.service(t, Scope{RepositoryIDs: []int64{f.repo.ID}}, nil))
			for _, limit := range []string{"null", "-0", "0"} {
				args := json.RawMessage(fmt.Sprintf(`{"limit":%s}`, limit))
				if tool == "run_logs_read" {
					args = json.RawMessage(fmt.Sprintf(`{"run_id":%d,"limit":%s}`, run.ID, limit))
				}
				result := requireEvidenceCall(t, broker, tool, args)
				if result.Status != "invalid_request" || len(result.Records) != 0 || len(result.Runs) != 0 || !result.Coverage.Incomplete {
					t.Fatalf("limit %s accepted: %+v", limit, result)
				}
			}
		})
	}
}

func TestTerminalNoticeReservesEncodedCorrelationBytes(t *testing.T) {
	f := newEvidenceFixture(t)
	budget, err := NewBudget(Limits{Bytes: 2048})
	if err != nil {
		t.Fatal(err)
	}
	service := f.service(t, Scope{RepositoryIDs: []int64{f.repo.ID}}, budget)
	id := strings.Repeat("a", 128)
	raw, err := service.execute(t.Context(), id, 0, func() Result { return Result{Status: "ok"} })
	if err != nil {
		t.Fatalf("terminal notice unavailable for 128-byte request identity: %v", err)
	}
	encoded, err := mcpadapter.EncodeToolResult(submission.Response{RequestID: id, Result: raw})
	if err != nil {
		t.Fatal(err)
	}
	var result Result
	if err = json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	if len(encoded) > terminalReserve || result.Status != "budget_exhausted" || result.Trigger != "bytes" || result.Budget.Consumed.Bytes != len(encoded) {
		t.Fatalf("terminal notice encoded %d bytes: %+v", len(encoded), result)
	}
}

func TestByteReservationExhaustionDoesNotOfferUnusableCursor(t *testing.T) {
	f := newEvidenceFixture(t)
	run := f.run(t, f.repo, 1, "{\"type\":\"assistant\",\"text\":\"one\"}\n{\"type\":\"result\",\"result\":\"two\"}\n", false)
	budget, err := NewBudget(Limits{Bytes: 10000})
	if err != nil {
		t.Fatal(err)
	}
	broker := evidenceBroker(t, f.service(t, Scope{RepositoryIDs: []int64{f.repo.ID}}, budget))
	result, wire, err := evidenceCall(t.Context(), broker, "run_logs_read", ReadRequest{RunID: run.ID, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := mcpadapter.EncodeToolResult(wire)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "budget_exhausted" || result.Trigger != "bytes" || result.NextCursor != "" || !result.Coverage.Incomplete || len(result.Records) != 1 || result.Budget.Consumed.Bytes != len(encoded) {
		t.Fatalf("unusable continuation at byte boundary: %+v", result)
	}
	if _, _, err := evidenceCall(t.Context(), broker, "run_logs_read", ReadRequest{RunID: run.ID}); err == nil {
		t.Fatal("terminal byte budget accepted retry")
	}
}
