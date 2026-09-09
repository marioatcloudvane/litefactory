package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
)

// OwnerExceptionRequest is the bounded explicit factory control. Generation is
// mandatory; selectors are exact stable keys, never free-form interpretations.
type OwnerExceptionRequest struct {
	Generation int      `json:"generation"`
	Selectors  []string `json:"selectors"`
	Reason     string   `json:"reason"`
}

// ParseOwnerException parses a bounded generation, exact selectors and required reason.
func ParseOwnerException(body string) (OwnerExceptionRequest, error) {
	var r OwnerExceptionRequest
	if len(body) > 8192 {
		return r, fmt.Errorf("exception command exceeds 8192 bytes")
	}
	head, reason, ok := strings.Cut(strings.TrimSpace(body), " -- ")
	fields := strings.Fields(head)
	if !ok || len(fields) != 3 || fields[0] != "/waive" || strings.TrimSpace(reason) == "" || len(reason) > 4096 {
		return r, fmt.Errorf("use /waive <generation> <selector[,selector...]> -- <reason>")
	}
	n, err := strconv.Atoi(fields[1])
	if err != nil || n < 1 {
		return r, fmt.Errorf("generation must be a positive integer")
	}
	r.Generation = n
	r.Reason = strings.TrimSpace(reason)
	seen := map[string]bool{}
	for _, s := range strings.Split(fields[2], ",") {
		decoded, err := url.PathUnescape(s)
		if err != nil || strings.ContainsAny(decoded, "\r\n\x00") {
			return r, fmt.Errorf("invalid escaped selector")
		}
		s = decoded
		if s == "" || len(s) > 256 || seen[s] || len(seen) >= 128 {
			return r, fmt.Errorf("invalid or duplicate exception selector")
		}
		if s != "all" && s != "acceptance" && s != "readiness" && !strings.HasPrefix(s, "story:") && !strings.HasPrefix(s, "criterion:") && !strings.HasPrefix(s, "review:") && !strings.HasPrefix(s, "tests:") && !strings.HasPrefix(s, "child:") {
			return r, fmt.Errorf("unknown exception selector %q", s)
		}
		seen[s] = true
		r.Selectors = append(r.Selectors, s)
	}
	return r, nil
}

// OwnerExceptionControl carries numeric owner identity and the explicit source comment.
type OwnerExceptionControl struct {
	Parent                      string
	CommentID, ActorID, OwnerID int64
	Body                        string
}
type exceptionRenderer interface {
	RenderOwnerException(context.Context, report.OwnerExceptionNotice) (redact.Body, error)
}
type exceptionChildren interface {
	ListSubIssues(context.Context, github.Repo, int) ([]github.Issue, error)
}

// ApplyOwnerException is called only for an admitted owner comment. It checks
// numeric authority again, takes the shared parent ownership, persists scope,
// then uses the same ordered per-story projection as factual acceptance.
func (p *StoryProgress) ApplyOwnerException(ctx context.Context, c OwnerExceptionControl) (err error) {
	if c.OwnerID < 1 || c.ActorID != c.OwnerID || c.CommentID < 1 {
		return fmt.Errorf("exception requires authenticated numeric owner comment")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	release, err := p.db.LockStoryProgress(ctx, c.Parent, p.lockDir)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, release()) }()
	request, err := ParseOwnerException(c.Body)
	if err != nil {
		return p.reportException(ctx, c, 0, err.Error(), false)
	}
	raw, err := json.Marshal(request)
	if err != nil {
		return err
	}
	old, err := p.db.GetOwnerException(ctx, c.Parent, c.CommentID)
	switch {
	case err == nil:
		if old.ActorID != c.ActorID || old.RequestJSON != string(raw) {
			return fmt.Errorf("exception replay changed authority or content")
		}
	case errors.Is(err, store.ErrNotFound):
		feature, err := p.db.GetManagedFeature(ctx, c.Parent)
		if err != nil {
			return err
		}
		if feature.WorkflowVersion != store.WorkflowSeparated || feature.Generation != request.Generation {
			return p.reportException(ctx, c, 0, "Exception refused: select the current separated-workflow generation.", false)
		}
		// Materialize any retained original assessment before expanding exceptions.
		assessments, err := p.db.ListUnprojectedValidationAssessments(ctx)
		if err != nil {
			return err
		}
		for _, a := range assessments {
			if a.Parent == c.Parent {
				if err := retainAcceptance(ctx, p.db, a); err != nil {
					return err
				}
			}
		}
		decisions, scope, err := p.expandOwnerException(ctx, c.Parent, request)
		if err != nil {
			return p.reportException(ctx, c, 0, "Exception refused: "+err.Error(), false)
		}
		expansion, err := json.Marshal(scope)
		if err != nil {
			return err
		}
		e := store.OwnerException{Parent: c.Parent, Generation: request.Generation, CommentID: c.CommentID, ActorID: c.ActorID, RequestJSON: string(raw), ExpansionJSON: string(expansion)}
		if err := p.db.RetainOwnerException(ctx, e, decisions); err != nil {
			return err
		}
		old, err = p.db.GetOwnerException(ctx, c.Parent, c.CommentID)
		if err != nil {
			return err
		}
	default:
		return err
	}
	// A remote failure leaves this durable decision and original comment pending.
	if err := p.deliverAcceptance(ctx, c.Parent); err != nil {
		return err
	}
	if old.ReportCommentID > 0 {
		return nil
	}
	var expansion []store.ExceptionObligation
	if err := json.Unmarshal([]byte(old.ExpansionJSON), &expansion); err != nil {
		return err
	}
	counts := map[string]int{}
	for _, o := range expansion {
		counts[o.Kind]++
	}
	detail := fmt.Sprintf("Reason: %s\nImmutable owner exception %d; generation %d. Exact expansion digest: %s.\nExpanded scope: %d criteria, %d review obligations, %d readiness obligations.\nFull exact identities, original findings and evidence attribution are retained in exception record %d. Per-story projection comments show criterion/review exceptions. Replay cannot expand this scope; later children require a new exception.", request.Reason, old.ID, old.Generation, agent.StoryDigest([]byte(old.ExpansionJSON)), counts["criterion"], counts["review"], counts["readiness"], old.ID)
	return p.reportException(ctx, c, old.ID, detail, true)
}

func (p *StoryProgress) expandOwnerException(ctx context.Context, parent string, r OwnerExceptionRequest) ([]store.AcceptanceDecision, []store.ExceptionObligation, error) {
	revisions, err := p.db.ListApprovedStoryRevisions(ctx, parent, r.Generation)
	if err != nil {
		return nil, nil, err
	}
	if len(revisions) == 0 {
		return nil, nil, fmt.Errorf("no approved frozen scope")
	}
	selected := map[string]bool{}
	used := map[string]bool{}
	for _, s := range r.Selectors {
		selected[s] = true
	}
	match := func(keys ...string) bool {
		yes := false
		for _, key := range keys {
			if selected[key] {
				used[key] = true
				yes = true
			}
		}
		return yes
	}
	var decisions []store.AcceptanceDecision
	var scope []store.ExceptionObligation
	for _, rev := range revisions {
		d, err := p.db.LatestAcceptance(ctx, parent, r.Generation, rev.Key)
		var findings store.AcceptanceFindings
		switch {
		case errors.Is(err, store.ErrNotFound):
			names, err := planbrief.AcceptanceCriteria(rev.Body)
			if err != nil {
				return nil, nil, err
			}
			d = store.AcceptanceDecision{Parent: parent, Generation: r.Generation, Story: rev.Key, IssueNumber: rev.IssueNumber, IssueID: rev.IssueID, ParentID: rev.ParentID, Revision: rev.Hash, FrozenBody: rev.Body}
			for _, name := range names {
				findings.Criteria = append(findings.Criteria, store.AcceptanceFinding{Name: name, Status: "unverified", Classification: "blocking", Evidence: "No retained validation assessment; execution or passage is not claimed."})
			}
			for _, name := range []string{"code", "architecture", "plan"} {
				findings.Reviews = append(findings.Reviews, store.AcceptanceFinding{Name: name, Status: "unverified", Classification: "blocking", Evidence: "No retained review assessment."})
			}
		case err != nil:
			return nil, nil, err
		default:
			if err := json.Unmarshal([]byte(d.FindingsJSON), &findings); err != nil {
				return nil, nil, err
			}
		}
		if d.Revision != rev.Hash {
			return nil, nil, fmt.Errorf("retained acceptance revision mismatch")
		}
		changed := false
		for _, group := range []struct {
			kind     string
			findings []store.AcceptanceFinding
		}{{"criterion", findings.Criteria}, {"review", findings.Reviews}} {
			for i := range group.findings {
				f := &group.findings[i]
				yes := match("all", "acceptance", "story:"+rev.Key, group.kind+":"+rev.Key+":"+f.Name)
				if !yes || d.Accepted || f.WaiverReason != "" || f.Status == "satisfied" {
					continue
				}
				scope = append(scope, store.ExceptionObligation{Story: rev.Key, IssueID: rev.IssueID, Kind: group.kind, Name: f.Name, Revision: d.Revision, Snapshot: d.Snapshot, Finding: *f})
				f.WaiverReason = r.Reason
				changed = true
			}
		}
		if changed {
			data, err := json.Marshal(findings)
			if err != nil {
				return nil, nil, err
			}
			d.FindingsJSON = string(data)
			d.Accepted = findings.Accepted()
			decisions = append(decisions, d)
		}
		if match("all", "readiness", "tests:"+rev.Key) {
			o, err := p.exceptionCheckEvidence(ctx, parent, r.Generation, rev)
			if err != nil {
				return nil, nil, err
			}
			o.Finding.WaiverReason = r.Reason
			scope = append(scope, o)
		}
		if match("all", "readiness") && !d.Accepted {
			scope = append(scope, store.ExceptionObligation{Story: rev.Key, IssueID: rev.IssueID, Kind: "readiness", Name: "acceptance:" + rev.Key, Revision: rev.Hash, Snapshot: d.Snapshot, Finding: store.AcceptanceFinding{Status: "unverified", Evidence: "Managed acceptance decision was not satisfied at expansion.", WaiverReason: r.Reason}})
		}
	}
	if selected["all"] || selected["readiness"] {
		f, err := p.db.GetManagedFeature(ctx, parent)
		if err != nil {
			return nil, nil, err
		}
		if !f.EngineeringSucceeded {
			scope = append(scope, store.ExceptionObligation{Kind: "readiness", Name: "engineering_save", Finding: store.AcceptanceFinding{Status: "unverified", Evidence: "Aggregate engineering and successful save evidence is incomplete; no successful execution or save is fabricated.", WaiverReason: r.Reason}})
		}
	}
	needChildren := selected["all"] || selected["readiness"]
	for s := range selected {
		if strings.HasPrefix(s, "child:") {
			needChildren = true
		}
	}
	if needChildren {
		gateway, ok := p.forge.(exceptionChildren)
		if !ok {
			return nil, nil, fmt.Errorf("attached-child enumeration unavailable")
		}
		ref, err := naming.ParseRef(parent)
		if err != nil {
			return nil, nil, err
		}
		children, err := gateway.ListSubIssues(ctx, github.RepoFromRef(ref), ref.Issue)
		if err != nil {
			return nil, nil, err
		}
		for _, child := range children {
			key := fmt.Sprintf("child:%d", child.ID)
			if child.ID < 1 {
				return nil, nil, fmt.Errorf("invalid attached-child identity")
			}
			if match("all", "readiness", key) && child.State != "closed" {
				scope = append(scope, store.ExceptionObligation{Kind: "readiness", Name: key, IssueID: child.ID, Finding: store.AcceptanceFinding{Status: "unmet", Evidence: "Attached child was open at expansion; remote state is unchanged.", WaiverReason: r.Reason}})
			}
		}
	}
	for _, s := range r.Selectors {
		if !used[s] {
			return nil, nil, fmt.Errorf("selector %q does not identify current frozen scope or attached children", s)
		}
	}
	return decisions, scope, nil
}

func (p *StoryProgress) reportException(ctx context.Context, c OwnerExceptionControl, id int64, detail string, accepted bool) error {
	renderer, ok := p.renderer.(exceptionRenderer)
	if !ok {
		return fmt.Errorf("exception renderer unavailable")
	}
	ref, err := naming.ParseRef(c.Parent)
	if err != nil {
		return err
	}
	repo := github.RepoFromRef(ref)
	n := report.OwnerExceptionNotice{Parent: c.Parent, CommentID: c.CommentID, ActorID: c.ActorID, Detail: detail, Accepted: accepted}
	comments, err := p.forge.ListCommentsAfter(ctx, repo, ref.Issue, 0)
	if err != nil {
		return err
	}
	var commentID int64
	for _, comment := range comments {
		if comment.Author.ID == p.botID && exactProgressMarker(comment.Body, n.Marker()) {
			commentID = comment.ID
			break
		}
	}
	if commentID == 0 {
		body, err := renderer.RenderOwnerException(ctx, n)
		if err != nil {
			return err
		}
		comment, err := p.forge.PostComment(ctx, repo, ref.Issue, body)
		if err != nil {
			return err
		}
		if comment.ID < 1 || comment.Author.ID != p.botID {
			return fmt.Errorf("exception report provenance mismatch")
		}
		commentID = comment.ID
	}
	if id > 0 {
		return p.db.SaveOwnerExceptionReport(ctx, id, commentID)
	}
	return nil
}

func (p *StoryProgress) exceptionCheckEvidence(ctx context.Context, parent string, generation int, rev store.ApprovedStoryRevision) (store.ExceptionObligation, error) {
	o := store.ExceptionObligation{Story: rev.Key, IssueID: rev.IssueID, Kind: "readiness", Name: "evidence:" + rev.Key, Revision: rev.Hash, Finding: store.AcceptanceFinding{Name: "executed_checks", Status: "unverified", Classification: "blocking", Evidence: "No retained initial engineering/check evidence."}}
	retained, err := p.db.EngineeringEvidence(ctx, parent, generation, rev.Key)
	if errors.Is(err, store.ErrNotFound) {
		return o, nil
	}
	if err != nil {
		return o, err
	}
	o.EvidenceRunID = retained.RunID
	o.EvidenceDigest = retained.Digest
	v := agent.ValidationStory{Story: agent.ApprovedStory{Key: rev.Key, IssueID: rev.IssueID, Revision: rev.Hash}, EvidenceStatus: "unverified"}
	attachValidationEvidence(&v, retained, store.ManagedFeature{ParentTask: parent, Generation: generation})
	o.Finding.Status = v.EvidenceStatus
	o.Finding.Evidence = v.EvidenceReason
	o.Snapshot = v.Snapshot
	if v.Evidence != nil {
		for _, check := range v.Evidence.Tests {
			status := "failed"
			if check.Passed {
				status = "satisfied"
			}
			o.Checks = append(o.Checks, store.AcceptanceFinding{Name: check.Name, Status: status, Evidence: check.Evidence})
		}
	}
	if o.Finding.Evidence == "" {
		o.Finding.Evidence = "Retained executed check results; the exception does not change them."
	}
	return o, nil
}
