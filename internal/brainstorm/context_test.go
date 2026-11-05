package brainstorm

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
)

func TestScanContextFilesAndDigestStayBounded(t *testing.T) {
	at := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	var entries []store.BrainstormContextEntry
	kinds := []string{KindIdea, KindDecision, KindOpenQuestion, KindRelatedIssue}
	for i := 1; i <= 300; i++ {
		e := store.BrainstormContextEntry{Seq: int64(i), Kind: kinds[i%len(kinds)], Text: fmt.Sprintf("entry %d %s", i, strings.Repeat("\"x<", 300)), CreatedAt: at}
		if e.Kind == KindRelatedIssue {
			e.IssueReference = fmt.Sprintf("acme/app#%d", i)
		}
		entries = append(entries, e)
	}
	document := strings.Repeat("spec line\n", MaxDocumentBytes/10)
	entries = append(entries, store.BrainstormContextEntry{Seq: 301, Kind: KindDocumentNote, DocumentName: "spec.pdf", Text: document, CreatedAt: at})
	entries = append(entries, store.BrainstormContextEntry{Seq: 302, Kind: KindIdea, Text: "newest idea", CreatedAt: at})
	earlier := []store.BrainstormScanRun{{ID: "scan-1", ScanSeq: 1, ExchangeSeq: 5, Question: "how?", Answer: "like this", BasisCommit: "abc", Research: `{"request":"how?","findings":[{"id":"F1","text":"found","evidence":[{"path":"a.go","line":3}]}],"assumptions":[{"id":"A1","text":"assumed"}]}`}}

	digest, files, err := buildScanContext("b1", "github:acme/app", entries, earlier)
	if err != nil {
		t.Fatal(err)
	}
	if len(digest) > MaxDigestBytes {
		t.Fatalf("digest = %d bytes", len(digest))
	}
	for name, raw := range files {
		if len(raw) > MaxContextFileBytes || !json.Valid(raw) {
			t.Fatalf("%s = %d bytes valid=%v", name, len(raw), json.Valid(raw))
		}
	}
	var idx contextIndex
	if err = json.Unmarshal(files["index.json"], &idx); err != nil {
		t.Fatal(err)
	}
	if idx.SeqRange == nil || idx.SeqRange.First != 1 || idx.SeqRange.Last != 302 || idx.Counts[KindDocumentNote] != 1 || len(idx.ExchangeFiles) != 1 || idx.ExchangeCount != 1 {
		t.Fatalf("index = %+v", idx)
	}
	if len(idx.Documents) != 1 || idx.Documents[0].Name != "spec.pdf" || len(idx.RelatedIssues) != 75 {
		t.Fatalf("index names = %+v %+v", idx.Documents, len(idx.RelatedIssues))
	}
	// Every entry is reachable in seq order, and the split document reassembles.
	var seqs []int64
	var rebuilt strings.Builder
	for _, ref := range idx.EntryFiles {
		var records []contextRecord
		if err = json.Unmarshal(files[strings.TrimPrefix(ref.File, ContextDir+"/")], &records); err != nil {
			t.Fatal(err)
		}
		for _, r := range records {
			if len(seqs) == 0 || seqs[len(seqs)-1] != r.Seq {
				seqs = append(seqs, r.Seq)
			}
			if r.Seq == 301 {
				rebuilt.WriteString(r.Text)
			}
		}
	}
	if len(seqs) != 302 || seqs[0] != 1 || seqs[301] != 302 {
		t.Fatalf("seqs = %d", len(seqs))
	}
	for i := range seqs {
		if seqs[i] != int64(i+1) {
			t.Fatalf("order broken at %d", i)
		}
	}
	if rebuilt.String() != document {
		t.Fatalf("document parts do not reassemble")
	}
	var d scanDigest
	if err = json.Unmarshal(digest, &d); err != nil {
		t.Fatal(err)
	}
	if len(d.Newest) == 0 || d.Newest[len(d.Newest)-1].Seq != 302 || d.OlderEntries+len(d.Newest) != len(entries) || d.Index != ContextDir+"/index.json" {
		t.Fatalf("digest newest = %d older = %d", len(d.Newest), d.OlderEntries)
	}
	var exchange []contextRecord
	if err = json.Unmarshal(files["exchanges-001.json"], &exchange); err != nil || len(exchange) != 3 || exchange[0].Question != "how?" || exchange[1].Kind != "finding" || len(exchange[1].Evidence) == 0 {
		t.Fatalf("exchanges = %+v %v", exchange, err)
	}
}

func TestScanContextEmptyBrainstorm(t *testing.T) {
	digest, files, err := buildScanContext("b1", "github:acme/app", nil, nil)
	if err != nil || len(files) != 1 || files["index.json"] == nil || !json.Valid(digest) {
		t.Fatalf("empty = %s %v %v", digest, files, err)
	}
}
