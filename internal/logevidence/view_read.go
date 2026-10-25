package logevidence

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"strings"

	"github.com/cloudvane/litefactory/internal/logreader"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/retainedevidence"
	"github.com/cloudvane/litefactory/internal/store"
)

// ViewLocator is a versioned position, never an access grant. Raw positions
// describe retained JSONL; ViewOffset describes completely sanitized UTF-8.
// A new authorized pass rebuilds and verifies the same view before disclosure.
type ViewLocator struct {
	CacheID           string `json:"cache_id,omitempty"`
	SourceRevision    string `json:"source_revision,omitempty"`
	Version           int    `json:"v"`
	RunID             int64  `json:"run_id"`
	Start             int64  `json:"start"`
	End               int64  `json:"end,omitempty"`
	Fingerprint       string `json:"fingerprint"`
	SourceDigest      string `json:"source_digest,omitempty"`
	Policy            string `json:"policy"`
	ViewDigest        string `json:"view_digest,omitempty"`
	ViewOffset        int    `json:"view_offset"`
	PreparationOffset int64  `json:"preparation_offset,omitempty"`
}

type recordPreparation struct {
	file                                                               *os.File
	start, offset, end, verified, scannedEnd                           int64
	fingerprint, sourceDigest, sourceRevision, cacheID, kind, omission string
	sourceHash, verifyHash                                             hash.Hash
	complete, ready                                                    bool
	view                                                               redact.View
}

func (p *recordPreparation) close() {
	if p.file != nil {
		name := p.file.Name()
		_ = p.file.Close()
		_ = os.Remove(name)
		p.file = nil
	}
}

func newPreparation(start int64, fp string) (*recordPreparation, error) {
	file, err := os.CreateTemp("", "litefactory-evidence-private-*")
	if err != nil {
		return nil, err
	}
	return &recordPreparation{file: file, start: start, offset: start, verified: start, fingerprint: fp, sourceHash: sha256.New(), verifyHash: sha256.New()}, nil
}

// advance copies bounded source work to private storage, then verifies a second
// digest from the opened source. No incomplete normalized/redacted view escapes.
func (p *recordPreparation) advance(ctx context.Context, source io.ReaderAt, redactor *redact.Redactor, allowance int) (int, error) {
	scanned := 0
	if !p.complete {
		progress, err := logreader.Assemble(ctx, source, io.MultiWriter(p.file, p.sourceHash), p.offset, allowance)
		p.scannedEnd = max(p.scannedEnd, p.offset+progress.Scanned)
		p.offset = progress.Offset
		scanned = int(progress.Scanned)
		if err != nil {
			return scanned, err
		}
		if !progress.Complete && !progress.EOF {
			return scanned, nil
		}
		p.complete = true
		p.end = p.offset
		p.sourceDigest = hex.EncodeToString(p.sourceHash.Sum(nil))
		if progress.EOF {
			p.omission = "incomplete"
		}
	}
	var buffer [4096]byte
	for p.verified < p.end && scanned < allowance {
		if err := ctx.Err(); err != nil {
			return scanned, err
		}
		amount := min(int64(len(buffer)), p.end-p.verified, int64(allowance-scanned))
		n, err := source.ReadAt(buffer[:amount], p.verified)
		scanned += n
		p.verified += int64(n)
		p.scannedEnd = max(p.scannedEnd, p.verified)
		_, _ = p.verifyHash.Write(buffer[:n])
		if err != nil && (!errors.Is(err, io.EOF) || p.verified != p.end) {
			return scanned, err
		}
		if n == 0 {
			return scanned, io.ErrNoProgress
		}
	}
	if p.verified < p.end {
		return scanned, nil
	}
	if hex.EncodeToString(p.verifyHash.Sum(nil)) != p.sourceDigest {
		return scanned, errors.New("source_changed")
	}
	if p.omission != "" {
		p.ready = true
		p.close()
		return scanned, nil
	}
	if _, err := p.file.Seek(0, io.SeekStart); err != nil {
		return scanned, err
	}
	raw, err := io.ReadAll(p.file)
	if err != nil {
		return scanned, err
	}
	text, kind, err := NormalizeRecord(raw)
	if err != nil {
		p.omission = "unsupported"
		if errors.Is(err, ErrMalformedRecord) {
			p.omission = "malformed"
		}
	} else {
		p.kind = kind
		p.view, err = redactor.Prepare(ctx, text)
		if err != nil {
			return scanned, errors.New("redaction_failure")
		}
	}
	p.ready = true
	p.close()
	return scanned, nil
}

func (s *Service) locator(run int64, p *recordPreparation, offset int) *ViewLocator {
	policy := s.redactor.PolicyID()
	if p.view.Valid() {
		policy = p.view.PolicyID()
	}
	return &ViewLocator{CacheID: p.cacheID, SourceRevision: p.sourceRevision, Version: 2, RunID: run, Start: p.start, End: p.end, Fingerprint: p.fingerprint, SourceDigest: p.sourceDigest, Policy: policy, ViewDigest: p.view.Digest(), ViewOffset: offset, PreparationOffset: p.offset}
}

func (s *Service) read(ctx context.Context, r ReadRequest) Result {
	if r.Citation != nil && r.Citation.Version == 1 {
		if r.Locator != nil {
			return failureResult("invalid_request")
		}
		return s.readLegacy(ctx, r)
	}
	selectors := 0
	if r.Cursor != "" {
		selectors++
	}
	if r.Citation != nil {
		selectors++
	}
	if r.Locator != nil {
		selectors++
	}
	if r.Offset != 0 {
		selectors++
	}
	if selectors > 1 {
		return failureResult("invalid_request")
	}
	row, err := s.db.GetPriorRun(ctx, s.query(), r.RunID)
	if err != nil {
		return failureResult("unavailable")
	}
	source, status := s.openSource(row)
	if status != "" {
		if status == "unavailable" {
			return sourceUnavailable()
		}
		return failureResult(status)
	}
	defer source.close()
	fp := fileFingerprint(source.info)
	var wanted *ViewLocator
	if r.Cursor != "" {
		c, ok := s.unsign(r.Cursor)
		if !ok || c.Kind != "view" || c.RunID != r.RunID {
			return failureResult("expired_cursor")
		}
		wanted = &ViewLocator{Version: 2, RunID: r.RunID, Start: c.Position.Offset, Fingerprint: c.Fingerprint, Policy: c.Filter, ViewOffset: c.ViewOffset}
		if c.View != nil {
			wanted = c.View
		}
		if c.View == nil && c.ViewOffset > 0 {
			key := digestJSON([]any{r.RunID, c.Position.Offset, c.Fingerprint, c.Filter})
			p := s.budget.preparations[key]
			if p == nil || !p.ready {
				return failureResult("expired_cursor")
			}
			wanted = s.locator(r.RunID, p, c.ViewOffset)
		}
	}
	if r.Locator != nil {
		position := *r.Locator
		wanted = &position
	}
	if r.Citation != nil {
		c := r.Citation
		if !validViewDigest(c.SourceRevision) || !validViewDigest(c.SourceDigest) || !validViewDigest(c.ViewDigest) {
			return failureResult("invalid_request")
		}
		if c.Version != 2 || c.RunID != row.ID || c.TaskRef != row.TaskRef || c.Ordinal != row.Ordinal || !c.StartedAt.Equal(row.StartedAt) || c.ViewStart < 0 || c.ViewEnd < c.ViewStart || c.ViewEnd > c.ViewBytes {
			return failureResult("invalid_request")
		}
		wanted = &ViewLocator{SourceRevision: c.SourceRevision, Version: 2, RunID: r.RunID, Start: c.Start, End: c.End, SourceDigest: c.SourceDigest, Policy: c.Policy, ViewDigest: c.ViewDigest, ViewOffset: c.ViewStart, Fingerprint: fp}
	}
	if wanted != nil {
		if !validViewLocator(*wanted) {
			return failureResult("invalid_request")
		}
		if wanted.Policy != s.redactor.PolicyID() {
			return failureResult("policy_changed")
		}
		if wanted.Version != 2 || wanted.RunID != r.RunID || wanted.Start < 0 || wanted.End < 0 || wanted.ViewOffset < 0 || wanted.PreparationOffset < 0 {
			return failureResult("invalid_request")
		}
		if wanted.Fingerprint != fp {
			return failureResult("source_changed")
		}
		if !s.resume(wanted) {
			return failureResult("invalid_request")
		}
		wanted.CacheID = ""
	}
	start := r.Offset
	viewOffset := 0
	if wanted != nil {
		start = wanted.Start
		viewOffset = wanted.ViewOffset
	}
	if start < 0 || start > source.info.Size() {
		return failureResult("invalid_request")
	}
	if start > 0 {
		var b [1]byte
		if _, err := source.file.ReadAt(b[:], start-1); err != nil || b[0] != '\n' {
			return failureResult("source_changed")
		}
	}
	if r.Limit == 0 {
		r.Limit = 10
	}
	out := Result{Status: "ok", Policy: PolicyVersion, PageLimit: r.Limit, ScanLimit: scanMaximum, Coverage: Coverage{Start: start, End: start, ScannedEnd: start, RequestedEnd: source.info.Size(), Fingerprint: fp, RetentionOnly: true, Incomplete: true}}
	allowance := scanMaximum
	snapshotKey := digestJSON([]any{r.RunID, fp})
	snapshot := s.budget.snapshots[snapshotKey]
	if snapshot == nil {
		snapshot, err = newSnapshot(source.info.Size())
		if err != nil {
			return failureResult("unavailable")
		}
		snapshot.run = r.RunID
		snapshot.fingerprint = fp
		snapshot.policy = s.redactor.PolicyID()
		s.budget.snapshots[snapshotKey] = snapshot
	}
	if !snapshot.ready {
		n, e := snapshot.advance(ctx, source.file, allowance)
		allowance -= n
		out.Coverage.ScannedEnd = max(snapshot.copied, snapshot.verified)
		if e != nil {
			snapshot.close()
			delete(s.budget.snapshots, snapshotKey)
			if e.Error() == "source_changed" {
				return failureResult("source_changed")
			}
			return failureResult("unavailable")
		}
	}
	if !snapshot.ready {
		out.Status = "preparing"
		out.Remaining = &ViewLocator{CacheID: snapshot.cacheID, Version: 2, RunID: r.RunID, Start: start, Fingerprint: fp, Policy: s.redactor.PolicyID(), ViewOffset: viewOffset, PreparationOffset: snapshot.copied}
		if wanted != nil {
			position := *wanted
			out.Remaining = &position
			out.Remaining.PreparationOffset = snapshot.copied
			out.Remaining.CacheID = snapshot.cacheID
		}
		out.NextCursor = s.sign(cursor{View: wanted, Kind: "view", RunID: r.RunID, Fingerprint: fp, Filter: s.redactor.PolicyID(), Position: logreader.Position{Offset: start}, ViewOffset: viewOffset})
		return s.stableResult(source, fp, out)
	}
	if wanted != nil && wanted.SourceRevision != "" && wanted.SourceRevision != snapshot.digest {
		return failureResult("source_changed")
	}

	for len(out.Records) < min(r.Limit, s.budget.limits.Records-s.budget.used.Records) && len(out.Coverage.Omissions) < 25 {
		if start == source.info.Size() {
			out.Coverage.EndOfSource = true
			out.Remaining = nil
			break
		}
		key := digestJSON([]any{r.RunID, start, fp, s.redactor.PolicyID()})
		p := s.budget.preparations[key]
		if p != nil && p.sourceRevision != snapshot.digest {
			p.close()
			delete(s.budget.preparations, key)
			p = nil
		}
		if p == nil && allowance <= 0 {
			out.Remaining = &ViewLocator{CacheID: snapshot.cacheID, SourceRevision: snapshot.digest, Version: 2, RunID: r.RunID, Start: start, Fingerprint: fp, Policy: s.redactor.PolicyID()}
			break
		}
		if p == nil {
			p, err = newPreparation(start, fp)
			if err != nil {
				return failureResult("unavailable")
			}
			p.sourceRevision = snapshot.digest
			p.cacheID = snapshot.cacheID
			s.budget.preparations[key] = p
		}
		if p.view.Valid() && p.view.PolicyID() != s.redactor.PolicyID() {
			return failureResult("policy_changed")
		}
		if !p.ready && allowance <= 0 {
			out.Remaining = s.locator(r.RunID, p, viewOffset)
			break
		}
		if !p.ready {
			n, advanceErr := p.advance(ctx, snapshot.file, s.redactor, allowance)
			allowance -= n
			out.Coverage.ScannedEnd = max(out.Coverage.ScannedEnd, min(source.info.Size(), p.scannedEnd))
			if advanceErr != nil {
				p.close()
				delete(s.budget.preparations, key)
				status := "unavailable"
				if advanceErr.Error() == "source_changed" || advanceErr.Error() == "redaction_failure" {
					status = advanceErr.Error()
				}
				return failureResult(status)
			}
		}
		out.Remaining = s.locator(r.RunID, p, viewOffset)
		if !p.ready {
			out.Status = "preparing"
			break
		}
		if wanted != nil && (wanted.SourceDigest != "" && wanted.SourceDigest != p.sourceDigest || wanted.End != 0 && wanted.End != p.end || wanted.ViewDigest != "" && wanted.ViewDigest != p.view.Digest()) {
			return failureResult("source_changed")
		}
		if p.omission != "" {
			out.Coverage.Omissions = append(out.Coverage.Omissions, Omission{Start: p.start, End: p.end, Reason: p.omission})
			start = p.end
			viewOffset = 0
			wanted = nil
			out.Coverage.End = start
			out.Remaining = &ViewLocator{CacheID: snapshot.cacheID, SourceRevision: snapshot.digest, Version: 2, RunID: r.RunID, Start: start, Fingerprint: fp, Policy: s.redactor.PolicyID()}
			continue
		}
		if viewOffset > len(p.view.String()) || viewOffset < len(p.view.String()) && p.view.String()[viewOffset]&0xc0 == 0x80 {
			return failureResult("invalid_request")
		}
		end := len(p.view.String())
		if end-viewOffset > 1400 {
			end = viewOffset + len(truncateUTF8(p.view.String()[viewOffset:], 1400))
		}
		if r.Citation != nil {
			end = r.Citation.ViewEnd
			if end > len(p.view.String()) || end < len(p.view.String()) && p.view.String()[end]&0xc0 == 0x80 || r.Citation.ExcerptBytes != end-viewOffset || end-viewOffset > pageMaximum {
				return failureResult("source_changed")
			}
		}
		task, taskErr := s.redactor.Prepare(ctx, row.TaskRef)
		if taskErr != nil || task.String() != row.TaskRef {
			return failureResult("redaction_failure")
		}
		record := s.viewRecord(row, p, viewOffset, end)
		if r.Citation == nil {
			out.NextCursor = s.sign(cursor{Kind: "view", RunID: r.RunID, Fingerprint: fp, Filter: p.view.PolicyID(), Position: logreader.Position{Offset: p.start}, ViewOffset: end})
		}
		out.Records = append(out.Records, record)
		// Reserve enough for continuation, counters and both transport encodings.
		for encodedSize(out, strings.Repeat("x", 128)) > pageMaximum-1800 && end > viewOffset {
			end = viewOffset + len(truncateUTF8(p.view.String()[viewOffset:end], (end-viewOffset)/2))
			out.Records[len(out.Records)-1] = s.viewRecord(row, p, viewOffset, end)
		}
		if end == viewOffset && viewOffset < len(p.view.String()) {
			out.Records = out.Records[:len(out.Records)-1]
			if len(out.Records) == 0 {
				out.Status = "page_limited"
			}
			break
		}
		viewOffset = end
		if r.Citation != nil {
			if !citationsEqual(out.Records[len(out.Records)-1].Citation, *r.Citation) {
				return failureResult("source_changed")
			}
			out.Remaining = nil
			out.Coverage.End = p.end
			out.Coverage.EndOfSource = p.end == source.info.Size()
			break
		}
		if viewOffset < len(p.view.String()) {
			out.Remaining = s.locator(r.RunID, p, viewOffset)
			break
		}
		start = p.end
		viewOffset = 0
		wanted = nil
		out.Coverage.End = start
		out.Remaining = &ViewLocator{CacheID: snapshot.cacheID, SourceRevision: snapshot.digest, Version: 2, RunID: r.RunID, Start: start, Fingerprint: fp, Policy: s.redactor.PolicyID()}
		if start == source.info.Size() {
			out.Coverage.EndOfSource = true
			out.Remaining = nil
			break
		}
		// A record page is complete before the following record is prepared.
		if encodedSize(out, strings.Repeat("x", 128)) > pageMaximum-2800 || allowance <= 0 {
			break
		}
	}

	out.NextCursor = ""
	if out.Remaining != nil && r.Citation == nil && r.Offset == 0 {
		out.NextCursor = s.sign(cursor{Kind: "view", RunID: r.RunID, Fingerprint: fp, Filter: out.Remaining.Policy, Position: logreader.Position{Offset: out.Remaining.Start}, ViewOffset: out.Remaining.ViewOffset})
	}
	return s.stableResult(source, fp, out)
}

func (s *Service) stableResult(source runSource, fp string, out Result) Result {
	after, e := source.file.Stat()
	current, e2 := source.root.Stat(source.rel)
	if e != nil || e2 != nil || !os.SameFile(source.info, current) || fileFingerprint(after) != fp || fileFingerprint(current) != fp {
		return failureResult("source_changed")
	}
	return out
}

func (s *Service) viewRecord(row store.PriorRun, p *recordPreparation, start, end int) Record {
	text := p.view.String()[start:end]
	citation := Citation{SourceRevision: p.sourceRevision, Version: 2, TaskRef: row.TaskRef, RunID: row.ID, Ordinal: row.Ordinal, StartedAt: row.StartedAt, Stream: p.kind, Start: p.start, End: p.end, Digest: digestText(text), Policy: p.view.PolicyID(), ExcerptBytes: len(text), SourceDigest: p.sourceDigest, ViewDigest: p.view.Digest(), ViewStart: start, ViewEnd: end, ViewBytes: len(p.view.String())}
	return Record{Source: &retainedevidence.Entry{Kind: "run-record", OwnerID: fmt.Sprintf("%d:%d:%d", row.ID, p.start, p.end), Revision: p.sourceRevision, Digest: p.sourceDigest, Bytes: p.end - p.start, Capture: retainedevidence.Unknown, Availability: retainedevidence.Available, Channel: p.kind}, ViewCoverage: retainedevidence.Complete, Text: text, Citation: citation, Redactions: p.view.Counts().Total(), Truncated: end < len(p.view.String())}
}

func validViewLocator(locator ViewLocator) bool {
	if len(locator.CacheID) > 128 || !validViewDigest(locator.Fingerprint) || !validViewDigest(locator.Policy) {
		return false
	}
	for _, value := range []string{locator.SourceRevision, locator.SourceDigest, locator.ViewDigest} {
		if value != "" && !validViewDigest(value) {
			return false
		}
	}
	if locator.ViewOffset > 0 && (locator.SourceRevision == "" || locator.SourceDigest == "" || locator.ViewDigest == "" || locator.End <= locator.Start) {
		return false
	}
	return true
}
func validViewDigest(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size && len(value) == sha256.Size*2
}
