// Package prevention collects bounded bug evidence and retains advisory standards analysis.
package prevention

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
)

// Limits can only lower the settled invocation ceilings.
type Limits struct {
	Issues          int   `json:"issues"`
	Fixes           int   `json:"fixes"`
	Requests        int   `json:"requests"`
	PageSize        int   `json:"page_size"`
	ResponseBytes   int64 `json:"response_bytes"`
	SourceBytes     int64 `json:"source_bytes"`
	EvidenceBytes   int   `json:"evidence_bytes"`
	DurationSeconds int   `json:"duration_seconds"`
	Turns           int   `json:"turns"`
	Corrections     int   `json:"corrections"`
	Proposals       int   `json:"proposals"`
}

// DefaultLimits returns the approved finite ceilings.
func DefaultLimits() Limits {
	return Limits{Issues: 5, Fixes: 5, Requests: 30, PageSize: 20, ResponseBytes: 1 << 20, SourceBytes: 4 << 20, EvidenceBytes: 256 << 10, DurationSeconds: 600, Turns: 24, Corrections: 3, Proposals: 5}
}

// Validate refuses increases and nonpositive limits.
func (l Limits) Validate() error {
	d := DefaultLimits()
	values := [][2]int64{{int64(l.Issues), int64(d.Issues)}, {int64(l.Fixes), int64(d.Fixes)}, {int64(l.Requests), int64(d.Requests)}, {int64(l.PageSize), int64(d.PageSize)}, {l.ResponseBytes, d.ResponseBytes}, {l.SourceBytes, d.SourceBytes}, {int64(l.EvidenceBytes), int64(d.EvidenceBytes)}, {int64(l.DurationSeconds), int64(d.DurationSeconds)}, {int64(l.Turns), int64(d.Turns)}, {int64(l.Corrections), int64(d.Corrections)}, {int64(l.Proposals), int64(d.Proposals)}}
	for _, v := range values {
		if v[0] < 1 || v[0] > v[1] {
			return errors.New("analysis limits must be positive and at or below defaults")
		}
	}
	return nil
}

// Scope declares one repository and an explicit finite issue selection.
type Scope struct {
	RequestKey string `json:"request_key"`
	Repository string `json:"repository"`
	Issues     []int  `json:"issues"`
	Limits     Limits `json:"limits"`
}

// Validate checks source identities and finite limits before collection.
func (s Scope) Validate() error {
	parts := strings.Split(s.Repository, "/")
	if len(parts) != 2 {
		return errors.New("analysis requires owner/repository")
	}
	if err := (github.Repo{Owner: parts[0], Name: parts[1]}).Validate(); err != nil {
		return err
	}
	if err := s.Limits.Validate(); err != nil {
		return err
	}
	if len(s.Issues) < 1 || len(s.Issues) > s.Limits.Issues {
		return errors.New("analysis requires bounded explicit issues")
	}
	seen := map[int]bool{}
	for _, n := range s.Issues {
		if n < 1 || seen[n] {
			return errors.New("invalid or duplicate analysis issue")
		}
		seen[n] = true
	}
	return nil
}

// Evidence is an immutable sanitized source observation.
type Evidence struct {
	ID              string    `json:"id"`
	Issue           int       `json:"issue"`
	Kind            string    `json:"kind"`
	Locator         string    `json:"locator"`
	Revision        string    `json:"revision,omitempty"`
	ReviewStatus    string    `json:"review_status,omitempty"`
	ObservedAt      time.Time `json:"observed_at"`
	UpdatedAt       time.Time `json:"updated_at,omitempty"`
	Excerpt         string    `json:"excerpt"`
	Digest          string    `json:"digest"`
	RedactionPolicy string    `json:"redaction_policy"`
	Truncated       bool      `json:"truncated"`
}

// Coverage records host-observed collection progress and omissions.
type Coverage struct {
	Issue  int    `json:"issue"`
	Source string `json:"source"`
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
}

// CollectionPolicy records fixed host guards against metadata amplification.
type CollectionPolicy struct {
	CoverageEntries        int `json:"coverage_entries"`
	CoverageBytes          int `json:"coverage_bytes"`
	LinkCandidatesPerIssue int `json:"link_candidates_per_issue"`
}

func collectionPolicy() CollectionPolicy { return CollectionPolicy{128, 64 << 10, 128} }

// Manifest freezes evidence, collection interval and host counters.
type Manifest struct {
	Policy              CollectionPolicy `json:"collection_policy"`
	CoverageOmitted     int              `json:"coverage_omitted"`
	ConnectionVersionID int64            `json:"connection_version_id,omitempty"`
	Evidence            []Evidence       `json:"evidence"`
	Coverage            []Coverage       `json:"coverage"`
	StartedAt           time.Time        `json:"started_at"`
	FinishedAt          time.Time        `json:"finished_at"`
	Requests            int              `json:"requests"`
	SourceBytes         int64            `json:"source_bytes"`
	EvidenceBytes       int              `json:"evidence_bytes"`
	Frozen              bool             `json:"frozen"`
}

// LocalObservation carries a bounded retained Bug diagnosis.
type LocalObservation struct{ Kind, Locator, Revision, ReviewStatus, Body string }

// LocalEvidence reads only selected issue identities without exposing SQL.
type LocalEvidence interface {
	ReadBugEvidence(context.Context, string, int, int) ([]LocalObservation, error)
}

// Collector owns source authority and sanitization before analyst launch.
type Collector struct {
	// Budget includes registered-connection verification and is never reset.
	Budget *github.AnalysisBudget
	// APIBase and WebHost come from the registered connection, never source text.
	APIBase  string
	WebHost  string
	GitHub   *github.Client
	Local    LocalEvidence
	Redactor *redact.Redactor
}

var fixURL = regexp.MustCompile(`https://[^\s<>"\)\]]+`)

type fixRef struct{ kind, value string }

// Collect freezes host-controlled observations before any model work. Remote
// observations are not a coherent historical snapshot; omissions remain visible.
func (c Collector) Collect(ctx context.Context, scope Scope) (Manifest, error) {
	m := Manifest{StartedAt: time.Now().UTC(), Policy: collectionPolicy()}
	if err := scope.Validate(); err != nil {
		return m, err
	}
	if c.Redactor == nil {
		return m, errors.New("analysis redactor required")
	}
	parts := strings.Split(scope.Repository, "/")
	repo := github.Repo{Owner: parts[0], Name: parts[1]}
	b := c.Budget
	if b == nil {
		b = &github.AnalysisBudget{Repository: scope.Repository, APIBase: c.APIBase, MaxRequests: scope.Limits.Requests, MaxResponseBytes: scope.Limits.ResponseBytes, MaxSourceBytes: scope.Limits.SourceBytes}
	}
	webHost := c.WebHost
	if webHost == "" {
		webHost = "github.com"
	}
	ctx = github.WithAnalysisBudget(ctx, b)
	stopped := false
	fixCount := 0
	coverageBytes := 0
	gap := func(issue int, source, status, reason string) {
		if m.CoverageOmitted > 0 {
			m.CoverageOmitted++
			stopped = true
			return
		}
		safeSource, sourceErr := c.Redactor.Apply(ctx, source, 2048)
		safeReason, reasonErr := c.Redactor.Apply(ctx, reason, 2048)
		safeStatus, statusErr := c.Redactor.Apply(ctx, status, 64)
		if sourceErr != nil || reasonErr != nil || statusErr != nil {
			source, status, reason = "source metadata", "unavailable", "redaction failed; metadata withheld"
		} else {
			source, status, reason = safeSource.String(), safeStatus.String(), safeReason.String()
		}
		entry := Coverage{Issue: issue, Source: source, Status: status, Reason: reason}
		encoded, _ := json.Marshal(entry)
		if len(m.Coverage) >= m.Policy.CoverageEntries-1 || coverageBytes+len(encoded)+1024 > m.Policy.CoverageBytes {
			m.CoverageOmitted = 1
			stopped = true
			m.Coverage = append(m.Coverage, Coverage{Source: "coverage metadata", Status: "limited", Reason: "coverage metadata ceiling reached; further omissions are counted, collection stopped"})
			return
		}
		coverageBytes += len(encoded)
		m.Coverage = append(m.Coverage, entry)
	}
	failure := func(issue int, source string, err error) {
		status := "unavailable"
		reason := "source unavailable"
		if errors.Is(err, github.ErrAnalysisLimit) {
			status = "limited"
			reason = "remote request or source byte ceiling reached"
			stopped = true
		}
		if errors.Is(err, github.ErrRateLimited) {
			status = "limited"
			reason = "remote rate limit; no retry in this invocation"
			var limited *github.RateLimitError
			if errors.As(err, &limited) {
				reason += fmt.Sprintf("; retry after %s", limited.RetryAfter)
			}
			stopped = true
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			status = "limited"
			reason = "collection cancelled or deadline reached"
			stopped = true
		}
		gap(issue, source, status, reason)
	}
	add := func(issue int, kind, locator, revision, review, body string, updated time.Time) {
		remaining := scope.Limits.EvidenceBytes - m.EvidenceBytes
		if remaining <= 0 {
			gap(issue, kind, "limited", "retained evidence ceiling reached")
			stopped = true
			return
		}
		safe, err := c.Redactor.Apply(ctx, body, min(remaining, 16384))
		if err != nil {
			gap(issue, kind, "unavailable", "redaction failed; content withheld")
			return
		}
		text := safe.String()
		safeLocator, locatorErr := c.Redactor.Apply(ctx, locator, 2048)
		safeRevision, revisionErr := c.Redactor.Apply(ctx, revision, 256)
		safeReview, reviewErr := c.Redactor.Apply(ctx, review, 128)
		if locatorErr != nil || revisionErr != nil || reviewErr != nil {
			gap(issue, kind, "unavailable", "metadata redaction failed; content withheld")
			return
		}
		locator, revision, review = safeLocator.String(), safeRevision.String(), safeReview.String()
		truncated := len(body) > len(text)
		sum := sha256.Sum256([]byte(text))
		digest := hex.EncodeToString(sum[:])
		for _, e := range m.Evidence {
			if e.Issue == issue && e.Kind == kind && e.Locator == locator && e.Digest == digest {
				return
			}
		}
		e := Evidence{ID: fmt.Sprintf("e%04d", len(m.Evidence)+1), Issue: issue, Kind: kind, Locator: locator, Revision: revision, ReviewStatus: review, ObservedAt: time.Now().UTC(), UpdatedAt: updated, Excerpt: text, Digest: digest, RedactionPolicy: "internal/redact:S-7", Truncated: truncated}
		encoded, encodeErr := json.Marshal(e)
		if encodeErr != nil || len(encoded) > remaining {
			gap(issue, kind, "limited", "retained evidence ceiling reached including metadata")
			stopped = true
			return
		}
		m.Evidence = append(m.Evidence, e)
		m.EvidenceBytes += len(encoded)
		gap(issue, locator, "read", "")
		if truncated {
			gap(issue, locator, "limited", "sanitized excerpt truncated or redacted")
		}
	}
	for _, issue := range scope.Issues {
		switch {
		case m.EvidenceBytes >= scope.Limits.EvidenceBytes:
			stopped = true
			gap(issue, "local_bug", "limited", "retained evidence ceiling reached")
		case c.Local != nil:
			allowance := min(int64(65536), b.RemainingSourceBytes())
			if allowance <= 0 {
				stopped = true
				gap(issue, "local_bug", "limited", "shared source byte ceiling reached")
				break
			}
			rows, localErr := c.Local.ReadBugEvidence(ctx, scope.Repository, issue, int(allowance))
			for _, row := range rows {
				if err := b.ChargeSource(int64(len(row.Body))); err != nil {
					stopped = true
					gap(issue, "local_bug", "limited", "shared source byte ceiling reached")
					break
				}
				kind := row.Kind
				if kind == "" {
					kind = "diagnosis"
				}
				add(issue, kind, row.Locator, row.Revision, row.ReviewStatus, row.Body, time.Time{})
			}
			if localErr != nil {
				status := "unavailable"
				if errors.Is(localErr, store.ErrPreventionEvidenceLimited) {
					status = "limited"
				}
				gap(issue, "local_bug", status, "local Bug evidence incomplete or unavailable; retained available prefix")
			} else if len(rows) == 0 {
				gap(issue, "local_bug", "unavailable", "no retained local Bug diagnosis or repair observations")
			}

		default:
			gap(issue, "local_bug", "unavailable", "local Bug evidence adapter unavailable")
		}
		if stopped {
			gap(issue, "issue_and_discussion", "limited", "remote collection stopped at earlier ceiling")
			continue
		}
		if c.GitHub == nil {
			gap(issue, "issue_and_discussion", "unavailable", "remote evidence adapter unavailable")
			continue
		}
		report, err := c.GitHub.GetIssue(ctx, repo, issue)
		if err != nil {
			failure(issue, "issue", err)
			continue
		}
		add(issue, "bug", report.HTMLURL, "", "", report.Title+"\n"+report.Body, report.UpdatedAt)
		texts := []string{report.Body}
		for page := 1; page > 0; {
			if stopped {
				break
			}
			comments, err := c.GitHub.ListCommentsPage(ctx, repo, issue, page, scope.Limits.PageSize)
			if err != nil {
				failure(issue, "discussion", err)
				break
			}
			for _, v := range comments.Comments {
				add(issue, "comment", fmt.Sprintf("github:%s#%d/comment/%d", scope.Repository, issue, v.ID), "", "", v.Body, v.UpdatedAt)
				texts = append(texts, v.Body)
			}
			if comments.NextPage <= page {
				break
			}
			page = comments.NextPage
		}
		seen := map[string]bool{}
		linkCount := 0
		for _, text := range texts {
			if stopped {
				break
			}
			remainingLinks := m.Policy.LinkCandidatesPerIssue - linkCount
			links := fixURL.FindAllString(text, remainingLinks+1)
			limitedLinks := len(links) > remainingLinks
			if limitedLinks {
				links = links[:remainingLinks]
			}
			linkCount += len(links)
			for _, raw := range links {
				if stopped {
					break
				}
				u, err := url.Parse(strings.TrimRight(raw, ".,;"))
				if err != nil {
					continue
				}
				path := strings.Split(strings.Trim(u.Path, "/"), "/")
				if len(path) < 4 {
					gap(issue, "external_link", "excluded", "attachments and arbitrary URLs are not collected")
					continue
				}
				if !strings.EqualFold(path[0]+"/"+path[1], scope.Repository) || !strings.EqualFold(u.Host, webHost) {
					gap(issue, "external_link", "excluded", "cross-repository or arbitrary URL not collected")
					continue
				}
				ref := fixRef{path[2], path[3]}
				if ref.kind != "pull" && ref.kind != "commit" {
					continue
				}
				key := ref.kind + ":" + ref.value
				if seen[key] {
					continue
				}
				seen[key] = true
				if fixCount >= scope.Limits.Fixes || stopped {
					gap(issue, key, "limited", "direct fix or remote ceiling reached")
					continue
				}
				fixCount++
				if ref.kind == "commit" {
					v, err := c.GitHub.GetAnalysisCommit(ctx, repo, ref.value, 1, scope.Limits.PageSize)
					if err != nil {
						failure(issue, key, err)
						continue
					}
					add(issue, "commit", v.URL, v.SHA, "", v.Message, time.Time{})
					for _, f := range v.Files {
						add(issue, "patch", v.URL+"#"+f.Filename, v.SHA, "", f.Filename+"\n"+f.Patch, time.Time{})
						if f.Patch == "" {
							gap(issue, key, "unavailable", "commit patch unavailable")
						}
					}
					if v.NextPage > 0 {
						gap(issue, key, "limited", "additional commit file pages omitted")
					}
					continue
				}
				n, err := strconv.Atoi(ref.value)
				if err != nil || n < 1 {
					gap(issue, key, "excluded", "invalid PR reference")
					continue
				}
				pr, err := c.GitHub.GetDeliveryPR(ctx, repo, n)
				if err != nil {
					failure(issue, key, err)
					continue
				}
				add(issue, "fix_pr", raw, pr.Head, "", pr.Title+"\n"+pr.Body, time.Time{})
				files, err := c.GitHub.ListAnalysisPRFiles(ctx, repo, n, 1, scope.Limits.PageSize)
				if err != nil {
					failure(issue, key+" files", err)
				} else {
					for _, f := range files.Files {
						add(issue, "patch", raw+"#"+f.Filename, pr.Head, "", f.Filename+"\n"+f.Patch, time.Time{})
						if f.Patch == "" {
							gap(issue, key, "unavailable", "PR patch unavailable")
						}
					}
					if files.NextPage > 0 {
						gap(issue, key, "limited", "additional PR file pages omitted")
					}
				}
				if !stopped {
					after, err := c.GitHub.GetDeliveryPR(ctx, repo, n)
					if err != nil {
						failure(issue, key+" head recheck", err)
					} else if after.Head != pr.Head {
						gap(issue, key, "unavailable", "PR head changed during file collection; patches are not a coherent pinned observation")
					}
				}
				if !stopped {
					reviews, err := c.GitHub.ListAnalysisReviewComments(ctx, repo, n, 1, scope.Limits.PageSize)
					if err != nil {
						failure(issue, key+" reviews", err)
					} else {
						for _, r := range reviews.Comments {
							add(issue, "review_comment", fmt.Sprintf("github:%s/pull/%d/review-comment/%d", scope.Repository, n, r.ID), r.CommitSHA, "", r.Body, r.UpdatedAt)
						}
						if reviews.NextPage > 0 {
							gap(issue, key, "limited", "additional PR review comment pages omitted")
						}
					}
				}
			}
			if limitedLinks {
				gap(issue, "direct fix references", "limited", "link candidate ceiling reached; remaining links omitted")
				stopped = true
			}
		}
	}
	m.Requests = b.Requests
	m.SourceBytes = b.SourceBytes
	m.FinishedAt = time.Now().UTC()
	m.Frozen = true
	return m, ctx.Err()
}
