package turn

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
)

// CodeDelivery contains only repository/branch/PR integration operations.
// Ticket adapters cannot select a code repository or acquire merge authority.
type CodeDelivery interface {
	GetDeliveryHead(context.Context, github.Repo, string) (string, error)
	ListDeliveryPRs(context.Context, github.Repo, string) ([]github.DeliveryPR, error)
	GetDeliveryPR(context.Context, github.Repo, int) (github.DeliveryPR, error)
	CreateDeliveryPR(context.Context, github.Repo, string, string, redact.Body, redact.Body) (github.DeliveryPR, error)
	MergeDeliveryPR(context.Context, github.Repo, int, string) error
}
type deliveryRenderer interface {
	RenderDeliveryContent(context.Context, string, string) (redact.Body, redact.Body, error)
	RenderDeliveryNotice(context.Context, string, string) (redact.Body, error)
}

// DeliveryControl carries explicit authenticated owner input, never agent text.
type DeliveryControl struct {
	Parent string
	// SourceID selects separately retained CLI or PR evidence; empty preserves legacy controls.
	SourceID                    string
	CommentID, ActorID, OwnerID int64
	Body                        string
}

// DeliveryRequest selects either a readiness request or exact PR/head approval.
type DeliveryRequest struct {
	Generation, PRNumber int
	Key, Head, Target    string
	Merge                bool
}

// ParseDelivery accepts only complete bounded controls with a fixed main target.
func ParseDelivery(body string) (DeliveryRequest, error) {
	var r DeliveryRequest
	f := strings.Fields(body)
	if len(body) > 512 || len(f) < 2 {
		return r, fmt.Errorf("use /deliver <generation> or /merge <generation> <proposal digest> <PR number> <head SHA> main")
	}
	n, err := strconv.Atoi(f[1])
	if err != nil || n < 1 {
		return r, fmt.Errorf("invalid delivery generation")
	}
	r.Generation = n
	if f[0] == "/deliver" && len(f) == 2 {
		return r, nil
	}
	if f[0] != "/merge" || len(f) != 6 || len(f[2]) != 64 || strings.Trim(f[2], "0123456789abcdef") != "" || len(f[4]) != 40 || strings.Trim(f[4], "0123456789abcdef") != "" || f[5] != "main" {
		return r, fmt.Errorf("merge approval must identify exact proposal, PR, head SHA and main target")
	}
	number, err := strconv.Atoi(f[3])
	if err != nil || number < 1 {
		return r, fmt.Errorf("invalid delivery PR number")
	}
	r.Merge = true
	r.Key = f[2]
	r.PRNumber = number
	r.Head = f[4]
	r.Target = f[5]
	return r, nil
}

// ApplyDelivery serializes approval with task execution/save through the existing
// session lock, and with proposal recovery through the parent progress lock.
func (p *StoryProgress) ApplyDelivery(ctx context.Context, c DeliveryControl) (err error) {
	if c.SourceID != "" {
		source, e := p.db.GetDeliveryControlSource(ctx, c.SourceID)
		if e != nil {
			return e
		}
		session, e := p.db.GetSession(ctx, source.SessionID)
		if e != nil {
			return e
		}
		authority, e := p.db.EnsureDeliveryCodeAuthority(ctx, source.SessionID)
		if e != nil {
			return e
		}
		if source.Completed {
			return nil
		}
		if c.Parent != session.TaskRef || c.Body != source.Body || c.ActorID != source.ActorID || c.OwnerID != authority.OwnerID || c.CommentID != source.CommentID {
			return fmt.Errorf("delivery source differs from retained GitHub authority")
		}
		defer func() {
			if err == nil {
				err = p.db.CompleteDeliveryControlSource(ctx, c.SourceID)
			}
		}()
	} else if c.OwnerID < 1 || c.ActorID != c.OwnerID || c.CommentID < 1 {
		return fmt.Errorf("delivery requires authenticated numeric owner comment")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	release, err := p.db.LockStoryProgress(ctx, c.Parent, p.lockDir)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, release()) }()
	r, err := ParseDelivery(c.Body)
	if err != nil {
		return p.deliveryRefusal(ctx, c, err.Error())
	}
	f, err := p.db.GetManagedFeature(ctx, c.Parent)
	if errors.Is(err, store.ErrNotFound) {
		return p.deliveryRefusal(ctx, c, "No approved separated workflow.")
	}
	if err != nil {
		return err
	}
	if f.WorkflowVersion != store.WorkflowSeparated || f.Generation != r.Generation {
		return p.deliveryRefusal(ctx, c, "Current separated-workflow generation does not match.")
	}
	session, err := p.db.GetSessionByTaskRef(ctx, c.Parent)
	if err != nil {
		return err
	}
	unlock, acquired, err := p.lockDeliverySession(ctx, session)
	if err != nil {
		return err
	}
	if !acquired {
		return fmt.Errorf("delivery waits for active task/save to finish")
	}
	defer func() { err = errors.Join(err, unlock()) }()
	if !r.Merge {
		return p.prepareDelivery(ctx, c.Parent, true)
	}
	q, err := p.db.GetDelivery(ctx, r.Key)
	if errors.Is(err, store.ErrNotFound) {
		return p.deliveryRefusal(ctx, c, "Unknown concrete delivery proposal.")
	}
	if err != nil {
		return err
	}
	if c.SourceID != "" {
		source, e := p.db.GetDeliveryControlSource(ctx, c.SourceID)
		if e != nil {
			return e
		}
		if source.Kind != "github-pr-comment" || source.ProposalKey != q.Key {
			return fmt.Errorf("merge requires the bound GitHub PR comment")
		}
	}
	if q.Parent != c.Parent || q.Generation != r.Generation || q.PRNumber != r.PRNumber || q.Head != r.Head || q.Target != r.Target || !q.Proposed || q.Superseded {
		return p.deliveryRefusal(ctx, c, "Delivery approval does not match the active proposed PR/head/main.")
	}
	ref, err := naming.ParseRef(q.Parent)
	if err != nil {
		return err
	}
	parent, err := p.forge.GetIssue(ctx, github.RepoFromRef(ref), ref.Issue)
	if err != nil {
		return err
	}
	if parent.ID != q.ParentID || parent.Number != ref.Issue {
		return fmt.Errorf("delivery parent identity changed")
	}
	gateway, err := p.deliveryGateway(ctx, q.Parent)
	if err != nil {
		return err
	}
	ok := gateway != nil
	if !ok {
		return fmt.Errorf("delivery forge unavailable")
	}
	repo, err := p.deliveryCodeRepository(ctx, q.Parent)
	if err != nil {
		return err
	}
	pr, err := gateway.GetDeliveryPR(ctx, repo, q.PRNumber)
	if err != nil {
		return err
	}
	if err = deliveryPRMatches(q, pr, repo); err != nil {
		return p.deliveryRefusal(ctx, c, err.Error())
	}
	if !pr.Merged {
		head, err := gateway.GetDeliveryHead(ctx, repo, q.Branch)
		if err != nil {
			return err
		}
		if head != q.Head {
			return p.deliveryRefusal(ctx, c, "Task branch head changed; request /deliver for a new proposal and approval.")
		}
	}
	if c.SourceID != "" {
		err = p.db.ApproveDeliverySource(ctx, q.Key, c.SourceID)
	} else {
		err = p.db.ApproveDelivery(ctx, q.Key, c.CommentID, c.ActorID)
	}
	if err != nil {
		return err
	}
	q, err = p.db.GetDelivery(ctx, q.Key)
	if err != nil {
		return err
	}
	return p.deliverDelivery(ctx, &q)
}
func (p *StoryProgress) lockDeliverySession(ctx context.Context, s store.Session) (func() error, bool, error) {
	owner := "delivery:" + s.TaskRef
	acquired, err := p.db.AcquireSessionLock(ctx, s.ID, owner)
	if err != nil || !acquired {
		return nil, false, err
	}
	release := func() error { return p.db.ReleaseSessionLock(context.WithoutCancel(ctx), s.ID, owner) }
	if s.State == store.SessionRunning {
		return nil, false, release()
	}
	return release, true, nil
}
func (p *StoryProgress) deliveryRefusal(ctx context.Context, c DeliveryControl, detail string) error {
	identity := fmt.Sprintf("%s/%d", c.Parent, c.CommentID)
	if c.SourceID != "" {
		identity = c.Parent + "/" + c.SourceID
	}
	key := agent.StoryDigest([]byte(identity))
	return p.deliveryNotice(ctx, c.Parent, key, "refused", fmt.Sprintf("Delivery control refused for comment %d: %s No merge approval was recorded. Submit a corrected explicit control.", c.CommentID, detail))
}

// DeliveryReadiness enumerates exact current blockers and historical exceptions.
// Waivers cover substantive obligations without changing their original facts.
type DeliveryReadiness struct{ Blockers, Exceptions []string }

func (r DeliveryReadiness) String() string {
	var b strings.Builder
	b.WriteString("Every attached child and managed acceptance/evidence were assessed.\n")
	for _, e := range r.Exceptions {
		fmt.Fprintf(&b, "Owner exception: %s\n", e)
	}
	for _, s := range r.Blockers {
		fmt.Fprintf(&b, "Blocked: %s\n", s)
	}
	if len(r.Blockers) == 0 {
		b.WriteString("Ready for a concrete PR proposal; integration still requires explicit approval and remote confirmation.\n")
	}
	return b.String()
}

// Summary bounds outward reports while naming the immutable full report digest.
func (r DeliveryReadiness) Summary() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Readiness: %d blockers; %d exact owner exceptions. Full report SHA256: %s.\n", len(r.Blockers), len(r.Exceptions), agent.StoryDigest([]byte(r.String())))
	for _, group := range []struct {
		name   string
		values []string
	}{{"Blocker", r.Blockers}, {"Exception scope", r.Exceptions}} {
		for i, value := range group.values {
			if i == 8 {
				fmt.Fprintf(&b, "%d more %s entries are retained in the full immutable report.\n", len(group.values)-i, group.name)
				break
			}
			key, _, _ := strings.Cut(value, ": ")
			if len(key) > 192 {
				key = key[:192] + "…"
			}
			fmt.Fprintf(&b, "%s: %s\n", group.name, key)
		}
	}
	if len(r.Blockers) == 0 {
		b.WriteString("Ready for a concrete PR proposal; explicit merge approval and actual integration confirmation remain required.\n")
	}
	return b.String()
}
func (p *StoryProgress) retainDeliveryReadiness(ctx context.Context, parent string, r DeliveryReadiness) (string, error) {
	f, err := p.db.GetManagedFeature(ctx, parent)
	if err != nil {
		return "", err
	}
	full := r.String()
	key := agent.StoryDigest([]byte(fmt.Sprintf("%s\x00%d\x00%s", parent, f.Generation, full)))
	if err = p.db.RetainDeliveryReadiness(ctx, key, parent, f.Generation, full); err != nil {
		return "", err
	}
	return r.Summary() + "Full exact readiness record: " + key + ".\n", nil
}

// DeliveryReadiness assesses factory facts and all attached children; external
// closure alone never supplies a managed acceptance decision.
func (p *StoryProgress) DeliveryReadiness(ctx context.Context, parent string) (DeliveryReadiness, error) {
	var out DeliveryReadiness
	f, err := p.db.GetManagedFeature(ctx, parent)
	if err != nil {
		return out, err
	}
	if f.WorkflowVersion != store.WorkflowSeparated {
		return out, fmt.Errorf("delivery requires separated workflow")
	}
	exceptions, err := p.db.ListReadinessExceptions(ctx, parent, f.Generation)
	if err != nil {
		return out, err
	}
	waived := map[string]string{}
	for _, e := range exceptions {
		if e.Finding.WaiverReason != "" {
			waived[e.Name] = e.Finding.WaiverReason
		}
	}
	obligation := func(key, detail string) {
		if reason, ok := waived[key]; ok {
			out.Exceptions = append(out.Exceptions, key+": "+reason+" (original: "+detail+")")
		} else {
			out.Blockers = append(out.Blockers, key+": "+detail)
		}
	}
	if !f.EngineeringSucceeded {
		obligation("engineering_save", "Aggregate engineering/test and successful task-branch save evidence is incomplete.")
	}
	refs, err := p.db.ListApprovedStoryRevisions(ctx, parent, f.Generation)
	if err != nil {
		return out, err
	}
	if len(refs) == 0 {
		out.Blockers = append(out.Blockers, "Approved managed scope is empty.")
	}
	for _, rev := range refs {
		d, err := p.db.LatestAcceptance(ctx, parent, f.Generation, rev.Key)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return out, err
		}
		if errors.Is(err, store.ErrNotFound) || !d.Accepted || !d.Delivered {
			obligation("acceptance:"+rev.Key, "No fully projected accepted/owner-waived decision for the approved story.")
		}
		evidence, err := p.exceptionCheckEvidence(ctx, parent, f.Generation, rev)
		if err != nil {
			return out, err
		}
		if evidence.Finding.Status != "satisfied" {
			obligation("evidence:"+rev.Key, evidence.Finding.Evidence)
		}
	}
	gateway, ok := p.forge.(interface {
		ListSubIssues(context.Context, github.Repo, int) ([]github.Issue, error)
	})
	if !ok {
		return out, fmt.Errorf("attached-child enumeration unavailable")
	}
	ref, err := naming.ParseRef(parent)
	if err != nil {
		return out, err
	}
	children, err := gateway.ListSubIssues(ctx, github.RepoFromRef(ref), ref.Issue)
	if err != nil {
		return out, err
	}
	seen := map[int64]bool{}
	for _, child := range children {
		if child.ID < 1 || child.Number < 1 || seen[child.ID] {
			return out, fmt.Errorf("invalid or duplicate attached-child identity")
		}
		seen[child.ID] = true
		if child.State != "closed" {
			obligation(fmt.Sprintf("child:%d", child.ID), fmt.Sprintf("Attached child #%d is %s.", child.Number, child.State))
		}
	}
	return out, nil
}

func (p *StoryProgress) pollDelivery(ctx context.Context, parent string) (err error) {
	f, err := p.db.GetManagedFeature(ctx, parent)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if f.WorkflowVersion != store.WorkflowSeparated {
		return nil
	}
	// Installed adapters without this optional capability continue to process
	// acceptance; explicit delivery reports the unavailable service.
	if p.code == nil {
		return nil
	}
	s, err := p.db.GetSessionByTaskRef(ctx, parent)
	if err != nil {
		return err
	}
	unlock, acquired, err := p.lockDeliverySession(ctx, s)
	if err != nil || !acquired {
		return err
	}
	defer func() { err = errors.Join(err, unlock()) }()
	all, err := p.db.ListDeliveries(ctx, parent)
	if err != nil {
		return err
	}
	for _, q := range all {
		if q.Superseded || q.Reported {
			continue
		}
		if err = p.deliverDelivery(ctx, &q); err != nil {
			return err
		}
		if q.Closed {
			return nil
		}
	}
	return p.prepareDelivery(ctx, parent, false)
}
func (p *StoryProgress) prepareDelivery(ctx context.Context, parent string, explicit bool) error {
	ready, err := p.DeliveryReadiness(ctx, parent)
	if err != nil {
		return err
	}
	summary, err := p.retainDeliveryReadiness(ctx, parent, ready)
	if err != nil {
		return err
	}
	if len(ready.Blockers) > 0 {
		if !explicit {
			return nil
		}
		key := agent.StoryDigest([]byte(parent + ready.String()))
		return p.deliveryNotice(ctx, parent, key, "readiness", summary)
	}
	gateway, err := p.deliveryGateway(ctx, parent)
	if err != nil {
		return err
	}
	ok := gateway != nil
	if !ok {
		return fmt.Errorf("delivery forge unavailable")
	}
	renderer, ok := p.renderer.(deliveryRenderer)
	if !ok {
		return fmt.Errorf("delivery renderer unavailable")
	}
	f, err := p.db.GetManagedFeature(ctx, parent)
	if err != nil {
		return err
	}
	s, err := p.db.GetSessionByTaskRef(ctx, parent)
	if err != nil {
		return err
	}
	if s.Branch == "" || s.Branch == "main" {
		return fmt.Errorf("no saved task branch for delivery")
	}
	ref, err := naming.ParseRef(parent)
	if err != nil {
		return err
	}
	ticketRepo := github.RepoFromRef(ref)
	repo, err := p.deliveryCodeRepository(ctx, parent)
	if err != nil {
		return err
	}
	remote, err := p.forge.GetIssue(ctx, ticketRepo, ref.Issue)
	if err != nil {
		return err
	}
	if remote.ID < 1 || remote.Number != ref.Issue {
		return fmt.Errorf("invalid delivery parent identity")
	}
	if remote.State == "closed" {
		return nil
	}
	head, err := gateway.GetDeliveryHead(ctx, repo, s.Branch)
	if err != nil {
		return err
	}
	key := agent.StoryDigest([]byte(fmt.Sprintf("%s\x00%d\x00%s\x00%s\x00main\x00%s", parent, f.Generation, s.Branch, head, ready.String())))
	old, err := p.db.GetDelivery(ctx, key)
	if err == nil {
		return p.deliverDelivery(ctx, &old)
	}
	if !errors.Is(err, store.ErrNotFound) {
		return err
	}
	title := fmt.Sprintf("Deliver issue %d to main", ref.Issue)
	body := fmt.Sprintf("Parent: %s\nGeneration: %d\nTask branch: %s\nAssessed head: %s\nTarget: main\n\n%s\nThis PR contains task-branch work. Parent closure is a separate factory action after confirmed integration. No merge is authorized by this proposal.\n\n%s", parent, f.Generation, s.Branch, head, summary, deliveryMarker(key))
	t, b, err := renderer.RenderDeliveryContent(ctx, title, body)
	if err != nil {
		return err
	}
	q := store.DeliveryProposal{Key: key, Parent: parent, Generation: f.Generation, ParentID: remote.ID, Branch: s.Branch, Head: head, Target: "main", Readiness: ready.String(), Title: t.String(), Body: b.String()}
	if err = p.db.RetainDelivery(ctx, q); err != nil {
		return err
	}
	return p.deliverDelivery(ctx, &q)
}
func deliveryMarker(key string) string { return "<!-- litefactory:delivery:" + key + " -->" }
func deliveryPRMatches(q store.DeliveryProposal, pr github.DeliveryPR, repo github.Repo) error {
	if !report.DeliveryContentSafe(pr.Title, pr.Body) {
		return fmt.Errorf("PR contains automatic issue-closing directive; ordered closure cannot be guaranteed")
	}
	if pr.ID < 1 || pr.Number < 1 || (q.PRID > 0 && pr.ID != q.PRID) || (q.PRNumber > 0 && pr.Number != q.PRNumber) || pr.Branch != q.Branch || pr.Head != q.Head || pr.Target != "main" || pr.BaseRepo != repo.String() || pr.HeadRepo != repo.String() {
		return fmt.Errorf("PR identity/head/branch/main target changed; fresh delivery proposal and approval required")
	}
	return nil
}
func (p *StoryProgress) deliveryNotice(ctx context.Context, parent, key, stage, detail string) error {
	publisherID, publisherErr := p.ticketPublisherID(ctx, parent)
	if publisherErr != nil {
		return publisherErr
	}
	renderer, ok := p.renderer.(deliveryRenderer)
	if !ok {
		return fmt.Errorf("delivery renderer unavailable")
	}
	ref, err := naming.ParseRef(parent)
	if err != nil {
		return err
	}
	repo := github.RepoFromRef(ref)
	marker := fmt.Sprintf("<!-- litefactory:delivery-notice:%s:%s -->", key, stage)
	comments, err := p.forge.ListCommentsAfter(ctx, repo, ref.Issue, 0)
	if err != nil {
		return err
	}
	for _, c := range comments {
		if !hasTicketNoticeGuard(p.forge) && c.Author.ID == publisherID && exactProgressMarker(c.Body, marker) {
			return nil
		}
	}
	body, err := renderer.RenderDeliveryNotice(ctx, detail, marker)
	if err != nil {
		return err
	}
	c, err := postTicketNotice(ctx, p.forge, marker, parent, publisherID, repo, ref.Issue, body)
	if err != nil {
		return err
	}
	if c.ID < 1 || c.Author.ID != publisherID {
		return fmt.Errorf("delivery report provenance mismatch")
	}
	return nil
}

func (p *StoryProgress) deliverDelivery(ctx context.Context, q *store.DeliveryProposal) (err error) {
	if q.Reported || q.Superseded {
		return nil
	}
	defer func() {
		if err != nil {
			q.Diagnostic = err.Error()
			saveErr := p.db.SaveDeliveryEffects(ctx, *q)
			noticeErr := p.deliveryNotice(ctx, q.Parent, q.Key, "pending", "Delivery remains pending: "+err.Error()+"\nNo successful integration or parent closure is claimed. Proposal: "+q.Key)
			err = errors.Join(err, saveErr, noticeErr)
		}
	}()
	gateway, err := p.deliveryGateway(ctx, q.Parent)
	if err != nil {
		return err
	}
	ok := gateway != nil
	if !ok {
		return fmt.Errorf("delivery forge unavailable")
	}
	renderer, ok := p.renderer.(deliveryRenderer)
	if !ok {
		return fmt.Errorf("delivery renderer unavailable")
	}
	ref, err := naming.ParseRef(q.Parent)
	if err != nil {
		return err
	}
	ticketRepo := github.RepoFromRef(ref)
	parent, err := p.forge.GetIssue(ctx, ticketRepo, ref.Issue)
	if err != nil {
		return err
	}
	if parent.ID != q.ParentID || parent.Number != ref.Issue {
		return fmt.Errorf("delivery parent identity changed")
	}
	repo, err := p.deliveryCodeRepository(ctx, q.Parent)
	if err != nil {
		return err
	}
	feature, err := p.db.GetManagedFeature(ctx, q.Parent)
	if err != nil {
		return err
	}
	if feature.Generation != q.Generation || feature.WorkflowVersion != store.WorkflowSeparated {
		return fmt.Errorf("delivery generation/workflow changed")
	}
	publisherID, err := p.deliveryPublisherID(ctx, q.Parent)
	if err != nil {
		return err
	}
	var pr github.DeliveryPR
	if q.PRNumber > 0 {
		pr, err = gateway.GetDeliveryPR(ctx, repo, q.PRNumber)
		if err != nil {
			return err
		}
		if pr.ID != q.PRID {
			return fmt.Errorf("delivery PR identity changed")
		}
	}
	if !pr.Merged {
		head, err := gateway.GetDeliveryHead(ctx, repo, q.Branch)
		if err != nil {
			return err
		}
		if head != q.Head {
			q.Superseded = true
			q.Diagnostic = "Task branch head changed; fresh proposal and approval required. Historical story acceptance remains intact."
			if err = p.db.SaveDeliveryEffects(ctx, *q); err != nil {
				return err
			}
			return p.deliveryNotice(ctx, q.Parent, q.Key, "superseded", q.Diagnostic)
		}
	}
	ready, err := p.DeliveryReadiness(ctx, q.Parent)
	if err != nil {
		return err
	}
	if len(ready.Blockers) > 0 {
		summary, retainErr := p.retainDeliveryReadiness(ctx, q.Parent, ready)
		if retainErr != nil {
			return retainErr
		}
		return fmt.Errorf("readiness blocked: %s", summary)
	}
	if q.PRNumber == 0 {
		prs, err := gateway.ListDeliveryPRs(ctx, repo, q.Branch)
		if err != nil {
			return err
		}
		previous, err := p.db.ListDeliveries(ctx, q.Parent)
		if err != nil {
			return err
		}
		matches := 0
		for _, candidate := range prs {
			if candidate.Branch != q.Branch || candidate.HeadRepo != repo.String() || candidate.CreatorID != publisherID {
				continue
			}
			known := exactProgressMarker(candidate.Body, deliveryMarker(q.Key))
			for _, old := range previous {
				if (old.PRID > 0 && old.PRID == candidate.ID) || exactProgressMarker(candidate.Body, deliveryMarker(old.Key)) {
					known = true
				}
			}
			if !known {
				continue
			}
			if candidate.Target != q.Target || candidate.BaseRepo != repo.String() {
				return fmt.Errorf("existing factory PR target changed")
			}
			if candidate.State == "closed" && !candidate.Merged {
				continue
			}
			pr = candidate
			matches++
		}
		if matches > 1 {
			return fmt.Errorf("ambiguous duplicate delivery PRs")
		}
		if matches == 0 {
			if q.CreateAttempted {
				return fmt.Errorf("uncertain PR creation; awaiting verified factory marker reconciliation before retry")
			}
			t, b, err := renderer.RenderDeliveryContent(ctx, q.Title, q.Body)
			if err != nil {
				return err
			}
			if t.String() != q.Title || b.String() != q.Body {
				return fmt.Errorf("delivery content changed during rendering; new proposal required")
			}
			q.CreateAttempted = true
			if err = p.db.SaveDeliveryEffects(ctx, *q); err != nil {
				return err
			}
			pr, err = gateway.CreateDeliveryPR(ctx, repo, q.Branch, q.Target, t, b)
			if err != nil {
				return err
			}
		}
		if pr.CreatorID != publisherID {
			return fmt.Errorf("delivery PR creator mismatch")
		}
		if err = deliveryPRMatches(*q, pr, repo); err != nil {
			return err
		}
		q.PRID = pr.ID
		q.PRNumber = pr.Number
		if err = p.db.SaveDeliveryEffects(ctx, *q); err != nil {
			return err
		}
		pr, err = gateway.GetDeliveryPR(ctx, repo, q.PRNumber)
		if err != nil {
			return err
		}
	}
	if err = deliveryPRMatches(*q, pr, repo); err != nil {
		return err
	}
	if !q.Proposed {
		detail := fmt.Sprintf("Delivery proposal %s\n\nPR: https://github.com/%s/pull/%d (numeric ID %d)\n%s\n\nApprove this exact PR/head/main with `/merge %d %s %d %s main`. This separate approval authorizes integration and parent closure only after confirmed successful integration.", q.Key, repo.String(), q.PRNumber, q.PRID, q.Body, q.Generation, q.Key, q.PRNumber, q.Head)
		if err = p.proposeDeliveryNotice(ctx, *q, detail); err != nil {
			return err
		}
		q.Proposed = true
		if err = p.db.SaveDeliveryEffects(ctx, *q); err != nil {
			return err
		}
	}
	if q.ApprovalComment == 0 {
		return nil
	}
	if !pr.Merged {
		if pr.State != "open" {
			return fmt.Errorf("approved PR is not open and has not merged")
		}
		// Re-read readiness and the PR immediately before the atomic head-guarded
		// merge; owner waivers never substitute for actual remote integration.
		ready, err = p.DeliveryReadiness(ctx, q.Parent)
		if err != nil {
			return err
		}
		if len(ready.Blockers) > 0 {
			summary, retainErr := p.retainDeliveryReadiness(ctx, q.Parent, ready)
			if retainErr != nil {
				return retainErr
			}
			return fmt.Errorf("merge readiness blocked: %s", summary)
		}
		pr, err = gateway.GetDeliveryPR(ctx, repo, q.PRNumber)
		if err != nil {
			return err
		}
		if err = deliveryPRMatches(*q, pr, repo); err != nil {
			return err
		}
		if !pr.Merged {
			q.MergeAttempted = true
			if err = p.db.SaveDeliveryEffects(ctx, *q); err != nil {
				return err
			}
			if err = gateway.MergeDeliveryPR(ctx, repo, q.PRNumber, q.Head); err != nil {
				return err
			}
		}
	}
	// Only this independent observation can unlock the close intent. Neither a
	// successful mutation response nor a waived criterion is integration proof.
	pr, err = gateway.GetDeliveryPR(ctx, repo, q.PRNumber)
	if err != nil {
		return err
	}
	if err = deliveryPRMatches(*q, pr, repo); err != nil {
		return err
	}
	if !pr.Merged || pr.State != "closed" || len(pr.MergeSHA) != 40 || strings.Trim(pr.MergeSHA, "0123456789abcdef") != "" {
		return fmt.Errorf("remote successful main integration is not confirmed")
	}
	q.MergeConfirmed = true
	q.MergeSHA = pr.MergeSHA
	if err = p.db.SaveDeliveryEffects(ctx, *q); err != nil {
		return err
	}
	parent, err = p.forge.GetIssue(ctx, ticketRepo, ref.Issue)
	if err != nil {
		return err
	}
	if parent.ID != q.ParentID {
		return fmt.Errorf("delivery parent identity changed")
	}
	if parent.State != "closed" {
		q.CloseAttempted = true
		if err = p.db.SaveDeliveryEffects(ctx, *q); err != nil {
			return err
		}
		closer, ok := p.forge.(interface {
			CloseDeliveredParent(context.Context, github.Repo, int, int64) error
		})
		if !ok {
			return fmt.Errorf("ticket closure unavailable")
		}
		if err = closer.CloseDeliveredParent(ctx, ticketRepo, ref.Issue, q.ParentID); err != nil {
			return err
		}
	}
	parent, err = p.forge.GetIssue(ctx, ticketRepo, ref.Issue)
	if err != nil {
		return err
	}
	if parent.ID != q.ParentID || parent.State != "closed" {
		return fmt.Errorf("parent closure not confirmed")
	}
	q.Closed = true
	if err = p.db.SaveDeliveryEffects(ctx, *q); err != nil {
		return err
	}
	detail := fmt.Sprintf("Delivery completed: PR https://github.com/%s/pull/%d integrated approved head %s into main; confirmed merge %s. Parent #%d is confirmed closed. Numeric owner %d approved in comment %d. Historical acceptance remains bound to its assessed snapshots.", repo.String(), q.PRNumber, q.Head, q.MergeSHA, ref.Issue, q.ActorID, q.ApprovalComment)
	if err = p.deliveryNotice(ctx, q.Parent, q.Key, "complete", detail); err != nil {
		return err
	}
	q.Reported = true
	q.Diagnostic = ""
	return p.db.SaveDeliveryEffects(ctx, *q)
}

// deliveryCodeRepository reads the registered clone independently of ticket
// coordinates. Historical delivery proposals have no repository column, so a
// contradictory registration cannot silently retarget their saved PR effects.
func (p *StoryProgress) deliveryCodeRepository(ctx context.Context, parent string) (github.Repo, error) {
	session, err := p.db.GetSessionByTaskRef(ctx, parent)
	if err != nil {
		return github.Repo{}, err
	}
	snapshot, err := p.db.GetExecutionSnapshot(ctx, session.ID)
	if err == nil {
		repo := github.Repo{Owner: snapshot.CodeTarget.Owner, Name: snapshot.CodeTarget.Name}
		if snapshot.CodeTarget.Forge != "github" {
			return github.Repo{}, fmt.Errorf("retained code provider is unsupported")
		}
		return repo, repo.Validate()
	}
	if !errors.Is(err, store.ErrNotFound) {
		return github.Repo{}, err
	}
	registered, err := p.db.GetRepo(ctx, session.RepoID)
	if err != nil {
		return github.Repo{}, err
	}
	ref, err := naming.ParseRef(parent)
	if err != nil {
		return github.Repo{}, err
	}
	repo := github.Repo{Owner: registered.Owner, Name: registered.Name}
	if registered.Forge != "github" || !strings.EqualFold(repo.String(), ref.Owner+"/"+ref.Repo) {
		return github.Repo{}, fmt.Errorf("registered code repository conflicts with retained delivery identity")
	}
	return repo, repo.Validate()
}

// deliveryGateway scopes publication to the original parent's retained code actor.
func (p *StoryProgress) deliveryGateway(ctx context.Context, parent string) (CodeDelivery, error) {
	gateway := p.code
	if scoped, ok := gateway.(interface{ ForSession(int64) CodeDelivery }); ok {
		session, err := p.db.GetSessionByTaskRef(ctx, parent)
		if err != nil {
			return nil, err
		}
		return scoped.ForSession(session.ID), nil
	}
	return gateway, nil
}
