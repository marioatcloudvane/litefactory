package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
)

// SpikeIssueForge is the narrow forge surface issue filing uses: create one
// plain issue and page through issues to find one it may already have created.
// It carries no sub-issue attachment, so the filed issue is never a Plan child.
type SpikeIssueForge interface {
	CreateIssue(context.Context, github.Repo, redact.Body, redact.Body) (github.Issue, error)
	ListIssuesPage(context.Context, github.Repo, string, int, int) (github.IssuePage, error)
}

const (
	spikeIssueTitleLimit = 256
	spikeIssueBodyLimit  = 60000
	spikeIssuePages      = 30
)

// Issue record states. Only filed is confirmed. Intent and uncertain both mean
// a create may or may not have happened, and are settled only by finding the
// marker; failed means nothing was, or will be, created.
const (
	spikeIssueIntent    = "intent"
	spikeIssueFiled     = "filed"
	spikeIssueUncertain = "uncertain"
	spikeIssueFailed    = "failed"
)

// SpikeIssueActivity files one plain repository issue for a change that turned
// out too big, once, and finds it again by a hidden marker if the run restarts.
// It writes only its own record. Both outcomes lead on to the report.
type SpikeIssueActivity struct {
	Profile *graphexec.Profile
	Context func(context.Context, store.GraphNodeAttempt) (agentresult.Context, error)
	Ref     naming.Ref
}

// spikeIssueRecord is the Spike-owned record of one filing.
type spikeIssueRecord struct {
	State           string `json:"state"`
	SourceAttemptID int64  `json:"source_attempt_id"`
	Marker          string `json:"marker"`
	Number          int    `json:"number,omitempty"`
	URL             string `json:"url,omitempty"`
	Detail          string `json:"detail,omitempty"`
}

func (r *spikeIssueRecord) view() *agentresult.SpikeIssueView {
	if r == nil {
		return nil
	}
	return &agentresult.SpikeIssueView{State: r.State, Number: r.Number, URL: r.URL}
}

// retainedSpikeIssue reads the filing record of exactly this source result.
func retainedSpikeIssue(ctx context.Context, db *store.DB, instanceID, sourceAttemptID int64) (*spikeIssueRecord, error) {
	raw, err := db.LatestSpikeRecord(ctx, instanceID, store.SpikeRecordIssue)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var record spikeIssueRecord
	if err := json.Unmarshal(raw.Payload, &record); err != nil {
		return nil, fmt.Errorf("retained spike issue is unreadable: %w", err)
	}
	if record.SourceAttemptID != sourceAttemptID {
		return nil, nil
	}
	return &record, nil
}

// spikeIssueMarker is hidden in the issue body. It names the session and the
// node, so one filing is found again and no other issue is mistaken for it.
func spikeIssueMarker(sessionID int64, node string) string {
	return fmt.Sprintf("<!-- lf:spike-issue session=%d node=%s -->", sessionID, node)
}

// Execute files the issue, or recovers or reports the earlier attempt to.
func (h SpikeIssueActivity) Execute(ctx context.Context, opts Options, req ActivityRequest) (ActivityResult, error) {
	step, err := loadSpikeStep(ctx, opts, req, h.Profile, h.Context, SpikeIssueAction)
	var unusable *spikeResultError
	if err != nil && !errors.As(err, &unusable) {
		return ActivityResult{Diagnostic: err.Error()}, err
	}
	instance, err := opts.DB.GetGraphInstance(ctx, step.attempt.InstanceID)
	if err != nil {
		return ActivityResult{Diagnostic: err.Error()}, err
	}
	record := spikeIssueRecord{SourceAttemptID: step.source.ID, Marker: spikeIssueMarker(instance.SessionID, step.node.Name)}
	put := func(state, detail string) error {
		record.State, record.Detail = state, detail
		raw, err := json.Marshal(record)
		if err == nil {
			err = opts.DB.PutSpikeRecord(ctx, store.SpikeRecord{InstanceID: instance.ID, Node: step.node.Name, Kind: store.SpikeRecordIssue, Payload: raw, UpdatedAt: spikeNow(opts)})
		}
		if err != nil {
			return fmt.Errorf("retain spike issue: %w", err)
		}
		return nil
	}
	failed := func(state, detail string) (ActivityResult, error) {
		if err := put(state, detail); err != nil {
			return ActivityResult{Diagnostic: err.Error()}, err
		}
		return ActivityResult{Outcome: "failed", Diagnostic: detail}, nil
	}
	filed := func(issue github.Issue) (ActivityResult, error) {
		record.Number, record.URL = issue.Number, issue.HTMLURL
		if err := put(spikeIssueFiled, ""); err != nil {
			return ActivityResult{Diagnostic: err.Error()}, err
		}
		return ActivityResult{Outcome: "filed"}, nil
	}
	// A filing already settled for this result is never redone.
	prior, err := retainedSpikeIssue(ctx, opts.DB, instance.ID, step.source.ID)
	if err != nil {
		return ActivityResult{Diagnostic: err.Error()}, err
	}
	if prior != nil {
		record = *prior
		switch prior.State {
		case spikeIssueFiled:
			return ActivityResult{Outcome: "filed"}, nil
		case spikeIssueFailed:
			return ActivityResult{Outcome: "failed", Diagnostic: prior.Detail}, nil
		}
	}
	if unusable != nil {
		return failed(spikeIssueFailed, "the fixer's result is unusable: "+unusable.cause.Error())
	}
	forge, redactor, err := h.forge(opts)
	if err != nil {
		return failed(spikeIssueFailed, err.Error())
	}
	repo := github.RepoFromRef(h.Ref)
	if err := repo.Validate(); err != nil || h.Ref.Issue < 1 {
		return failed(spikeIssueFailed, "the source issue is unknown")
	}
	if prior != nil {
		// A create may have happened. Only the marker can say so; a missing match
		// never authorizes another create.
		found, matches, err := findSpikeIssue(ctx, forge, repo, record.Marker, opts.MCP.BotID)
		if err != nil {
			return ActivityResult{Outcome: "failed", Diagnostic: "the earlier filing could not be checked: " + err.Error()}, nil //nolint:nilerr // The declared failed outcome reaches the report; the retained intent is kept for the next replay.
		}
		switch matches {
		case 1:
			return filed(found)
		case 0:
			return failed(spikeIssueUncertain, "an earlier attempt to file left no issue that could be found; not filing again")
		default:
			return failed(spikeIssueUncertain, "several issues carry this filing's marker; not filing again")
		}
	}
	check, err := retainedSpikeCheck(ctx, opts.DB, instance.ID, step.source.ID)
	if err != nil {
		return ActivityResult{Diagnostic: err.Error()}, err
	}
	if check == nil || check.Outcome != "too_big" || step.candidate.Result.Payload.Spike == nil {
		return failed(spikeIssueFailed, "an issue is filed only for a change measured as too big")
	}
	earlier, err := spikeEarlier(ctx, opts.DB, instance.ID, step.source.ID, h.Profile, SpikeWorkflowPolicy())
	if err != nil {
		return failed(spikeIssueFailed, err.Error())
	}
	title, body := spikeIssueContent(h.Ref, record.Marker, step.candidate.Result, earlier, check)
	safeTitle, err := redactor.Apply(ctx, title, spikeIssueTitleLimit)
	if err != nil {
		return ActivityResult{Diagnostic: err.Error()}, err
	}
	safeBody, err := redactor.Apply(ctx, body, spikeIssueBodyLimit)
	if err != nil {
		return ActivityResult{Diagnostic: err.Error()}, err
	}
	if safeTitle.Truncated() || safeBody.Truncated() || !strings.Contains(safeBody.String(), record.Marker) {
		return failed(spikeIssueFailed, "the issue content does not fit whole")
	}
	// The intent is durable before the forge is asked for anything.
	if err := put(spikeIssueIntent, "create intent recorded before the forge call"); err != nil {
		return ActivityResult{Diagnostic: err.Error()}, err
	}
	created, err := forge.CreateIssue(ctx, repo, safeTitle, safeBody)
	if err != nil {
		if ctx.Err() != nil {
			return ActivityResult{Diagnostic: ctx.Err().Error()}, ctx.Err()
		}
		return failed(spikeIssueUncertain, "creating the issue did not confirm: "+err.Error())
	}
	return filed(created)
}

func (h SpikeIssueActivity) forge(opts Options) (SpikeIssueForge, *redact.Redactor, error) {
	if opts.MCP == nil || opts.MCP.Creation == nil || opts.MCP.Redactor == nil {
		return nil, nil, errors.New("issue filing is not available in this run")
	}
	forge, ok := opts.MCP.Creation.(SpikeIssueForge)
	if !ok {
		return nil, nil, errors.New("issue filing is not available in this run")
	}
	return forge, opts.MCP.Redactor, nil
}

// findSpikeIssue looks for issues carrying the marker among the bot's own.
func findSpikeIssue(ctx context.Context, forge SpikeIssueForge, repo github.Repo, marker string, botID int64) (github.Issue, int, error) {
	var found github.Issue
	matches := 0
	for _, state := range []string{github.StateOpen, "closed"} {
		for page := 1; page > 0; {
			if page > spikeIssuePages {
				return found, matches, errors.New("too many issues to search for the marker")
			}
			listed, err := forge.ListIssuesPage(ctx, repo, state, page, 100)
			if err != nil {
				return found, matches, err
			}
			for _, issue := range listed.Items {
				if strings.Contains(issue.Body, marker) && (botID == 0 || issue.Author.ID == botID) {
					found = issue
					matches++
				}
			}
			page = listed.NextPage
		}
	}
	return found, matches, nil
}

// spikeIssueContent builds a title and body that stand alone: the original
// question, what was found, why the change is too big and where it came from.
func spikeIssueContent(source naming.Ref, marker string, fixer agentresult.Result, earlier *agentresult.ResearchNode, check *spikeCheckRecord) (string, string) {
	spike := fixer.Payload.Spike
	request := strings.TrimSpace(spike.Request)
	first, _, _ := strings.Cut(request, "\n")
	title := "Spike follow-up: " + clipText(strings.TrimSpace(first), 120)
	var b strings.Builder
	b.WriteString(marker + "\n\n")
	fmt.Fprintf(&b, "Filed by Spike because a small fix turned out too big. Source: %s/%s#%d\n\n", source.Owner, source.Repo, source.Issue)
	b.WriteString("## Original question\n\n" + blankFallback(request, "_The question was not recorded._") + "\n\n## What the investigation and fix attempt found\n")
	items := func(list []string) {
		for _, item := range list {
			b.WriteString("\n- " + strings.TrimSpace(item))
		}
	}
	if earlier != nil && earlier.Result.Payload.Spike != nil {
		items(earlier.Result.Payload.Spike.Findings)
		if fix := earlier.Result.Payload.Spike.ProposedFix; fix != nil {
			fmt.Fprintf(&b, "\n- Proposed change: %s (files: %s)", strings.Join(fix.Defects, "; "), strings.Join(fix.Files, ", "))
		}
	}
	items(spike.Findings)
	items(spike.WorkDone)
	if fix := spike.FixReport; fix != nil {
		fmt.Fprintf(&b, "\n- The fix attempt: %s", strings.TrimSpace(fix.Summary))
	}
	if len(spike.NotDone) > 0 {
		b.WriteString("\n\n### Not done\n")
		items(spike.NotDone)
	}
	b.WriteString("\n\n## Why it is too big\n\n")
	fmt.Fprintf(&b, "%s.\n\nFiles changed (%d, at most %d allowed): %s", strings.TrimSuffix(blankFallback(check.Reason, "the change exceeded the agreed limits"), "."), len(check.Files), check.MaxFiles, strings.Join(check.Files, ", "))
	if len(check.Forbidden) > 0 {
		b.WriteString("\n\nKinds of change that are not allowed in a Spike: " + strings.Join(check.Forbidden, ", "))
	}
	b.WriteString("\n\nThe attempted change was left in the working branch of the source issue and is not ready.\n")
	return title, b.String()
}
