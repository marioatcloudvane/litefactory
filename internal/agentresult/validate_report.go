package agentresult

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"
)

// The Validate reporter writes the owner's short summary. It chooses one action
// per item from a fixed list; the factory turns each action into the exact
// command with the current revision, decision reference, IDs and digests. The
// reporter never writes a command, a link other than an evidence permalink, or a
// factory marker.

// Reporter modes: the first report of a tested commit, or an answer to one
// owner comment on the waiting report.
const (
	ReportModeReport   = "report"
	ReportModeFollowUp = "follow-up"
)

// Report actions. The factory renders each one as an exact owner command.
const (
	ReportActionRepair      = "repair"
	ReportActionCheckPass   = "check-pass"
	ReportActionCheckFail   = "check-fail"
	ReportActionDefer       = "defer"
	ReportActionIssueBug    = "issue-bug"
	ReportActionIssueStory  = "issue-story"
	ReportActionSendBack    = "send-back"
	ReportActionShip        = "ship"
	ReportActionRerun       = "rerun"
	ReportActionFactoryBug  = "factory-bug"
	ReportActionWaitForMain = "wait-for-main"
)

// ReportActions is the closed action list of the reporter.
var ReportActions = []string{ReportActionRepair, ReportActionCheckPass, ReportActionCheckFail, ReportActionDefer, ReportActionIssueBug, ReportActionIssueStory, ReportActionSendBack, ReportActionShip, ReportActionRerun, ReportActionFactoryBug, ReportActionWaitForMain}

// Diagnosis classes of one item. The first four classify a failure; unclear
// says the evidence does not decide it; none marks an item that is not a
// failure, such as a manual check or an optional suggestion.
const (
	DiagnosisStoryDefect    = "story-defect"
	DiagnosisAlreadyFailing = "already-failing"
	DiagnosisFactory        = "factory"
	DiagnosisFlaky          = "flaky"
	DiagnosisUnclear        = "unclear"
	DiagnosisNone           = "none"
)

// DiagnosisClasses is the closed set of diagnosis classes.
var DiagnosisClasses = []string{DiagnosisStoryDefect, DiagnosisAlreadyFailing, DiagnosisFactory, DiagnosisFlaky, DiagnosisUnclear, DiagnosisNone}

// ReportCertainties are the certainty words of the "why" line.
var ReportCertainties = []string{"certain", "likely", "unsure"}

// Re-run targets and results.
var (
	ReportRerunCommits = []string{"tested", "base"}
	ReportRerunResults = []string{"passed", "failed", "timeout", "not-run"}
)

// Bounds of one reporter result. The summary has to fit one screen.
const (
	MaxReportHeadline       = 160
	MaxReportFailedLines    = 3
	MaxReportFailedLine     = 200
	MaxReportWhy            = 400
	MaxReportItems          = 30
	MaxReportRecommendation = 240
	MaxReportEvidence       = 300
	MaxReportReruns         = 3
	MaxReportBaseReruns     = 1
	MaxReportRerunCommand   = 300
	MaxReportRerunNote      = 300
	MaxReportAnswer         = 1500
)

// ValidateReport is the reporter's structured result. The factory stores it
// once and renders the summary from storage.
type ValidateReport struct {
	Mode       string        `json:"mode"`
	Headline   string        `json:"headline,omitempty"`
	WhatFailed []string      `json:"what_failed,omitempty"`
	Why        string        `json:"why,omitempty"`
	Certainty  string        `json:"certainty,omitempty"`
	Next       *ReportItem   `json:"next,omitempty"`
	Items      []ReportItem  `json:"items,omitempty"`
	Reruns     []ReportRerun `json:"reruns,omitempty"`
	// Answer is the plain answer to the owner's comment in follow-up mode.
	Answer string `json:"answer,omitempty"`
}

// ReportItem is one recommendation: which item, what to do, why, and on what
// evidence. Ref names a finding ID, a check ID, a failure index (failure-1) or
// a repair candidate ID; the next step may leave it empty for an action on the
// whole run (ship, send-back, rerun, wait-for-main).
type ReportItem struct {
	Ref            string `json:"ref,omitempty"`
	Action         string `json:"action"`
	Recommendation string `json:"recommendation"`
	Diagnosis      string `json:"diagnosis"`
	Evidence       string `json:"evidence,omitempty"`
}

// ReportRerun is one focused read-only re-run the reporter made to diagnose a
// failure. It is evidence for the details, never a command for the owner.
type ReportRerun struct {
	Command string `json:"command"`
	Commit  string `json:"commit"`
	Result  string `json:"result"`
	Note    string `json:"note,omitempty"`
}

// ReportScope is the trusted context a reporter result is judged against: the
// items of the current state and the actions each one admits now.
type ReportScope struct {
	FollowUp bool
	// Items maps each item reference to the actions it admits.
	Items map[string][]string
	// Global lists the actions the next step may take without a reference.
	Global []string
	// Evidence lists the references an item may cite besides item references
	// and its own re-runs (rerun-1 …), such as retained check IDs.
	Evidence map[string]bool
	// Permalinks are the URL prefixes an evidence reference may use.
	Permalinks []string
}

// Clone returns an independent copy.
func (s *ReportScope) Clone() *ReportScope {
	if s == nil {
		return nil
	}
	out := *s
	out.Items = make(map[string][]string, len(s.Items))
	for k, v := range s.Items {
		out.Items[k] = slices.Clone(v)
	}
	out.Global = slices.Clone(s.Global)
	out.Evidence = make(map[string]bool, len(s.Evidence))
	for k, v := range s.Evidence {
		out.Evidence[k] = v
	}
	out.Permalinks = slices.Clone(s.Permalinks)
	return &out
}

var (
	// reportCommandPattern finds a factory command: a slash word that starts a
	// token and is not a path segment.
	reportCommandPattern = regexp.MustCompile("(?:^|[\\s`(\\[\"'])/[A-Za-z][A-Za-z0-9_-]*(?:$|[\\s`)\\].,;:!?\"'])")
	// reportDecisionPattern finds a displayed decision reference.
	reportDecisionPattern = regexp.MustCompile(`\bd-[0-9a-f]{12}\b`)
	reportURLPattern      = regexp.MustCompile(`(?i)\b(?:https?|ftp|file|mailto|javascript|data):|\bwww\.`)
	reportMarkerPattern   = regexp.MustCompile(`(?i)<!--|-->|\blf:[a-z]|\blitefactory:[a-z]`)
)

// reportTextFault names why one piece of reporter text is refused, or "".
func reportTextFault(text string, permalinks []string) string {
	switch {
	case reportMarkerPattern.MatchString(text):
		return "a factory marker"
	case reportCommandPattern.MatchString(text):
		return "a command"
	case reportDecisionPattern.MatchString(text):
		return "a decision reference"
	}
	if reportURLPattern.MatchString(text) {
		if len(permalinks) == 0 {
			return "a link"
		}
		for _, field := range strings.Fields(text) {
			if !reportURLPattern.MatchString(field) {
				continue
			}
			if !slices.ContainsFunc(permalinks, func(p string) bool { return p != "" && strings.HasPrefix(field, p) }) {
				return "a link that is not an evidence permalink"
			}
		}
	}
	return ""
}

// reportText checks one bounded piece of reporter text.
func reportText(d *Diagnostics, path, text string, limit int, multiline, required bool, permalinks []string) {
	if strings.TrimSpace(text) == "" {
		if required {
			add(d, path, "blank", "nonblank string", "blank", "Supply meaningful content.")
		}
		return
	}
	if utf8.RuneCountInString(text) > limit {
		add(d, path, "limit", fmt.Sprintf("at most %d characters", limit), "too long", "Say it shorter, in plain words.")
	}
	if !multiline && strings.ContainsAny(text, "\r\n") {
		add(d, path, "limit", "one line", "several lines", "Write this as one line.")
	}
	if fault := reportTextFault(text, permalinks); fault != "" {
		add(d, path, "forbidden", "plain words without commands, links or markers", fault, "Name the action from the list instead; the factory writes every command, link and marker.")
	}
}

// validateReportPayload checks the reporter's result against its scope.
func validateReportPayload(p *Validate, outcome string, ctx Context, d *Diagnostics) {
	const base = "$.result.payload.validate.report"
	scope := ctx.Report
	if scope == nil {
		add(d, base, "forbidden", "no report outside the reporter role", "present report", "Only the reporter node writes the owner summary.")
		return
	}
	if len(p.Stories) != 0 || p.Repair != nil {
		add(d, "$.result.payload.validate", "forbidden", "a report only", "assessments or a repair report", "Write the summary only; reviewers assess and the repair agent repairs.")
	}
	r := p.Report
	if r == nil {
		if outcome == "completed" {
			add(d, base, "required", "a report", "absent", "Write the owner summary.")
		}
		return
	}
	wantMode := ReportModeReport
	if scope.FollowUp {
		wantMode = ReportModeFollowUp
	}
	if r.Mode != wantMode {
		add(d, base+".mode", "enum", wantMode, "different mode", "Use the mode the factory invoked you in.")
	}
	recommends := r.Next != nil || len(r.Items) > 0 || r.Headline != ""
	switch {
	case scope.FollowUp:
		reportText(d, base+".answer", r.Answer, MaxReportAnswer, true, true, nil)
	case r.Answer != "":
		add(d, base+".answer", "forbidden", "no answer in report mode", "present answer", "Only a follow-up answers an owner comment.")
	}
	if !scope.FollowUp || recommends {
		reportText(d, base+".headline", r.Headline, MaxReportHeadline, false, true, nil)
		if r.Next == nil {
			add(d, base+".next", "required", "one next step", "absent", "Name the one step the owner should take next.")
		}
	}
	if len(r.WhatFailed) > MaxReportFailedLines {
		add(d, base+".what_failed", "limit", fmt.Sprintf("at most %d lines", MaxReportFailedLines), "too many", "Name the 1-3 most important failures; the details keep the rest.")
	}
	for i, line := range r.WhatFailed {
		reportText(d, fmt.Sprintf("%s.what_failed[%d]", base, i), line, MaxReportFailedLine, false, true, nil)
	}
	reportText(d, base+".why", r.Why, MaxReportWhy, false, false, nil)
	if r.Why != "" && !slices.Contains(ReportCertainties, r.Certainty) {
		add(d, base+".certainty", "enum", strings.Join(ReportCertainties, ", "), "missing or unsupported certainty", "Say how sure you are of the cause.")
	}
	if r.Why == "" && r.Certainty != "" {
		add(d, base+".certainty", "forbidden", "certainty only with a why", "certainty without why", "Give the cause with its certainty, or neither.")
	}
	if len(r.Reruns) > MaxReportReruns {
		add(d, base+".reruns", "limit", fmt.Sprintf("at most %d re-runs", MaxReportReruns), "too many", "Stay within the diagnosis budget.")
	}
	bases := 0
	for i, run := range r.Reruns {
		rp := fmt.Sprintf("%s.reruns[%d]", base, i)
		if strings.TrimSpace(run.Command) == "" || len(run.Command) > MaxReportRerunCommand || strings.ContainsAny(run.Command, "\r\n") || reportMarkerPattern.MatchString(run.Command) {
			add(d, rp+".command", "limit", "one test command line of at most 300 bytes", "invalid command", "Name the focused test command you ran.")
		}
		if !slices.Contains(ReportRerunCommits, run.Commit) {
			add(d, rp+".commit", "enum", strings.Join(ReportRerunCommits, ", "), "unsupported commit", "Say whether you ran it on the tested or the base commit.")
		}
		if run.Commit == "base" {
			bases++
		}
		if !slices.Contains(ReportRerunResults, run.Result) {
			add(d, rp+".result", "enum", strings.Join(ReportRerunResults, ", "), "unsupported result", "Report what the re-run showed.")
		}
		reportText(d, rp+".note", run.Note, MaxReportRerunNote, false, false, nil)
	}
	if bases > MaxReportBaseReruns {
		add(d, base+".reruns", "limit", "at most one re-run on the base commit", "too many base re-runs", "Use one base re-run to tell a new failure from an old one.")
	}
	if r.Next != nil {
		reportItem(d, base+".next", *r.Next, scope, len(r.Reruns), true)
	}
	if len(r.Items) > MaxReportItems {
		add(d, base+".items", "limit", fmt.Sprintf("at most %d items", MaxReportItems), "too many", "One line per open item.")
	}
	seen := map[string]bool{}
	for i, item := range r.Items {
		ip := fmt.Sprintf("%s.items[%d]", base, i)
		if seen[item.Ref] {
			add(d, ip+".ref", "duplicate", "each item once", "duplicate item", "Give each item one recommendation.")
		}
		seen[item.Ref] = true
		reportItem(d, ip, item, scope, len(r.Reruns), false)
	}
}

// reportItem checks one recommendation against the scope.
func reportItem(d *Diagnostics, path string, item ReportItem, scope *ReportScope, reruns int, next bool) {
	if !slices.Contains(ReportActions, item.Action) {
		add(d, path+".action", "enum", strings.Join(ReportActions, ", "), "unsupported action", "Choose one action from the list.")
	} else if item.Ref == "" {
		if !next {
			add(d, path+".ref", "required", "an item reference", "absent", "Name the finding, check, failure or repair candidate.")
		} else if !slices.Contains(scope.Global, item.Action) {
			add(d, path+".action", "reference", "an action the whole run admits now", "not available", "Name the item this action applies to, or choose an available action.")
		}
	} else if allowed, ok := scope.Items[item.Ref]; !ok {
		add(d, path+".ref", "reference", "an item of this report", "unknown item", "Use a finding ID, failure index or repair candidate ID from your input.")
	} else if !slices.Contains(allowed, item.Action) {
		add(d, path+".action", "reference", strings.Join(allowed, ", "), "not available for this item", "Choose an action this item admits now.")
	}
	if !slices.Contains(DiagnosisClasses, item.Diagnosis) {
		add(d, path+".diagnosis", "enum", strings.Join(DiagnosisClasses, ", "), "unsupported diagnosis", "Classify the item.")
	}
	reportText(d, path+".recommendation", item.Recommendation, MaxReportRecommendation, false, true, nil)
	if e := strings.TrimSpace(item.Evidence); e != "" {
		reportText(d, path+".evidence", e, MaxReportEvidence, false, false, scope.Permalinks)
		if !reportEvidenceKnown(e, scope, reruns) {
			add(d, path+".evidence", "reference", "a check ID, item reference, re-run (rerun-1) or evidence permalink", "unknown evidence", "Cite evidence from your input or your own re-runs.")
		}
	}
}

func reportEvidenceKnown(e string, scope *ReportScope, reruns int) bool {
	for _, field := range strings.FieldsFunc(e, func(r rune) bool { return r == ',' || r == ';' || r == ' ' }) {
		if _, ok := scope.Items[field]; ok || scope.Evidence[field] {
			return true
		}
		for i := 1; i <= reruns; i++ {
			if field == fmt.Sprintf("rerun-%d", i) {
				return true
			}
		}
		if slices.ContainsFunc(scope.Permalinks, func(p string) bool { return p != "" && strings.HasPrefix(field, p) }) {
			return true
		}
	}
	return false
}
