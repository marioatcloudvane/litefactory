package taskevidence

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/connector"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
)

type fixtureReader struct {
	items []map[string]any
	gaps  []string
	fail  bool
}

func (r fixtureReader) ReadTaskEvidence(_ context.Context, _ int64, _ string, offset, limit int) (store.TaskEvidencePage, error) {
	if r.fail {
		return store.TaskEvidencePage{}, errors.New("private /Users/private/error")
	}
	end := min(offset+limit, len(r.items))
	return store.TaskEvidencePage{Revision: "fixed", Items: r.items[offset:end], Sources: []map[string]any{{"source": "fixture", "observed_at": "2026-01-01"}}, Gaps: r.gaps, HasMore: end < len(r.items), NextOffset: end}, nil
}

func TestTaskEvidenceSanitizesWholeUnitsAndFailsClosed(t *testing.T) {
	token := strings.Repeat("sensitive-token-", 5)
	reader := fixtureReader{items: []map[string]any{{"id": "one", "comment": "text " + token + " at /Users/private/project/evidence.txt", "url": "https://user:" + token + "@example.org/x", "metadata": map[string]any{"path": "file:///Users/private/key", "note": token, "encoded_json": `{"path":"\u002fUsers/private/key"}`, "encoded_url": "https://example.org/?key=" + url.QueryEscape(url.QueryEscape(token))}, "questions": []any{map[string]any{"text": token, "options": []any{map[string]any{"text": token}}}}}}}
	redactor := redact.New(redact.Options{Token: secrets.NewToken(token), Environ: []string{}})
	s := New(reader, redactor, nil, "/Users/private")
	page, err := s.Evidence(t.Context(), store.ConnectorGrantTarget{SessionID: 1}, connector.EvidenceRequest{Section: "results", Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(page)
	if strings.Contains(string(raw), token) || strings.Contains(string(raw), "/Users/private") {
		t.Fatalf("sensitive facts survived: %s", raw)
	}
	// An invalid redactor cannot release any evidence, including already clean
	// preceding fields or source metadata.
	s.redactor = &redact.Redactor{}
	page, err = s.Evidence(t.Context(), store.ConnectorGrantTarget{SessionID: 1}, connector.EvidenceRequest{Section: "results", Limit: 50})
	if err == nil || len(page.Items) > 0 || len(page.Sources) > 0 {
		t.Fatalf("redaction failure released facts: %+v %v", page, err)
	}
}

func TestTaskEvidenceFiniteByteAndRecordPages(t *testing.T) {
	records := []map[string]any{{"id": "oversized", "comment": strings.Repeat("x", 300<<10)}}
	for i := range 129 {
		records = append(records, map[string]any{"id": fmt.Sprintf("record-%03d", i), "comment": strings.Repeat("quoted \"界\" ", 120)})
	}
	s := New(fixtureReader{items: records}, redact.New(redact.Options{Environ: []string{}}), nil)
	offset := 0
	seen := map[string]bool{}
	unavailable := 0
	for pages := 0; ; pages++ {
		if pages > 140 {
			t.Fatal("paging did not terminate")
		}
		p, err := s.Evidence(t.Context(), store.ConnectorGrantTarget{SessionID: 1}, connector.EvidenceRequest{Section: "results", Offset: offset, Limit: 50})
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(p)
		escaped, _ := json.Marshal(string(raw))
		if len(raw)+len(escaped) > 56<<10 || len(p.Items) > 50 {
			t.Fatalf("page exceeds bound: %d bytes %d records", len(raw)+len(escaped), len(p.Items))
		}
		for _, item := range p.Items {
			if item["unavailable"] == true {
				unavailable++
				continue
			}
			id, ok := item["id"].(string)
			if !ok {
				t.Fatal("missing record identity")
			}
			if seen[id] {
				t.Fatalf("duplicate %s", id)
			}
			seen[id] = true
			index := 0
			if _, err := fmt.Sscanf(id, "record-%d", &index); err != nil {
				t.Fatal(err)
			}
			if item["comment"] != records[index+1]["comment"] {
				t.Fatalf("changed readable content %s", id)
			}
		}
		if !p.HasMore {
			break
		}
		if p.NextOffset <= offset {
			t.Fatal("cursor did not advance")
		}
		offset = p.NextOffset
	}
	if len(seen) != 129 || unavailable != 1 {
		t.Fatalf("missing records: %d readable %d unavailable", len(seen), unavailable)
	}
}
