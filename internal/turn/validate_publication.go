package turn

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
)

const validatePartPurpose = "validate-guidance-part:"
const validateIndexPurpose = "validate-guidance-index:"

type validateGuidanceManifest struct {
	CreatedAt                                                time.Time
	Partial, PreviousDigest, PreviousRevision                string
	Intents                                                  []store.ReportDelivery
	Profile                                                  *graphexec.Profile
	Instance                                                 int64
	Revision, Commit, Head, Set, Base, PartialDigest, Notice string
	Parts                                                    []string
}

type validateIndexEdit struct {
	PartialBody                                   string
	Profile                                       *graphexec.Profile
	Instance                                      int64
	Revision, PartialDigest, Digest, Head, Commit string
	CommentID                                     int64
}

// prepareValidateGuidance freezes every final redacted part before dispatch.
// The first part's retained purpose carries the immutable set manifest; it is
// application metadata, never a publication or an invented provider receipt.
func (r *Reporter) prepareValidateGuidance(ctx context.Context, sessionID int64, task naming.Ref, st ValidateState, profile *graphexec.Profile, base, notice string, at time.Time) (string, []store.ReportDelivery, error) {
	var err error
	st, err = r.redactValidateFindingSummaries(ctx, st)
	if err != nil {
		return "", nil, err
	}
	items := validateDecisionItems(st)
	if len(items) == 0 {
		return base, nil, nil
	}
	header := validateGuidanceHeader(st)
	full := base + header + strings.Join(items, "\n\n") + "\n"
	if len(full) <= github.CommentSizeLimit-validateStoryCommentReserve {
		return full, nil, nil
	}
	if len(base) > github.CommentSizeLimit/2 {
		var lines []string
		for _, line := range strings.Split(base, "\n") {
			if strings.HasPrefix(line, "Ship this displayed") || strings.HasPrefix(line, "Return reviewed") || strings.HasPrefix(line, "Legacy repair option:") || strings.HasPrefix(line, "Next:") || strings.HasPrefix(line, "To rerun the missing reviews:") {
				lines = append(lines, line)
			}
		}
		base = fmt.Sprintf("**Validate report** · tested `%s` · suite %s · revision `%s`\n\nThe detailed story overview exceeds this comment's capacity. Every item's state, current choices and consequences are in the numbered guidance below. Checks and finding choices do not authorize delivery.\n\n%s\n", st.Commit, st.Suite.Outcome, st.Revision, strings.Join(lines, "\n\n"))
	}
	baseBodyForSet, err := r.renderer.RenderReportDelivery(ctx, base, "")
	if err != nil {
		return "", nil, err
	}
	base = baseBodyForSet.String()
	for i, item := range items {
		rendered, e := r.renderer.RenderReportDelivery(ctx, item, "")
		if e != nil {
			return "", nil, e
		}
		items[i] = rendered.String()
	}
	parts, err := packValidateDecisionItems(items, github.CommentSizeLimit-validateStoryCommentReserve-2048)
	if err != nil {
		return "", nil, err
	}
	layout, _ := json.Marshal(struct {
		Version                              int
		Base, Header, Revision, Commit, Head string
		Parts                                []string
	}{1, base, header, st.Revision, st.Commit, st.DeliveryCommit, parts})
	sum := sha256.Sum256(layout)
	set := fmt.Sprintf("%x", sum)
	partial := base + header + fmt.Sprintf("**Partial publication:** %d numbered guidance parts for this revision are being confirmed. The current index is incomplete; omitted choices are not all reachable yet. Existing recovery completes this index automatically; no owner acknowledgment is needed.\n\n<!-- lf:validate-guidance-set %s -->\n", len(parts), set)
	partialBody, e := r.renderer.RenderReportDelivery(ctx, partial, "")
	if e != nil {
		return "", nil, e
	}
	partial = partialBody.String()
	// The overview is bounded separately from the complete per-item projection.
	if len(partial) > github.CommentSizeLimit-validateStoryCommentReserve {
		return "", nil, errors.New("Validate overview exceeds its publication budget")
	}
	minimal := &graphexec.Profile{}
	for _, n := range profile.Nodes {
		if n.Action == graphexec.ValidateSuiteAction || n.Action == graphexec.ValidateImplementAction {
			minimal.Nodes = append(minimal.Nodes, graphexec.Node{Name: n.Name, Action: n.Action})
		}
	}
	m := validateGuidanceManifest{CreatedAt: at, Profile: minimal, Instance: st.InstanceID, Revision: st.Revision, Commit: st.Commit, Head: st.DeliveryCommit, Set: set, Base: base + header, PartialDigest: fmt.Sprintf("%x", sha256.Sum256([]byte(partial))), Notice: notice}
	intents := make([]store.ReportDelivery, len(parts))
	for i, part := range parts {
		purpose := fmt.Sprintf("%s%d:%s:%d", validatePartPurpose, st.InstanceID, set, i+1)
		intent := r.deliveryIntent(task, task, sessionID, 0, 0, purpose)
		intent.TriggerNamespace = store.PlanNoticeNamespace
		text := fmt.Sprintf("**Validate decision guidance · part %d/%d** · revision `%s` · tested `%s` · displayed delivery `%s`\n\nReply on the parent issue. This part belongs only to this displayed revision; old decisions remain refused.\n\n%s\n", i+1, len(parts), st.Revision, st.Commit, st.DeliveryCommit, part)
		body, e := r.renderer.RenderReportDelivery(ctx, text, intent.Marker)
		if e != nil {
			return "", nil, e
		}
		intent.Body = body.String()
		intent.BotUserID, e = retainedTicketPublisher(ctx, r.db, sessionID, r.botUserID)
		if e != nil {
			return "", nil, e
		}
		intents[i] = intent
		m.Parts = append(m.Parts, intent.Key)
	}
	// Redact the base before it enters immutable metadata as well.
	baseBody, err := r.renderer.RenderReportDelivery(ctx, m.Base, "")
	if err != nil {
		return "", nil, err
	}
	m.Base = baseBody.String()
	noticeBody, err := r.renderer.RenderReportDelivery(ctx, m.Notice, "")
	if err != nil {
		return "", nil, err
	}
	m.Notice = noticeBody.String()
	m.Partial = partial
	previous, e := r.db.GetValidateReport(ctx, st.InstanceID)
	if e != nil && !errors.Is(e, store.ErrNotFound) {
		return "", nil, e
	}
	m.PreviousDigest, m.PreviousRevision = previous.Digest, previous.Revision
	m.Intents = append([]store.ReportDelivery(nil), intents...)
	encoded, err := json.Marshal(m)
	if err != nil {
		return "", nil, err
	}
	if existing, e := r.db.GetReportDelivery(ctx, intents[0].Key); e == nil {
		intents[0].Purpose = existing.Purpose
	} else if !errors.Is(e, store.ErrNotFound) {
		return "", nil, e
	} else {
		intents[0].Purpose += "|" + string(encoded)
	}
	for i, intent := range intents {
		saved, e := r.db.EnsureReportDelivery(ctx, intent)
		if e != nil {
			return "", nil, e
		}
		intents[i] = saved
	}
	index := r.deliveryIntent(task, task, sessionID, 0, 0, validateIndexPurpose+set)
	if saved, e := r.db.GetReportDelivery(ctx, index.Key); e == nil && saved.Completed {
		report, e := r.db.GetValidateReport(ctx, st.InstanceID)
		if e == nil && report.Revision == st.Revision {
			return strings.TrimSuffix(saved.Body, "\n\n"+report.Marker), nil, nil
		}
	}
	return partial, intents, nil
}

// redactValidateFindingSummaries preserves raw retained state while ensuring
// a later summary clip cannot hide the suffix of a secret from redaction.
func (r *Reporter) redactValidateFindingSummaries(ctx context.Context, st ValidateState) (ValidateState, error) {
	st.Findings = slices.Clone(st.Findings)
	for i := range st.Findings {
		summary, err := r.renderer.RenderReportDelivery(ctx, st.Findings[i].Summary, "")
		if err != nil {
			return st, err
		}
		st.Findings[i].Summary = summary.String()
		if st.Findings[i].Verdict != nil {
			verdict := *st.Findings[i].Verdict
			safe, err := r.renderer.RenderReportDelivery(ctx, verdict.Evidence, "")
			if err != nil {
				return st, err
			}
			verdict.Evidence = safe.String()
			st.Findings[i].Verdict = &verdict
		}
	}
	return st, nil
}

// RecoverValidatePublications uses the existing report recovery phase. It
// sends only retained bytes and never runs validation or grants authority.
func (r *Reporter) RecoverValidatePublications(ctx context.Context, sessionID int64) (err error) {
	latest, err := r.db.LatestWorkflowInstance(ctx, sessionID, "validate")
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	unlock, err := r.lockValidateReport(ctx, latest.ID)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, unlock()) }()
	rows, err := r.db.ListSessionReportDeliveries(ctx, sessionID)
	if err != nil {
		return err
	}
	var faults []error
	for _, d := range rows {
		if strings.HasPrefix(d.Purpose, validatePartPurpose) && strings.Contains(d.Purpose, "|") {
			var m validateGuidanceManifest
			_, raw, _ := strings.Cut(d.Purpose, "|")
			if e := json.Unmarshal([]byte(raw), &m); e != nil {
				faults = append(faults, e)
				continue
			}
			if e := r.resumeValidateGuidanceSet(ctx, d, m); e != nil {
				faults = append(faults, e)
			}
		}
	}
	for _, d := range rows {
		if !d.Completed && (strings.HasPrefix(d.Purpose, validatePartPurpose) || strings.HasPrefix(d.Purpose, validateIndexPurpose)) {
			fresh, e := r.db.GetReportDelivery(ctx, d.Key)
			if errors.Is(e, store.ErrNotFound) {
				continue
			}
			if e != nil {
				faults = append(faults, e)
				continue
			}
			if fresh.Completed {
				continue
			}
			_, e = r.dispatchDelivery(ctx, fresh)
			faults = append(faults, e)
		}
	}
	// Reconcile the current projection after settling historical receipts: an
	// old remote edit can complete late, even after this revision was confirmed.
	faults = append(faults, r.reconcileCurrentValidateIndex(ctx, sessionID))
	return errors.Join(faults...)
}

func (r *Reporter) resumeValidateGuidanceSet(ctx context.Context, first store.ReportDelivery, m validateGuidanceManifest) error {
	latest, err := r.db.LatestWorkflowInstance(ctx, first.SessionID, "validate")
	if err != nil {
		return err
	}
	if latest.ID != m.Instance {
		return nil
	}
	current, err := LoadValidateState(ctx, Options{DB: r.db}, m.Profile, m.Instance)
	if err != nil {
		return err
	}
	if current.Revision != m.Revision || current.Commit != m.Commit {
		return nil
	}
	session, err := r.db.GetSession(ctx, first.SessionID)
	if err != nil {
		return err
	}
	if r.validateHead == nil {
		return errors.New("current Validate delivery head cannot be observed")
	}
	head, err := r.validateHead(ctx, session)
	if err != nil {
		return err
	}
	if head != m.Head {
		return nil
	}
	saved, err := r.db.GetValidateReport(ctx, m.Instance)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}
	if saved.Digest == "" {
		effective, e := r.validateManifestReportDigest(ctx, first, m, saved)
		if e != nil {
			return e
		}
		if effective != "" {
			saved.Digest = effective
		}
	}
	if saved.Digest != m.PartialDigest {
		if saved.Digest != m.PreviousDigest || saved.Revision != m.PreviousRevision {
			return nil
		}
		if _, err = r.ReportValidate(ctx, m.Instance, m.Revision, m.Notice, m.Partial, m.CreatedAt); err != nil {
			return err
		}
	}
	// Restore a set interrupted during intent retention, before dispatching any
	// part. Every restored body was already redacted in the first manifest.
	for i, intent := range m.Intents {
		if i == 0 {
			continue
		}
		if _, err = r.db.EnsureReportDelivery(ctx, intent); err != nil {
			return err
		}
	}
	for _, key := range m.Parts {
		part, e := r.db.GetReportDelivery(ctx, key)
		if e != nil {
			return e
		}
		if !part.Completed {
			if _, e = r.dispatchDelivery(ctx, part); e != nil {
				return e
			}
		}
	}
	// A crash may leave all parts complete before the current partial overview
	// exists. Derive the index from their retained receipts on every recovery.
	return r.finishValidatePart(ctx, first)
}

// reconcileCurrentValidateIndex projects only the authoritative current set.
// Provider edits have no compare-and-swap: an old request may finish after a
// newer one, so the existing recovery phase observes and repairs current bytes.
func (r *Reporter) reconcileCurrentValidateIndex(ctx context.Context, sessionID int64) error {
	latest, err := r.db.LatestWorkflowInstance(ctx, sessionID, "validate")
	if err != nil {
		return err
	}
	saved, err := r.db.GetValidateReport(ctx, latest.ID)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	rows, err := r.db.ListSessionReportDeliveries(ctx, sessionID)
	if err != nil {
		return err
	}
	for _, d := range rows {
		if !strings.HasPrefix(d.Purpose, validateIndexPurpose) || !d.Completed || d.CommentID == 0 {
			continue
		}
		_, raw, ok := strings.Cut(d.Purpose, "|")
		if !ok {
			return errors.New("Validate index edit metadata unavailable")
		}
		var meta validateIndexEdit
		if err = json.Unmarshal([]byte(raw), &meta); err != nil {
			return err
		}
		if meta.Instance != latest.ID || meta.Revision != saved.Revision || meta.Digest != saved.Digest || meta.CommentID != saved.CommentID {
			continue
		}
		current, err := LoadValidateState(ctx, Options{DB: r.db}, meta.Profile, meta.Instance)
		if err != nil {
			return err
		}
		if current.Revision != meta.Revision || current.Commit != meta.Commit {
			return nil
		}
		session, err := r.db.GetSession(ctx, sessionID)
		if err != nil {
			return err
		}
		if r.validateHead == nil {
			return errors.New("current Validate delivery head cannot be observed")
		}
		head, err := r.validateHead(ctx, session)
		if err != nil {
			return err
		}
		if head != meta.Head {
			return nil
		}
		destination, err := naming.ParseRef(d.Destination)
		if err != nil {
			return err
		}
		// This scoped provider lookup must observe the confirmed parent identity,
		// factory author and exact retained current bytes before skipping an edit.
		comments, err := r.deliveryComments(ctx, destination)
		if err != nil {
			return err
		}
		for _, c := range comments {
			if c.ID == meta.CommentID && c.Author.ID == d.BotUserID && c.Body == d.Body {
				return nil
			}
		}
		// Direct dispatch rechecks active instance, factual revision, tested commit
		// and observed delivery head. Completed history never authorizes old bytes.
		_, err = r.dispatchValidateIndex(ctx, d)
		return err
	}
	return nil
}

func (r *Reporter) lockValidateReport(ctx context.Context, instanceID int64) (func() error, error) {
	instance, err := r.db.GetGraphInstance(ctx, instanceID)
	if err != nil {
		return nil, err
	}
	session, err := r.db.GetSession(ctx, instance.SessionID)
	if err != nil {
		return nil, err
	}
	dir := r.storyLockDir
	if dir == "" {
		dir = filepath.Dir(r.db.Path())
	}
	return r.db.LockValidateReport(ctx, session.TaskRef, dir)
}

// A cleared report digest is the existing story-refresh retry flag. It is
// eligible for publication recovery only when provider bytes independently
// confirm this exact frozen partial or final projection; the flag stays clear.
func (r *Reporter) validateManifestReportDigest(ctx context.Context, d store.ReportDelivery, m validateGuidanceManifest, saved store.ValidateReport) (string, error) {
	partial, err := r.renderer.RenderReportDelivery(ctx, m.Partial, saved.Marker)
	if err != nil {
		return "", err
	}
	destination, err := naming.ParseRef(d.Destination)
	if err != nil {
		return "", err
	}
	index := r.deliveryIntent(destination, destination, d.SessionID, 0, 0, validateIndexPurpose+m.Set)
	var finalBody, finalDigest string
	if final, e := r.db.GetReportDelivery(ctx, index.Key); e == nil {
		finalBody = final.Body
		_, raw, _ := strings.Cut(final.Purpose, "|")
		var meta validateIndexEdit
		if e = json.Unmarshal([]byte(raw), &meta); e != nil {
			return "", e
		}
		finalDigest = meta.Digest
	} else if !errors.Is(e, store.ErrNotFound) {
		return "", e
	}
	return r.validateObservedReportDigest(ctx, d, saved, partial.String(), m.PartialDigest, finalBody, finalDigest)
}
func (r *Reporter) validateObservedReportDigest(ctx context.Context, d store.ReportDelivery, saved store.ValidateReport, partial, partialDigest, final, finalDigest string) (string, error) {
	destination, err := naming.ParseRef(d.Destination)
	if err != nil {
		return "", err
	}
	comments, err := r.deliveryComments(ctx, destination)
	if err != nil {
		return "", err
	}
	for _, c := range comments {
		if c.ID != saved.CommentID || c.Author.ID != d.BotUserID {
			continue
		}
		if c.Body == partial {
			return partialDigest, nil
		}
		if final != "" && c.Body == final {
			return finalDigest, nil
		}
	}
	return "", nil
}

func (r *Reporter) validateManifest(ctx context.Context, d store.ReportDelivery) (validateGuidanceManifest, error) {
	rows, err := r.db.ListSessionReportDeliveries(ctx, d.SessionID)
	if err != nil {
		return validateGuidanceManifest{}, err
	}
	for _, row := range rows {
		if !strings.HasPrefix(row.Purpose, validatePartPurpose) {
			continue
		}
		_, raw, ok := strings.Cut(row.Purpose, "|")
		if !ok {
			continue
		}
		var m validateGuidanceManifest
		if err := json.Unmarshal([]byte(raw), &m); err != nil {
			return m, err
		}
		for _, key := range m.Parts {
			if key == d.Key {
				return m, nil
			}
		}
	}
	return validateGuidanceManifest{}, errors.New("Validate part set manifest unavailable")
}

// finishValidatePart runs before the last confirmed part is completed, closing
// the crash window between its provider receipt and the durable index edit.
func (r *Reporter) finishValidatePart(ctx context.Context, d store.ReportDelivery) error {
	if !strings.HasPrefix(d.Purpose, validatePartPurpose) {
		return nil
	}
	m, err := r.validateManifest(ctx, d)
	if err != nil {
		return err
	}
	saved, err := r.db.GetValidateReport(ctx, m.Instance)
	if err != nil {
		return err
	}
	if saved.Digest == "" {
		effective, e := r.validateManifestReportDigest(ctx, d, m, saved)
		if e != nil {
			return e
		}
		if effective == "" {
			return errors.New("current Validate overview bytes are not confirmed")
		}
		saved.Digest = effective
	}
	latest, err := r.db.LatestWorkflowInstance(ctx, d.SessionID, "validate")
	if err != nil {
		return err
	}
	if latest.ID != m.Instance || saved.Revision != m.Revision || saved.Digest != m.PartialDigest {
		return nil
	}
	current, err := LoadValidateState(ctx, Options{DB: r.db}, m.Profile, m.Instance)
	if err != nil {
		return err
	}
	if current.Revision != m.Revision || current.Commit != m.Commit {
		return nil
	}
	destination, err := naming.ParseRef(d.Destination)
	if err != nil {
		return err
	}
	// A receipt identifies the publication, but complete guidance also requires
	// its current provider bytes and publisher. Resolve links only after one
	// scoped listing confirms every retained part, for every provider adapter.
	retainedParts := make([]store.ReportDelivery, 0, len(m.Parts))
	for _, key := range m.Parts {
		part, e := r.db.GetReportDelivery(ctx, key)
		if e != nil {
			return e
		}
		if part.CommentID == 0 {
			return nil
		}
		retainedParts = append(retainedParts, part)
	}
	comments, err := r.deliveryComments(ctx, destination)
	if err != nil {
		return err
	}
	observed := make(map[int64]github.Comment, len(comments))
	for _, c := range comments {
		observed[c.ID] = c
	}
	var links []string
	for i, part := range retainedParts {
		comment, ok := observed[part.CommentID]
		if !ok || comment.Author.ID != part.BotUserID || comment.Body != part.Body {
			return fmt.Errorf("Validate guidance part %d/%d is absent or changed; its complete commands are not confirmed", i+1, len(m.Parts))
		}
		link := comment.HTMLURL
		if _, qualified := r.forge.(interface {
			SourceLinks(context.Context, naming.Ref, int, int64) (string, string, error)
		}); qualified {
			_, link, err = reportSourceLinks(ctx, r.forge, destination, destination.Issue, part.CommentID)
			if err != nil {
				return err
			}
		}
		if link == "" {
			return errors.New("confirmed Validate part has no provider-observed destination")
		}
		links = append(links, fmt.Sprintf("- [Part %d/%d](%s)", i+1, len(m.Parts), link))
	}

	text := m.Base + "All numbered parts for this revision are confirmed. Every item's current commands and consequences are available below.\n\n" + strings.Join(links, "\n") + "\n"
	partialBody, e := r.renderer.RenderReportDelivery(ctx, m.Partial, saved.Marker)
	if e != nil {
		return e
	}
	meta := validateIndexEdit{PartialBody: partialBody.String(), Profile: m.Profile, Instance: m.Instance, Revision: m.Revision, Head: m.Head, Commit: m.Commit, PartialDigest: m.PartialDigest, Digest: fmt.Sprintf("%x", sha256.Sum256([]byte(text))), CommentID: saved.CommentID}
	raw, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	intent := r.deliveryIntent(destination, destination, d.SessionID, 0, 0, validateIndexPurpose+m.Set)
	intent.TriggerNamespace = store.PlanNoticeNamespace
	body, err := r.renderer.RenderReportDelivery(ctx, text, saved.Marker)
	if err != nil {
		return err
	}
	intent.Body = body.String()
	intent.Purpose += "|" + string(raw)
	intent.BotUserID = d.BotUserID
	retained, err := r.db.EnsureReportDelivery(ctx, intent)
	if err != nil {
		return err
	}
	_, err = r.dispatchDelivery(ctx, retained)
	return err
}

func (r *Reporter) dispatchValidateIndex(ctx context.Context, d store.ReportDelivery) (Posted, error) {
	_, raw, ok := strings.Cut(d.Purpose, "|")
	if !ok {
		return Posted{}, errors.New("Validate index edit metadata unavailable")
	}
	var meta validateIndexEdit
	if err := json.Unmarshal([]byte(raw), &meta); err != nil {
		return Posted{}, err
	}
	destination, err := naming.ParseRef(d.Destination)
	if err != nil {
		return Posted{}, err
	}
	// An edit may have succeeded before its response was lost. Observe those
	// exact bytes before deciding whether an old local intent can be discarded.
	if d.PossiblySent && d.CommentID == 0 {
		comments, e := r.deliveryComments(ctx, destination)
		if e != nil {
			return Posted{}, e
		}
		for _, c := range comments {
			if c.ID == meta.CommentID && c.Author.ID == d.BotUserID && c.Body == d.Body {
				d, e = r.db.ConfirmReportDelivery(ctx, d.Key, c.ID)
				if e != nil {
					return Posted{}, e
				}
				break
			}
		}
	}
	saved, err := r.db.GetValidateReport(ctx, meta.Instance)
	if err != nil {
		return Posted{}, err
	}
	retryStories := saved.Digest == ""
	if retryStories {
		effective, e := r.validateObservedReportDigest(ctx, d, saved, meta.PartialBody, meta.PartialDigest, d.Body, meta.Digest)
		if e != nil {
			return Posted{}, e
		}
		if effective == "" {
			return Posted{}, errors.New("current Validate overview bytes are not confirmed")
		}
		saved.Digest = effective
	}
	latest, err := r.db.LatestWorkflowInstance(ctx, d.SessionID, "validate")
	if err != nil {
		return Posted{}, err
	}
	current, err := LoadValidateState(ctx, Options{DB: r.db}, meta.Profile, meta.Instance)
	if err != nil {
		return Posted{}, err
	}
	if (current.Revision != meta.Revision || current.Commit != meta.Commit) && saved.Revision == meta.Revision && (saved.Digest == meta.PartialDigest || saved.Digest == meta.Digest) {
		return Posted{}, errors.New("Validate evidence changed; current report refresh must replace the pending index")
	}
	if latest.ID != meta.Instance || saved.Revision != meta.Revision || (saved.Digest != meta.PartialDigest && saved.Digest != meta.Digest) {
		if d.CommentID != 0 {
			completed, e := r.db.CompleteReportDelivery(ctx, d.Key)
			if e != nil {
				return Posted{}, e
			}
			return r.completedDelivery(ctx, completed, true)
		}
		if d.PossiblySent {
			// Keep historical uncertainty and its immutable intent. It has no
			// authority over the current index and cannot hold current owner work.
			return Posted{Skipped: true}, nil
		}
		if err = r.db.SupersedeValidateIndexEdit(ctx, d.Key, meta.Instance, meta.Revision, meta.PartialDigest, meta.Digest); err != nil {
			return Posted{}, err
		}
		return Posted{Skipped: true}, nil
	}
	session, err := r.db.GetSession(ctx, d.SessionID)
	if err != nil {
		return Posted{}, err
	}
	if r.validateHead == nil {
		return Posted{}, errors.New("current Validate delivery head cannot be observed")
	}
	head, err := r.validateHead(ctx, session)
	if err != nil {
		return Posted{}, err
	}
	// The manifest target is retained in the index text and decision bindings.
	if head == "" || head != meta.Head {
		return Posted{}, errors.New("Validate delivery head moved; current report refresh is required before indexing")
	}
	editor, ok := r.forge.(CommentEditor)
	if !ok {
		return Posted{}, errors.New("Validate index cannot be edited")
	}
	body, err := r.renderer.RenderReportDelivery(ctx, d.Body, "")
	if err != nil {
		return Posted{}, err
	}
	if body.String() != d.Body {
		return Posted{}, errors.New("retained Validate index bytes changed during outbound rendering")
	}
	if _, err = r.db.MarkReportDeliverySent(ctx, d.Key); err != nil {
		return Posted{}, err
	}
	if err = editor.EditComment(ctx, github.RepoFromRef(destination), meta.CommentID, body); err != nil {
		if errors.Is(err, github.ErrRateLimited) {
			err = errors.Join(err, r.db.UnmarkReportDeliverySent(context.WithoutCancel(ctx), d.Key))
		}
		return Posted{}, err
	}
	comments, err := r.deliveryComments(ctx, destination)
	if err != nil {
		return Posted{}, err
	}
	confirmed := false
	for _, c := range comments {
		if c.ID == meta.CommentID && c.Author.ID == d.BotUserID && c.Body == body.String() {
			confirmed = true
			break
		}
	}
	if !confirmed {
		return Posted{}, errors.New("Validate index edit is not confirmed by provider observation")
	}
	if _, err = r.db.ConfirmReportDelivery(ctx, d.Key, meta.CommentID); err != nil {
		return Posted{}, err
	}
	saved.Digest = meta.Digest
	if retryStories {
		saved.Digest = ""
	}

	if err = r.db.PutValidateReport(ctx, saved); err != nil {
		return Posted{}, err
	}
	current.DecisionTask = d.Task
	current.DeliveryCommit = meta.Head
	if !effectDone(current.Effects.Close) && !current.Reimplementing {
		for _, action := range ValidateControlCommands {
			if action == ValidateReturnCommand && (!current.Reimplement || !current.Assessed) {
				continue
			}
			if err = r.db.PutOwnerDecisionDisplay(ctx, store.OwnerDecisionDisplay{TaskRef: d.Task, Action: action, Target: validateDecisionTarget(current), Revision: meta.Revision, InstanceID: meta.Instance, PublicationID: meta.CommentID, TestedCommit: current.Commit, DeliveryCommit: meta.Head, CreatedAt: saved.UpdatedAt}); err != nil {
				return Posted{}, err
			}
		}
	}
	completed, err := r.db.CompleteReportDelivery(ctx, d.Key)
	if err != nil {
		return Posted{}, err
	}
	return r.completedDelivery(ctx, completed, false)
}
