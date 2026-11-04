package brainstorm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/store"
)

// ContextDir is where scan context files live inside the temporary workspace.
const ContextDir = "context/brainstorm"

// contextRecord is one item of a paged context file. A text longer than one
// file is split into numbered parts so every file stays within its bound.
type contextRecord struct {
	Seq            int64           `json:"seq,omitempty"`
	Kind           string          `json:"kind"`
	ScanID         string          `json:"scan,omitempty"`
	ScanSeq        int64           `json:"scan_seq,omitempty"`
	ID             string          `json:"id,omitempty"`
	DocumentName   string          `json:"document_name,omitempty"`
	IssueReference string          `json:"issue_reference,omitempty"`
	Question       string          `json:"question,omitempty"`
	Commit         string          `json:"commit,omitempty"`
	Evidence       json.RawMessage `json:"evidence,omitempty"`
	Text           string          `json:"text"`
	Part           int             `json:"part,omitempty"`
	Parts          int             `json:"parts,omitempty"`
	CreatedAt      time.Time       `json:"created_at,omitzero"`
}

type seqRange struct {
	First int64 `json:"first"`
	Last  int64 `json:"last"`
}

type contextFileRef struct {
	File  string `json:"file"`
	First int64  `json:"first"`
	Last  int64  `json:"last"`
}

type namedSeq struct {
	Name string `json:"name"`
	Seq  int64  `json:"seq"`
}

// contextIndex describes the whole brainstorm and where each part is written.
type contextIndex struct {
	BrainstormID   string               `json:"brainstorm"`
	RepositoryRef  string               `json:"repository"`
	Counts         map[string]int64     `json:"counts"`
	SeqRange       *seqRange            `json:"seq_range,omitempty"`
	KindRanges     map[string]*seqRange `json:"kind_ranges"`
	Documents      []namedSeq           `json:"documents,omitempty"`
	RelatedIssues  []namedSeq           `json:"related_issues,omitempty"`
	NameFiles      []contextFileRef     `json:"name_files,omitempty"`
	EntryFiles     []contextFileRef     `json:"entry_files"`
	ExchangeFiles  []contextFileRef     `json:"exchange_files"`
	ExchangeCount  int64                `json:"exchanges_with_findings"`
	ContextDirName string               `json:"directory"`
}

// scanDigest is the bounded prompt fact block.
type scanDigest struct {
	Index         string               `json:"index_file"`
	Counts        map[string]int64     `json:"counts"`
	SeqRange      *seqRange            `json:"seq_range,omitempty"`
	KindRanges    map[string]*seqRange `json:"kind_ranges"`
	Documents     []namedSeq           `json:"documents,omitempty"`
	RelatedIssues []namedSeq           `json:"related_issues,omitempty"`
	ListsOmitted  bool                 `json:"lists_in_index_only,omitempty"`
	OlderEntries  int                  `json:"older_entries_in_files"`
	Newest        []contextRecord      `json:"newest_entries"`
}

// buildScanContext returns the bounded digest and the paged context files for
// one scan. Entries are in seq order; earlier exchanges in scan seq order.
func buildScanContext(brainstormID, repositoryRef string, entries []store.BrainstormContextEntry, earlier []store.BrainstormScanRun) (json.RawMessage, map[string][]byte, error) {
	files := map[string][]byte{}
	idx := contextIndex{BrainstormID: brainstormID, RepositoryRef: repositoryRef, Counts: map[string]int64{}, KindRanges: map[string]*seqRange{}, EntryFiles: []contextFileRef{}, ExchangeFiles: []contextFileRef{}, ContextDirName: ContextDir}
	var records []contextRecord
	for _, e := range entries {
		idx.Counts[e.Kind]++
		if idx.SeqRange == nil {
			idx.SeqRange = &seqRange{First: e.Seq}
		}
		idx.SeqRange.Last = e.Seq
		if r := idx.KindRanges[e.Kind]; r == nil {
			idx.KindRanges[e.Kind] = &seqRange{First: e.Seq, Last: e.Seq}
		} else {
			r.Last = e.Seq
		}
		if e.DocumentName != "" {
			idx.Documents = append(idx.Documents, namedSeq{e.DocumentName, e.Seq})
		}
		if e.IssueReference != "" {
			idx.RelatedIssues = append(idx.RelatedIssues, namedSeq{e.IssueReference, e.Seq})
		}
		records = append(records, contextRecord{Seq: e.Seq, Kind: e.Kind, ScanID: e.ScanID, DocumentName: e.DocumentName, IssueReference: e.IssueReference, Text: e.Text, CreatedAt: e.CreatedAt})
	}
	refs, err := packRecords(files, "entries", records, func(r contextRecord) int64 { return r.Seq })
	if err != nil {
		return nil, nil, err
	}
	idx.EntryFiles = refs
	var exchanges []contextRecord
	for _, s := range earlier {
		idx.ExchangeCount++
		exchanges = append(exchanges, contextRecord{Kind: KindExchange, ScanID: s.ID, ScanSeq: s.ScanSeq, Seq: s.ExchangeSeq, Question: s.Question, Commit: s.BasisCommit, Text: s.Answer})
		var research agentresult.Research
		if s.Research != "" {
			if err = json.Unmarshal([]byte(s.Research), &research); err != nil {
				return nil, nil, err
			}
		}
		for _, f := range research.Findings {
			evidence, e := json.Marshal(f.Evidence)
			if e != nil {
				return nil, nil, e
			}
			if len(f.Evidence) == 0 {
				evidence = nil
			}
			exchanges = append(exchanges, contextRecord{Kind: "finding", ScanID: s.ID, ScanSeq: s.ScanSeq, ID: f.ID, Evidence: evidence, Text: f.Text})
		}
		for _, d := range research.Decisions {
			exchanges = append(exchanges, contextRecord{Kind: "decision", ScanID: s.ID, ScanSeq: s.ScanSeq, ID: d.ID, Text: d.Text})
		}
		for _, a := range research.Assumptions {
			exchanges = append(exchanges, contextRecord{Kind: "assumption", ScanID: s.ID, ScanSeq: s.ScanSeq, ID: a.ID, Text: a.Text})
		}
	}
	if idx.ExchangeFiles, err = packRecords(files, "exchanges", exchanges, func(r contextRecord) int64 { return r.ScanSeq }); err != nil {
		return nil, nil, err
	}
	raw, err := marshalCompact(idx)
	if err != nil {
		return nil, nil, err
	}
	if len(raw) > MaxContextFileBytes {
		// Move the name lists into their own paged files to keep the index bounded.
		var names []contextRecord
		for _, d := range idx.Documents {
			names = append(names, contextRecord{Seq: d.Seq, Kind: KindDocumentNote, DocumentName: d.Name})
		}
		for _, d := range idx.RelatedIssues {
			names = append(names, contextRecord{Seq: d.Seq, Kind: KindRelatedIssue, IssueReference: d.Name})
		}
		idx.Documents, idx.RelatedIssues = nil, nil
		if idx.NameFiles, err = packRecords(files, "names", names, func(r contextRecord) int64 { return r.Seq }); err != nil {
			return nil, nil, err
		}
		if raw, err = marshalCompact(idx); err != nil {
			return nil, nil, err
		}
		if len(raw) > MaxContextFileBytes {
			return nil, nil, fmt.Errorf("brainstorm context index exceeds %d bytes", MaxContextFileBytes)
		}
	}
	files["index.json"] = raw
	digest, err := buildDigest(idx, records)
	return digest, files, err
}

// buildDigest keeps the index summary and the newest contiguous entries within MaxDigestBytes.
func buildDigest(idx contextIndex, records []contextRecord) (json.RawMessage, error) {
	d := scanDigest{Index: ContextDir + "/index.json", Counts: idx.Counts, SeqRange: idx.SeqRange, KindRanges: idx.KindRanges, Documents: idx.Documents, RelatedIssues: idx.RelatedIssues, Newest: []contextRecord{}}
	raw, err := marshalCompact(d)
	if err != nil {
		return nil, err
	}
	if len(raw) > MaxDigestBytes/2 || idx.NameFiles != nil {
		d.Documents, d.RelatedIssues, d.ListsOmitted = nil, nil, true
		if raw, err = marshalCompact(d); err != nil {
			return nil, err
		}
	}
	size := len(raw)
	start := len(records)
	for start > 0 {
		r := records[start-1]
		one, e := marshalCompact(r)
		if e != nil {
			return nil, e
		}
		if size+len(one)+1 > MaxDigestBytes {
			break
		}
		size += len(one) + 1
		start--
	}
	d.Newest = append(d.Newest, records[start:]...)
	d.OlderEntries = start
	return marshalCompact(d)
}

// packRecords writes records into <prefix>-NNN.json arrays of at most
// MaxContextFileBytes each, in order, and returns the file references.
func packRecords(files map[string][]byte, prefix string, records []contextRecord, key func(contextRecord) int64) ([]contextFileRef, error) {
	refs := []contextFileRef{}
	var current [][]byte
	var first, last int64
	size := 2
	flush := func() {
		if len(current) == 0 {
			return
		}
		name := fmt.Sprintf("%s-%03d.json", prefix, len(refs)+1)
		files[name] = append(append([]byte("["), bytes.Join(current, []byte(","))...), ']')
		refs = append(refs, contextFileRef{File: ContextDir + "/" + name, First: first, Last: last})
		current, size = nil, 2
	}
	for _, r := range records {
		parts, err := splitRecord(r)
		if err != nil {
			return nil, err
		}
		for _, raw := range parts {
			if size+len(raw)+1 > MaxContextFileBytes {
				flush()
			}
			if len(current) == 0 {
				first = key(r)
			}
			current = append(current, raw)
			size += len(raw) + 1
			last = key(r)
		}
	}
	flush()
	return refs, nil
}

// splitRecord encodes one record, splitting its text into parts that each fit one file.
func splitRecord(r contextRecord) ([][]byte, error) {
	raw, err := marshalCompact(r)
	if err != nil {
		return nil, err
	}
	if len(raw)+2 <= MaxContextFileBytes {
		return [][]byte{raw}, nil
	}
	text := r.Text
	r.Text = ""
	r.Part, r.Parts = 9999, 9999
	overhead, err := marshalCompact(r)
	if err != nil {
		return nil, err
	}
	budget := MaxContextFileBytes - len(overhead) - 8
	if budget < 1024 {
		return nil, fmt.Errorf("brainstorm context record %d exceeds file bound", r.Seq)
	}
	var chunks []string
	for len(text) > 0 {
		n, used := 0, 0
		for n < len(text) {
			c, width := utf8.DecodeRuneInString(text[n:])
			cost := escapedLen(c, width)
			if used+cost > budget {
				break
			}
			used += cost
			n += width
		}
		chunks = append(chunks, text[:n])
		text = text[n:]
	}
	out := make([][]byte, 0, len(chunks))
	for i, c := range chunks {
		r.Text, r.Part, r.Parts = c, i+1, len(chunks)
		if raw, err = marshalCompact(r); err != nil {
			return nil, err
		}
		out = append(out, raw)
	}
	return out, nil
}

func escapedLen(c rune, width int) int {
	switch {
	case c == '"' || c == '\\' || c == '\n' || c == '\r' || c == '\t':
		return 2
	case c < 0x20 || c == 0x2028 || c == 0x2029 || c == utf8.RuneError:
		return 6
	}
	return width
}

func marshalCompact(v any) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(b.Bytes(), "\n"), nil
}
