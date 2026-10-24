package logevidence

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/cloudvane/litefactory/internal/logreader"
	"github.com/cloudvane/litefactory/internal/store"
)

type cursor struct {
	ViewOffset  int                `json:"vo,omitempty"`
	View        *ViewLocator       `json:"view,omitempty"`
	Version     int                `json:"v"`
	Kind        string             `json:"k"`
	Scope       string             `json:"s"`
	RunID       int64              `json:"r,omitempty"`
	Fingerprint string             `json:"f,omitempty"`
	Position    logreader.Position `json:"p"`
	BeforeID    int64              `json:"b,omitempty"`
	Filter      string             `json:"q,omitempty"`
}

func digestText(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
func digestJSON(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func (s *Service) sign(c cursor) string {
	c.Version = 1
	c.Scope = s.scopeDigest
	b, _ := json.Marshal(c)
	m := hmac.New(sha256.New, s.key[:])
	m.Write(b)
	return base64.RawURLEncoding.EncodeToString(b) + "." + base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}
func (s *Service) unsign(token string) (cursor, bool) {
	var c cursor
	left, right, ok := strings.Cut(token, ".")
	if !ok {
		return c, false
	}
	b, e := base64.RawURLEncoding.DecodeString(left)
	sig, e2 := base64.RawURLEncoding.DecodeString(right)
	if e != nil || e2 != nil {
		return c, false
	}
	m := hmac.New(sha256.New, s.key[:])
	m.Write(b)
	if !hmac.Equal(sig, m.Sum(nil)) || json.Unmarshal(b, &c) != nil || c.Version != 1 || c.Scope != s.scopeDigest {
		return c, false
	}
	return c, true
}
func (s *Service) query() store.PriorRunQuery {
	return store.PriorRunQuery{Scope: s.scope, Cutoff: s.cutoff, HighWater: s.highWater, StartedAfter: s.startedAfter, StartedBefore: s.startedBefore}
}
func (s *Service) list(ctx context.Context, r ListRequest) Result {
	if r.Limit == 0 {
		r.Limit = 10
	}
	if r.Status != "" && r.Status != store.RunSucceeded && r.Status != store.RunFailed && r.Status != store.RunReported {
		return failureResult("invalid_request")
	}
	if !r.StartedAfter.IsZero() && !r.StartedBefore.IsZero() && r.StartedAfter.After(r.StartedBefore) {
		return failureResult("invalid_request")
	}
	q := s.query()
	q.TaskRef = r.TaskRef
	q.StartedAfter = r.StartedAfter
	q.StartedBefore = r.StartedBefore
	if !s.startedAfter.IsZero() && (q.StartedAfter.IsZero() || q.StartedAfter.Before(s.startedAfter)) {
		q.StartedAfter = s.startedAfter
	}
	if !s.startedBefore.IsZero() && (q.StartedBefore.IsZero() || q.StartedBefore.After(s.startedBefore)) {
		q.StartedBefore = s.startedBefore
	}
	q.Status = r.Status
	filter := r
	filter.Cursor = ""
	filter.Limit = 0
	fd := digestJSON(filter)
	if r.Cursor != "" {
		c, ok := s.unsign(r.Cursor)
		if !ok || c.Kind != "list" || c.Filter != fd {
			return failureResult("expired_cursor")
		}
		q.BeforeID = c.BeforeID
	}
	q.Limit = min(r.Limit, s.budget.limits.Rows-s.budget.used.Rows) + 1
	rows, err := s.db.ListPriorRuns(ctx, q)
	if err != nil {
		return failureResult("unavailable")
	}
	out := Result{Status: "ok", Cutoff: s.cutoff, HighWater: s.highWater, ScopeDigest: s.scopeDigest, Policy: PolicyVersion, PageLimit: q.Limit - 1}
	for i, row := range rows {
		if i == q.Limit-1 {
			out.NextCursor = s.sign(cursor{Kind: "list", BeforeID: rows[i-1].ID, Filter: fd})
			break
		}
		values := []string{row.TaskRef, row.Mode, row.Agent}
		safe := make([]string, 3)
		redactions := 0
		truncated := false
		for j, value := range values {
			body, e := s.redactor.Apply(ctx, value, recordMaximum*4)
			if e != nil || body.Truncated() {
				return failureResult("redaction_failure")
			}
			redactions += body.Counts().Total()
			safe[j] = body.String()
			if j > 0 && len(safe[j]) > 256 {
				safe[j] = truncateUTF8(safe[j], 256)
				truncated = true
			}
		}
		next := RunMetadata{TaskRef: safe[0], RunID: row.ID, Ordinal: row.Ordinal, Mode: safe[1], Agent: safe[2], Status: row.Status, StartedAt: row.StartedAt, FinishedAt: row.FinishedAt, Redactions: redactions, Truncated: truncated}
		out.Runs = append(out.Runs, next)
		if encodedSize(out, strings.Repeat("x", 128)) > pageTarget && len(out.Runs) > 1 {
			out.Runs = out.Runs[:len(out.Runs)-1]
			out.NextCursor = s.sign(cursor{Kind: "list", BeforeID: rows[i-1].ID, Filter: fd})
			break
		}
	}
	return out
}
func fileFingerprint(info os.FileInfo) string {
	identity := ""
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		identity = fmt.Sprintf("%d:%d", st.Dev, st.Ino)
	}
	return digestJSON([]any{identity, info.Size(), info.ModTime().UnixNano(), info.Mode()})
}
func (s *Service) readLegacy(ctx context.Context, r ReadRequest) Result {
	if r.Limit == 0 {
		r.Limit = 10
	}
	if r.Cursor != "" && r.Citation != nil || r.Offset != 0 && (r.Cursor != "" || r.Citation != nil) {
		return failureResult("invalid_request")
	}
	row, err := s.db.GetPriorRun(ctx, s.query(), r.RunID)
	if err != nil {
		return failureResult("unavailable")
	}
	src, status := s.openSource(row)
	if status != "" {
		if status == "unavailable" {
			return sourceUnavailable()
		}
		return failureResult(status)
	}
	defer src.close()
	root, rel, file, pinned := src.root, src.rel, src.file, src.info
	fp := fileFingerprint(pinned)
	pos := logreader.Position{}
	if r.Cursor != "" {
		c, ok := s.unsign(r.Cursor)
		if !ok || c.Kind != "read" || c.RunID != r.RunID {
			return failureResult("expired_cursor")
		}
		if c.Fingerprint != fp {
			return failureResult("source_changed")
		}
		pos = c.Position
	}
	if r.Offset != 0 {
		// A digest offers record starts; anything else is not a record boundary.
		var boundary [1]byte
		if r.Offset < 0 || r.Offset >= pinned.Size() {
			return failureResult("invalid_request")
		}
		if _, e := file.ReadAt(boundary[:], r.Offset-1); e != nil || boundary[0] != '\n' {
			return failureResult("source_changed")
		}
		pos.Offset = r.Offset
	}
	if r.Citation != nil {
		c := r.Citation
		if c.Policy != PolicyVersion {
			result := failureResult("policy_changed")
			result.Policy = PolicyVersion
			return result
		}
		if c.Version != 1 || c.RunID != row.ID || c.TaskRef != row.TaskRef || c.Ordinal != row.Ordinal || !c.StartedAt.Equal(row.StartedAt) || c.Start < 0 || c.End <= c.Start || c.End > pinned.Size() || c.End-c.Start > recordMaximum || c.ExcerptBytes < 0 || c.ExcerptBytes > 700 {
			return failureResult("source_changed")
		}
		var boundary [1]byte
		if c.Start > 0 {
			if _, e := file.ReadAt(boundary[:], c.Start-1); e != nil || boundary[0] != '\n' {
				return failureResult("source_changed")
			}
		}
		if _, e := file.ReadAt(boundary[:], c.End-1); e != nil || boundary[0] != '\n' {
			return failureResult("source_changed")
		}
		pos.Offset = c.Start
		r.Limit = 1
	}
	out := Result{Status: "ok", Policy: PolicyVersion, Coverage: Coverage{Start: pos.Offset, Fingerprint: fp, Incomplete: true, RetentionOnly: true, RequestedEnd: pinned.Size()}, PageLimit: r.Limit, SourceRecordBytes: recordMaximum, ScanLimit: scanMaximum}
	remaining := scanMaximum
	if r.Citation != nil {
		out.Coverage.RequestedEnd = r.Citation.End
		remaining -= 2
	}
	wanted := min(r.Limit, s.budget.limits.Records-s.budget.used.Records)
	out.PageLimit = wanted
	for len(out.Records) < wanted && remaining > 0 {
		scanStart := pos.Offset
		page, e := logreader.Scan(ctx, file, pos, 1, recordMaximum, remaining)
		if e != nil {
			return failureResult("unavailable")
		}
		remaining -= int(page.Scanned)
		out.Coverage.ScannedEnd = max(out.Coverage.ScannedEnd, scanStart+page.Scanned)
		if len(page.Records) == 0 {
			out.Coverage.EndOfSource = page.EOF
			pos = page.Next
			break
		}
		rec := page.Records[0]
		reason := rec.Omission
		if reason == "" {
			text, kind, e := NormalizeRecord(rec.Data)
			if e != nil {
				reason = "unsupported"
				if errors.Is(e, ErrMalformedRecord) {
					reason = "malformed"
				}
			} else {
				// Redact the whole decoded record before applying excerpt/page bounds.
				body, e := s.redactor.Apply(ctx, text, recordMaximum*8)
				if e != nil {
					return failureResult("redaction_failure")
				}
				safe := body.String()
				truncated := body.Truncated()
				if len(safe) > 700 {
					safe = truncateUTF8(safe, 700)
					truncated = true
				}
				if r.Citation != nil {
					if r.Citation.ExcerptBytes > len(safe) {
						return failureResult("source_changed")
					}
					safe = truncateUTF8(safe, r.Citation.ExcerptBytes)
					truncated = truncated || len(safe) < len(body.String())
				}
				citation := Citation{Version: 1, TaskRef: row.TaskRef, RunID: row.ID, Ordinal: row.Ordinal, StartedAt: row.StartedAt, Stream: kind, Start: rec.Start, End: rec.End, Digest: digestText(safe), Policy: PolicyVersion, ExcerptBytes: len(safe)}
				// Task refs in citations are metadata too; withholding avoids changing selectors.
				taskBody, e := s.redactor.Apply(ctx, row.TaskRef, recordMaximum*4)
				if e != nil || taskBody.String() != row.TaskRef {
					return failureResult("redaction_failure")
				}
				record := Record{Text: safe, Citation: citation, Redactions: body.Counts().Total(), Truncated: truncated}
				out.Records = append(out.Records, record)
				if len(out.Records) == 1 && r.Citation == nil {
					for encodedSize(out, strings.Repeat("x", 128)) > pageTarget && len(out.Records[0].Text) > 32 {
						item := &out.Records[0]
						item.Text = truncateUTF8(item.Text, len(item.Text)/2)
						item.Truncated = true
						item.Citation.Digest = digestText(item.Text)
						item.Citation.ExcerptBytes = len(item.Text)
					}
				}
				if encodedSize(out, strings.Repeat("x", 128)) > pageTarget && len(out.Records) > 1 {
					out.Records = out.Records[:len(out.Records)-1]
					break
				}
				if truncated {
					out.Coverage.Incomplete = true
				}
			}
		}
		if reason != "" {
			out.Coverage.Omissions = append(out.Coverage.Omissions, Omission{rec.Start, rec.End, reason})
			out.Coverage.Incomplete = true
		}
		pos = page.Next
		if page.EOF || pos.Offset >= pinned.Size() {
			out.Coverage.EndOfSource = true
			break
		}
		if len(out.Coverage.Omissions) >= 25 {
			break
		}
	}
	out.Coverage.End = pos.Offset
	// An offset read is one window, never a path to whole-run coverage.
	if !out.Coverage.EndOfSource && r.Offset == 0 {
		out.Coverage.Incomplete = true
		out.NextCursor = s.sign(cursor{Kind: "read", RunID: r.RunID, Fingerprint: fp, Position: pos})
	}
	if r.Offset != 0 {
		out.Coverage.Incomplete = true
	}
	after, e := file.Stat()
	current, e2 := root.Stat(rel)
	if e != nil || e2 != nil || fileFingerprint(after) != fp || !os.SameFile(pinned, current) || fileFingerprint(current) != fp {
		return failureResult("source_changed")
	}
	if r.Citation != nil {
		if len(out.Records) != 1 || !citationsEqual(out.Records[0].Citation, *r.Citation) {
			return failureResult("source_changed")
		}
		out.NextCursor = ""
	}
	return out
}

// runSource is one opened, pinned retained run log.
type runSource struct {
	root *os.Root
	rel  string
	file *os.File
	info os.FileInfo
}

func (r runSource) close() { _ = r.file.Close(); _ = r.root.Close() }

// openSource opens a run's log below the runs root without following links.
// The status is "unavailable" or "source_changed" when it cannot be used.
func (s *Service) openSource(row store.PriorRun) (runSource, string) {
	path, err := logreader.ResolveRootPath(s.root, row.TaskRef, row.Run)
	if err != nil || filepath.Ext(path) != ".jsonl" {
		return runSource{}, "unavailable"
	}
	root, err := os.OpenRoot(s.root)
	if err != nil {
		return runSource{}, "unavailable"
	}
	rel, err := filepath.Rel(s.root, path)
	if err != nil || !filepath.IsLocal(rel) {
		_ = root.Close()
		return runSource{}, "unavailable"
	}
	// Lstat rejects nonregular endpoints before opening (including FIFOs).
	info, err := root.Lstat(rel)
	if err != nil || !info.Mode().IsRegular() {
		_ = root.Close()
		return runSource{}, "unavailable"
	}
	file, err := root.OpenFile(rel, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
	if err != nil {
		_ = root.Close()
		return runSource{}, "unavailable"
	}
	pinned, err := file.Stat()
	if err != nil || !pinned.Mode().IsRegular() || !os.SameFile(info, pinned) {
		_ = file.Close()
		_ = root.Close()
		return runSource{}, "source_changed"
	}
	return runSource{root, rel, file, pinned}, ""
}

func truncateUTF8(s string, n int) string {
	for n > 0 && n < len(s) && s[n]&0xc0 == 0x80 {
		n--
	}
	return s[:n]
}

func citationsEqual(a, b Citation) bool {
	if !a.StartedAt.Equal(b.StartedAt) {
		return false
	}
	a.StartedAt = b.StartedAt
	return a == b
}

func sourceUnavailable() Result {
	r := failureResult("unavailable")
	r.Coverage.Omissions = []Omission{{Reason: "source_unavailable"}}
	r.Coverage.RetentionOnly = true
	return r
}
