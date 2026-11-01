package taskdiscussion

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestAdvisoryResultPagesPreserveTextAndBoundEscaping(t *testing.T) {
	text := strings.Repeat("\x00界", 9000)
	answer := Answer{Answer: text, Sources: []string{strings.Repeat("\x00", 6000), "saved-plan:immutable"}, Gaps: []string{strings.Repeat("\n", 13000)}, SummarySuggestion: &Summary{Question: strings.Repeat("q", 5000), Conclusion: strings.Repeat("c", 5000), AlternativesOrUncertainty: strings.Repeat("a", 5000), EvidenceBasis: strings.Repeat("e", 5000), Attribution: strings.Repeat("r", 5000)}}
	position := resultPosition{}
	var received strings.Builder
	omissions := map[string]bool{}
	sources := []string{}
	for n := 0; n < 50; n++ {
		page, next, complete, err := resultPage(answer, position)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(page)
		if err != nil || len(raw) > resultPageBytes {
			t.Fatalf("page exceeds encoded budget: %d %v", len(raw), err)
		}
		received.WriteString(page.Answer)
		sources = append(sources, page.Sources...)
		for _, gap := range page.Gaps {
			omissions[gap] = true
		}
		position = next
		if complete {
			break
		}
	}
	if received.String() != text || len(sources) != 1 || sources[0] != "saved-plan:immutable" {
		t.Fatal("pagination lost UTF-8 answer bytes or ordinary source")
	}
	for _, gap := range []string{"oversized_result_source_omitted", "oversized_result_gap_omitted", "oversized_summary_suggestion_omitted"} {
		if !omissions[gap] {
			t.Errorf("omission not disclosed: %s", gap)
		}
	}
	if _, _, _, err := resultPage(Answer{Answer: "界"}, resultPosition{AnswerOffset: 1}); err == nil {
		t.Fatal("page split a Unicode code point")
	}
}

func TestAdvisoryResultCursorBindsAuthorityAndResult(t *testing.T) {
	now := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	service := &Service{now: func() time.Time { return now }, cursorKey: [32]byte{1, 3, 5, 7}}
	owner := Principal{OwnerID: "42", OwnerGeneration: 3, Transport: "mcp", ClientID: "client", GrantID: "grant"}
	position := resultPosition{AnswerOffset: 8192, SourceOffset: 1}
	cursor, err := service.sealResultCursor(position, owner, "job", "result-digest")
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := service.openResultCursor(cursor, owner, "job", "result-digest")
	if err != nil || decoded != position {
		t.Fatalf("stable continuation = %+v %v", decoded, err)
	}
	differentGrant := owner
	differentGrant.GrantID = "replacement"
	for _, tc := range []struct {
		p                   Principal
		job, result, cursor string
	}{
		{differentGrant, "job", "result-digest", cursor},
		{owner, "other-job", "result-digest", cursor},
		{owner, "job", "changed-result", cursor},
		{owner, "job", "result-digest", cursor + "x"},
	} {
		if _, err := service.openResultCursor(tc.cursor, tc.p, tc.job, tc.result); err == nil {
			t.Error("continuation escaped its admission binding")
		}
	}
	now = now.Add(15 * time.Minute)
	if _, err := service.openResultCursor(cursor, owner, "job", "result-digest"); err == nil {
		t.Fatal("expired continuation accepted")
	}
}

func TestAdvisoryPublicBasisKeepsPrivateCapturesLocal(t *testing.T) {
	service := &Service{protect: func(_ context.Context, value string) (string, error) {
		return strings.ReplaceAll(value, "token-marker", "[redacted]"), nil
	}}
	basis := Basis{TaskRef: "github:acme/api#7", Revision: "immutable", Evidence: json.RawMessage(`{"private":"saved-artifact-marker"}`), Guidance: json.RawMessage(`{"private":"role-capture-marker"}`), Sources: []string{"token-marker", strings.Repeat("\x00", 2000)}, Gaps: []string{}}
	public, err := service.publicBasis(t.Context(), basis)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(public)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"saved-artifact-marker", "role-capture-marker", "token-marker"} {
		if strings.Contains(string(raw), secret) {
			t.Errorf("private capture leaked: %s", secret)
		}
	}
	if len(raw) > 12<<10 || len(public.Sources) != 1 || public.Sources[0] != "[redacted]" {
		t.Fatalf("metadata projection is not bounded/redacted: %d bytes", len(raw))
	}
	if string(basis.Evidence) != `{"private":"saved-artifact-marker"}` {
		t.Fatal("projection changed retained immutable evidence")
	}
}
