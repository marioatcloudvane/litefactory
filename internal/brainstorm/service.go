package brainstorm

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/cloudvane/litefactory/internal/store"
)

func mintID(prefix string) string {
	var b [24]byte
	if _, err := rand.Read(b[:]); err != nil {
		return ""
	}
	return prefix + hex.EncodeToString(b[:])
}

func requestDigest(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func validText(s string, n int) bool {
	return strings.TrimSpace(s) != "" && len(s) <= n && utf8.ValidString(s)
}

func ownerOf(p Principal) (store.BrainstormOwner, error) {
	id, err := strconv.ParseInt(p.OwnerID, 10, 64)
	if err != nil || id <= 0 || p.OwnerGeneration <= 0 {
		return store.BrainstormOwner{}, ErrForbidden
	}
	return store.BrainstormOwner{OwnerID: id, OwnerGeneration: p.OwnerGeneration}, nil
}

// storeError maps storage refusals to the package's plain refusals.
func storeError(err error) error {
	var revision store.BrainstormRevisionError
	var capErr store.BrainstormCapError
	switch {
	case err == nil:
		return nil
	case errors.As(err, &revision):
		return fmt.Errorf("%w (current revision %d)", ErrStaleRevision, revision.Current)
	case errors.As(err, &capErr):
		return fmt.Errorf("%w: %s", ErrCapReached, capErr.Cap)
	case errors.Is(err, store.ErrNotFound):
		return ErrNotFound
	case errors.Is(err, store.ErrBrainstormClosed):
		return ErrClosed
	case errors.Is(err, store.ErrWorkflowConflict):
		return ErrRequestReuse
	case errors.Is(err, store.ErrBrainstormQueueFull):
		return ErrQueueFull
	case errors.Is(err, store.ErrConnectorInvalid):
		return ErrInvalid
	}
	return err
}

func (s *Service) checkConsent(ctx context.Context, p Principal, repository string) error {
	if s.consent == nil {
		return nil
	}
	return s.consent(ctx, p, repository)
}

// brainstorm reads one owner-scoped brainstorm after lazy expiry and checks
// live consent for its repository.
func (s *Service) brainstorm(ctx context.Context, p Principal, id string) (store.BrainstormOwner, store.BrainstormRecord, error) {
	o, err := ownerOf(p)
	if err != nil {
		return o, store.BrainstormRecord{}, err
	}
	if !validText(id, 256) {
		return o, store.BrainstormRecord{}, ErrNotFound
	}
	b, err := s.db.ReadBrainstorm(ctx, o, id, s.now())
	if err != nil {
		return o, b, storeError(err)
	}
	return o, b, s.checkConsent(ctx, p, b.RepositoryRef)
}

func headerOf(b store.BrainstormRecord) Brainstorm {
	return Brainstorm{ID: b.ID, RepositoryRef: b.RepositoryRef, State: b.State, CloseReason: b.CloseReason, Revision: b.Revision, CreatedAt: b.CreatedAt, LastActivityAt: b.LastActivityAt, ClosedAt: b.ClosedAt}
}

func entryOf(e store.BrainstormEntry) Entry {
	return Entry{Seq: e.Seq, Kind: e.Kind, Text: e.Text, DocumentName: e.DocumentName, IssueReference: e.IssueReference, ScanID: e.ScanID, CreatedAt: e.CreatedAt}
}

// plainState maps the shared job state to the plain scan state and reason.
func plainState(sc store.BrainstormScan) (string, string) {
	switch sc.JobState {
	case "queued":
		return ScanWaiting, ""
	case "running":
		return ScanRunning, ""
	case "stop_requested":
		return ScanStopping, ""
	case "succeeded":
		return ScanAnswered, ""
	case "failed":
		reason := sc.FailureReason
		if reason == "" {
			reason = sc.JobSummary
		}
		if reason == "" {
			reason = "the scan failed"
		}
		return ScanFailed, reason
	case "cancelled":
		return ScanCancelled, ""
	default:
		return ScanUnsure, "the result of this scan is not known; ask again to start a new scan"
	}
}

func scanItem(sc store.BrainstormScan) ScanItem {
	state, reason := plainState(sc)
	item := ScanItem{ScanID: sc.ID, Seq: sc.ScanSeq, Question: sc.Question, State: state, AdmittedAt: sc.CreatedAt, FinishedAt: sc.FinishedAt, Reason: reason}
	if state == ScanAnswered {
		item.ExchangeSeq = sc.ExchangeSeq
	}
	return item
}

func scanFinished(state string) bool {
	return state != ScanWaiting && state != ScanRunning && state != ScanStopping
}

func (s *Service) view(ctx context.Context, b store.BrainstormRecord) (View, error) {
	v := View{Brainstorm: headerOf(b), EntryCounts: map[string]int64{}, ScanCounts: map[string]int64{}, Entries: []Entry{}, Scans: []ScanItem{}}
	for _, k := range []string{KindIdea, KindDecision, KindOpenQuestion, KindRelatedIssue, KindDocumentNote, KindExchange} {
		v.EntryCounts[k] = 0
	}
	counts, err := s.db.BrainstormEntryCounts(ctx, b.ID)
	if err != nil {
		return View{}, err
	}
	var entryTotal int64
	for k, n := range counts {
		v.EntryCounts[k] = n
		entryTotal += n
	}
	states, err := s.db.BrainstormScanStateCounts(ctx, b.ID)
	if err != nil {
		return View{}, err
	}
	var scanTotal int64
	for state, n := range states {
		plain, _ := plainState(store.BrainstormScan{JobState: state})
		v.ScanCounts[plain] += n
		scanTotal += n
	}
	entries, err := s.db.NewestBrainstormEntries(ctx, b.ID, viewItems)
	if err != nil {
		return View{}, err
	}
	for _, e := range entries {
		v.Entries = append(v.Entries, entryOf(e))
	}
	v.Entries = v.Entries[fitSuffix(v.Entries, viewBytes):]
	scans, err := s.db.NewestBrainstormScans(ctx, b.ID, viewItems)
	if err != nil {
		return View{}, err
	}
	for _, sc := range scans {
		v.Scans = append(v.Scans, scanItem(sc))
	}
	v.Scans = v.Scans[fitSuffix(v.Scans, viewBytes):]
	if entryTotal > int64(len(v.Entries)) {
		v.NextActions = append(v.NextActions, "brainstorm_entries")
	}
	if scanTotal > int64(len(v.Scans)) {
		v.NextActions = append(v.NextActions, "brainstorm_scans")
	}
	if b.State == "open" {
		v.NextActions = append(v.NextActions, "brainstorm_add", "brainstorm_ask", "brainstorm_close")
	}
	return v, nil
}

// Open returns the owner's open brainstorm for the repository, or creates one.
func (s *Service) Open(ctx context.Context, p Principal, repositoryRef string) (View, error) {
	o, err := ownerOf(p)
	if err != nil {
		return View{}, err
	}
	if !validText(repositoryRef, 1024) {
		return View{}, ErrInvalid
	}
	if err = s.checkConsent(ctx, p, repositoryRef); err != nil {
		return View{}, err
	}
	if s.team != nil {
		if _, err = s.team.Capture(ctx, repositoryRef); err != nil {
			return View{}, err
		}
	}
	b, _, err := s.db.OpenBrainstorm(ctx, store.BrainstormRecord{BrainstormOwner: o, ID: mintID("brainstorm-"), RepositoryRef: repositoryRef, GrantID: p.GrantID}, s.now())
	if err != nil {
		return View{}, storeError(err)
	}
	return s.view(ctx, b)
}

// Get returns the header, counts and newest pages of one brainstorm.
func (s *Service) Get(ctx context.Context, p Principal, brainstormID string) (View, error) {
	_, b, err := s.brainstorm(ctx, p, brainstormID)
	if err != nil {
		return View{}, err
	}
	return s.view(ctx, b)
}

// Entries pages entries oldest first by seq, optionally of one kind.
func (s *Service) Entries(ctx context.Context, p Principal, brainstormID, kind, cursor string, limit int) (EntryPage, error) {
	limit, err := pageLimit(limit, cursor)
	if err != nil {
		return EntryPage{}, err
	}
	switch kind {
	case "", KindIdea, KindDecision, KindOpenQuestion, KindRelatedIssue, KindDocumentNote, KindExchange:
	default:
		return EntryPage{}, ErrInvalid
	}
	_, b, err := s.brainstorm(ctx, p, brainstormID)
	if err != nil {
		return EntryPage{}, err
	}
	want := boundCursor(p, "entries", b.ID)
	want.Kind = kind
	c, err := s.openCursor(cursor, want)
	if err != nil {
		return EntryPage{}, err
	}
	rows, err := s.db.BrainstormEntries(ctx, b.ID, kind, c.Seq, limit+1)
	if err != nil {
		return EntryPage{}, err
	}
	more := len(rows) > limit
	rows = rows[:min(len(rows), limit)]
	page := EntryPage{Entries: []Entry{}}
	for _, e := range rows {
		page.Entries = append(page.Entries, entryOf(e))
	}
	if n := fitPrefix(page.Entries, pageBytes); n < len(page.Entries) {
		page.Entries, more = page.Entries[:n], true
	}
	if more && len(page.Entries) > 0 {
		c.Seq = page.Entries[len(page.Entries)-1].Seq
		if page.NextCursor, err = s.sealCursor(c); err != nil {
			return EntryPage{}, err
		}
	}
	return page, nil
}

// Scans pages every admitted question oldest first by scan seq with live states.
func (s *Service) Scans(ctx context.Context, p Principal, brainstormID, cursor string, limit int) (ScanPage, error) {
	limit, err := pageLimit(limit, cursor)
	if err != nil {
		return ScanPage{}, err
	}
	_, b, err := s.brainstorm(ctx, p, brainstormID)
	if err != nil {
		return ScanPage{}, err
	}
	c, err := s.openCursor(cursor, boundCursor(p, "scans", b.ID))
	if err != nil {
		return ScanPage{}, err
	}
	rows, err := s.db.BrainstormScans(ctx, b.ID, c.Seq, limit+1)
	if err != nil {
		return ScanPage{}, err
	}
	more := len(rows) > limit
	rows = rows[:min(len(rows), limit)]
	page := ScanPage{Scans: []ScanItem{}}
	for _, sc := range rows {
		page.Scans = append(page.Scans, scanItem(sc))
	}
	if n := fitPrefix(page.Scans, pageBytes); n < len(page.Scans) {
		page.Scans, more = page.Scans[:n], true
	}
	if more && len(page.Scans) > 0 {
		c.Seq = page.Scans[len(page.Scans)-1].Seq
		if page.NextCursor, err = s.sealCursor(c); err != nil {
			return ScanPage{}, err
		}
	}
	return page, nil
}

// List pages the owner's brainstorms for one repository, newest first.
func (s *Service) List(ctx context.Context, p Principal, repositoryRef, cursor string, limit int) (ListPage, error) {
	limit, err := pageLimit(limit, cursor)
	if err != nil {
		return ListPage{}, err
	}
	o, err := ownerOf(p)
	if err != nil {
		return ListPage{}, err
	}
	if !validText(repositoryRef, 1024) {
		return ListPage{}, ErrInvalid
	}
	if err = s.checkConsent(ctx, p, repositoryRef); err != nil {
		return ListPage{}, err
	}
	c, err := s.openCursor(cursor, boundCursor(p, "list", repositoryRef))
	if err != nil {
		return ListPage{}, err
	}
	var after time.Time
	if c.ID != "" {
		if after, err = time.Parse(time.RFC3339Nano, c.Created); err != nil {
			return ListPage{}, ErrCursor
		}
	}
	rows, err := s.db.ListBrainstorms(ctx, o, repositoryRef, after, c.ID, limit+1, s.now())
	if err != nil {
		return ListPage{}, storeError(err)
	}
	page := ListPage{Brainstorms: []Brainstorm{}}
	for _, b := range rows[:min(len(rows), limit)] {
		page.Brainstorms = append(page.Brainstorms, headerOf(b))
	}
	if len(rows) > limit {
		last := rows[limit-1]
		c.Created, c.ID = cursorTime(last.CreatedAt), last.ID
		if page.NextCursor, err = s.sealCursor(c); err != nil {
			return ListPage{}, err
		}
	}
	return page, nil
}

// Add appends one owner entry under the expected revision.
func (s *Service) Add(ctx context.Context, p Principal, r AddRequest) (View, error) {
	if !validText(r.RequestKey, 128) || r.ExpectedRevision < 0 || !utf8.ValidString(r.Text) || strings.TrimSpace(r.Text) == "" {
		return View{}, ErrInvalid
	}
	switch r.Kind {
	case KindDocumentNote:
		if !validText(r.DocumentName, 512) {
			return View{}, fmt.Errorf("%w: a document note needs the document name", ErrInvalid)
		}
		if len(r.Text) > MaxEntryBytes {
			return View{}, fmt.Errorf("%w: a document note is at most 4 KB", ErrCapReached)
		}
	case KindIdea, KindDecision, KindOpenQuestion, KindRelatedIssue:
		if r.DocumentName != "" {
			return View{}, fmt.Errorf("%w: only a document note has a document name", ErrInvalid)
		}
		if len(r.Text) > MaxEntryBytes {
			return View{}, fmt.Errorf("%w: an entry is at most 4 KB", ErrCapReached)
		}
	default:
		return View{}, fmt.Errorf("%w: kind must be idea, decision, open_question, related_issue or document_note", ErrInvalid)
	}
	if r.IssueReference != "" && (r.Kind != KindRelatedIssue || !validText(r.IssueReference, 512)) {
		return View{}, fmt.Errorf("%w: only a related issue has an issue reference", ErrInvalid)
	}
	o, b, err := s.brainstorm(ctx, p, r.BrainstormID)
	if err != nil {
		return View{}, err
	}
	digest := requestDigest(struct{ Kind, Text, DocumentName, IssueReference string }{r.Kind, r.Text, r.DocumentName, r.IssueReference})
	b, _, replayed, err := s.db.AddBrainstormEntry(ctx, o, store.BrainstormEntry{ID: mintID("entry-"), BrainstormID: b.ID, Kind: r.Kind, Text: r.Text, DocumentName: r.DocumentName, IssueReference: r.IssueReference, RequestKey: r.RequestKey, Digest: digest}, r.ExpectedRevision, s.now())
	if err != nil {
		return View{}, storeError(err)
	}
	v, err := s.view(ctx, b)
	v.Replayed = replayed
	return v, err
}

// Close closes an open brainstorm by owner request; it stays readable.
func (s *Service) Close(ctx context.Context, p Principal, brainstormID, requestKey string, expectedRevision int64) (View, error) {
	if !validText(requestKey, 128) || expectedRevision < 0 {
		return View{}, ErrInvalid
	}
	o, b, err := s.brainstorm(ctx, p, brainstormID)
	if err != nil {
		return View{}, err
	}
	b, replayed, err := s.db.CloseBrainstorm(ctx, o, b.ID, expectedRevision, s.now())
	if err != nil {
		return View{}, storeError(err)
	}
	v, err := s.view(ctx, b)
	v.Replayed = replayed
	return v, err
}

// Ask admits one factual question as a queued brainstorm_scan job.
func (s *Service) Ask(ctx context.Context, p Principal, r AskRequest) (ScanView, error) {
	if !validText(r.RequestKey, 128) || !validText(r.Question, MaxQuestionBytes) {
		return ScanView{}, ErrInvalid
	}
	o, b, err := s.brainstorm(ctx, p, r.BrainstormID)
	if err != nil {
		return ScanView{}, err
	}
	digest := requestDigest(struct{ Question string }{r.Question})
	if old, err := s.db.BrainstormScanByRequest(ctx, o, b.ID, r.RequestKey); err == nil {
		if old.Digest != digest {
			return ScanView{}, ErrRequestReuse
		}
		v, err := s.scanView(p, old, "", MaxPageLimit)
		v.Replayed = true
		return v, err
	} else if !errors.Is(err, store.ErrNotFound) {
		return ScanView{}, err
	}
	if b.State != "open" {
		return ScanView{}, ErrClosed
	}
	if s.team == nil {
		return ScanView{}, ErrSkillMissing
	}
	team, err := s.team.Capture(ctx, b.RepositoryRef)
	if err != nil {
		return ScanView{}, err
	}
	files, err := json.Marshal(team)
	if err != nil {
		return ScanView{}, err
	}
	if team.Version == "" {
		return ScanView{}, ErrSkillMissing
	}
	if err = s.checkConsent(ctx, p, b.RepositoryRef); err != nil {
		return ScanView{}, err
	}
	consent, _ := json.Marshal(p)
	sc, replayed, err := s.db.AdmitBrainstormScan(ctx, store.BrainstormScan{BrainstormOwner: o, ID: mintID("scan-"), BrainstormID: b.ID, RequestKey: r.RequestKey, Digest: digest, Question: r.Question, TeamVersion: team.Version, TeamFiles: string(files), GrantID: p.GrantID, ConsentRevision: string(consent), JobID: mintID("brainstorm-scan-"), ExecutionID: mintID("execution-")}, s.now())
	if err != nil {
		return ScanView{}, storeError(err)
	}
	v, err := s.scanView(p, sc, "", MaxPageLimit)
	v.Replayed = replayed
	return v, err
}

// ScanGet returns one scan's live state, spoken answer and a page of evidence.
func (s *Service) ScanGet(ctx context.Context, p Principal, scanID, cursor string, limit int) (ScanView, error) {
	limit, err := pageLimit(limit, cursor)
	if err != nil {
		return ScanView{}, err
	}
	sc, err := s.scan(ctx, p, scanID)
	if err != nil {
		return ScanView{}, err
	}
	return s.scanView(p, sc, cursor, limit)
}

// ScanCancel cancels a waiting scan or asks a running scan to stop.
func (s *Service) ScanCancel(ctx context.Context, p Principal, scanID string) (ScanView, error) {
	sc, err := s.scan(ctx, p, scanID)
	if err != nil {
		return ScanView{}, err
	}
	o, _ := ownerOf(p)
	sc, _, err = s.db.CancelBrainstormScan(ctx, o, sc.ID, s.now())
	if err != nil {
		return ScanView{}, storeError(err)
	}
	return s.scanView(p, sc, "", MaxPageLimit)
}

func (s *Service) scan(ctx context.Context, p Principal, scanID string) (store.BrainstormScan, error) {
	o, err := ownerOf(p)
	if err != nil {
		return store.BrainstormScan{}, err
	}
	if !validText(scanID, 256) {
		return store.BrainstormScan{}, ErrNotFound
	}
	sc, err := s.db.ReadBrainstormScan(ctx, o, scanID)
	if err != nil {
		return sc, storeError(err)
	}
	if _, _, err = s.brainstorm(ctx, p, sc.BrainstormID); err != nil {
		return sc, err
	}
	return sc, nil
}

func (s *Service) scanView(p Principal, sc store.BrainstormScan, cursor string, limit int) (ScanView, error) {
	item := scanItem(sc)
	v := ScanView{Scan: item, BrainstormID: sc.BrainstormID, Cost: CostEstimateUnknown}
	if !scanFinished(item.State) {
		if cursor != "" {
			return ScanView{}, ErrCursor
		}
		v.NextActions = []string{"brainstorm_scan_get", "brainstorm_scan_cancel"}
		return v, nil
	}
	v.Cost = sc.Cost
	if v.Cost == "" {
		v.Cost = CostUnknown
	}
	v.Answer, v.Commit = sc.Answer, sc.BasisCommit
	want := boundCursor(p, "evidence", sc.ID)
	want.State = item.State
	c, err := s.openCursor(cursor, want)
	if err != nil {
		return ScanView{}, err
	}
	var e evidence
	if sc.Research != "" {
		if err = json.Unmarshal([]byte(sc.Research), &e); err != nil {
			return ScanView{}, err
		}
	}
	page, next, done := evidencePage(e, c.Offsets, limit)
	v.Findings, v.Decisions, v.Assumptions = page.Findings, page.Decisions, page.Assumptions
	if !done {
		c.Offsets = next
		if v.NextCursor, err = s.sealCursor(c); err != nil {
			return ScanView{}, err
		}
		v.NextActions = []string{"brainstorm_scan_get"}
	}
	return v, nil
}
