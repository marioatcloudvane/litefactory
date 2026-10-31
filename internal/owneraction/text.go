package owneraction

import (
	"strings"
	"time"
	"unicode/utf8"
)

// Transports a rendering is shown on.
const (
	TransportDiscord = "discord"
	TransportMCP     = "mcp"
)

// Step kinds name the next owner action, independent of wording.
const (
	StepReplyYes   = "reply_yes"
	StepReplyNo    = "reply_no"
	StepTapCheck   = "tap_check"
	StepMCPConfirm = "mcp_confirm"
	StepMCPCancel  = "mcp_cancel"
)

// Outcome kinds are the plain meaning of a lifecycle state.
const (
	OutcomeProposed          = "proposed"
	OutcomeAwaitingTap       = "awaiting_tap"
	OutcomeDeliveryUncertain = "delivery_uncertain"
	OutcomeStarted           = "started"
	OutcomeSucceeded         = "succeeded"
	OutcomeFailed            = "failed"
	OutcomeUncertain         = "uncertain"
	OutcomeCancelled         = "cancelled"
	OutcomeExpired           = "expired"
	OutcomeInvalidated       = "invalidated"
	OutcomeRecorded          = "recorded"
)

// Mode selects which factory message is rendered.
type Mode int

// Brief is the ordinary chat or tool text, Details the full review and
// Approval the separate Discord approval request.
const (
	Brief Mode = iota
	Details
	Approval
)

// excerptRunes bounds owner text quoted in brief renderings.
const excerptRunes = 160

// Options names where the rendering is shown and which message it is.
type Options struct {
	Transport string
	Mode      Mode
}

// Term is one readable material term of the reviewed action.
type Term struct {
	Label string
	Value string
}

// Step is one ordered next owner action with its time bounds. A zero
// NotBefore on a Discord yes means "5 seconds after this message".
type Step struct {
	Kind      string
	NotBefore time.Time
	Until     time.Time
	// Later marks the ✅ tap announced before its approval request exists;
	// Here marks the tap on the approval request itself.
	Later, Here bool
}

// View is the structured, factory-rendered owner review. It never holds raw
// arguments, principal identities, raw state or reason codes or hashes.
type View struct {
	What      string
	Terms     []Term
	Steps     []Step
	Outcome   string
	Note      string
	Result    string
	Posted    bool
	More      string
	Estimate  string
	Revision  string
	Truncated bool
	mode      Mode
	transport string
	proposal  string
}

// Render builds the plain view of p from frozen facts only.
func Render(p Proposal, o Options) View {
	if o.Transport == "" {
		o.Transport = p.Principal.Transport
	}
	v := View{mode: o.Mode, transport: o.Transport, proposal: p.ID}
	full := o.Mode == Details
	v.What, v.Terms, v.Truncated = summary(p.Facts, full)
	if full {
		v.Revision = p.Facts.TargetRevision
	}
	if e := strings.TrimSpace(p.Facts.Estimate); e != "" && e != "unknown" {
		v.Estimate = e
	}
	v.Result = strings.TrimSpace(p.Result.Summary)
	until := p.ExpiresAt
	cancel := Step{Kind: StepReplyNo, Until: until}
	if o.Transport == TransportMCP {
		cancel.Kind = StepMCPCancel
	}
	switch p.State {
	case "awaiting_confirmation":
		v.Outcome = OutcomeProposed
		confirm := Step{Kind: StepReplyYes, NotBefore: p.ConfirmNotBefore, Until: until}
		if p.Principal.Transport == TransportMCP {
			confirm.Kind = StepMCPConfirm
		}
		v.Steps = append(v.Steps, confirm)
		if p.Tier == Binding {
			v.Steps = append(v.Steps, Step{Kind: StepTapCheck, Until: until, Later: true})
		}
		v.Steps = append(v.Steps, cancel)
		if p.Reason == "too_early" {
			v.Note = "That confirmation came too early; confirm again within the time shown."
		}
	case "awaiting_discord":
		if p.DeliveryState == "uncertain" {
			v.Outcome = OutcomeDeliveryUncertain
			v.Note = "The approval request could not be confirmed as delivered, so nothing ran. Request a fresh proposal."
			break
		}
		v.Outcome = OutcomeAwaitingTap
		v.Steps = []Step{{Kind: StepTapCheck, Until: until, Here: o.Mode == Approval}, cancel}
	case "admitted", "executing":
		v.Outcome = OutcomeStarted
		v.Posted = p.ApprovalChannel != ""
		if o.Mode != Details {
			v.Result = ""
		}
	case "succeeded":
		v.Outcome = OutcomeSucceeded
	case "failed":
		v.Outcome = OutcomeFailed
	case "uncertain":
		v.Outcome = OutcomeUncertain
	case "cancelled":
		v.Outcome = OutcomeCancelled
	case "expired":
		v.Outcome = OutcomeExpired
		v.Note = "Request a fresh proposal if you still want it."
	case "invalidated":
		v.Outcome = OutcomeInvalidated
		v.Note = "The reviewed target or confirmation policy changed. Request a fresh proposal if you still want it."
	default:
		v.Outcome = OutcomeRecorded
	}
	switch reason := p.Reason; {
	case v.Result != "":
		// Domain and service results already explain these causes.
	case reason == "authority_revoked":
		v.Note = "Authority was revoked before it ran."
	case reason == "stale_target_repropose":
		v.Note = "The reviewed target changed. Request a fresh proposal if you still want it."
	case reason == "interrupted_effect":
		v.Note = "Execution was interrupted."
	}
	if o.Transport == TransportMCP {
		v.More = "Use action_inspect with proposal_id " + p.ID + " for the full record."
	} else if !full {
		v.More = "Reply details for more."
		if v.Truncated {
			v.More = "Reply details for the full text."
		}
	}
	return v
}

// Text formats the view in plain words.
func (v View) Text() string {
	var lines []string
	if v.mode == Approval {
		lines = append(lines, "Approval request")
	}
	lines = append(lines, outcomeLead(v.Outcome)+v.What+".")
	if v.Note != "" {
		lines = append(lines, v.Note)
	}
	if v.Result != "" {
		lines = append(lines, v.Result)
	}
	if v.Posted {
		where := "here"
		if v.transport == TransportMCP {
			where = "in Discord"
		}
		lines = append(lines, "The result will be posted "+where+".")
	}
	if v.Outcome == OutcomeUncertain {
		if v.transport == TransportMCP {
			lines = append(lines, "Check it with action_inspect before requesting it again.")
		} else {
			lines = append(lines, "Reply details to check it before requesting it again.")
		}
	}
	if v.mode == Details {
		for _, t := range v.Terms {
			lines = append(lines, t.Label+": "+t.Value)
		}
		if v.Revision != "" {
			lines = append(lines, "Reviewed revision: "+v.Revision)
		}
	}
	if v.Estimate != "" {
		lines = append(lines, "Estimate: "+v.Estimate)
	}
	for _, s := range v.Steps {
		lines = append(lines, v.stepText(s))
	}
	if v.More != "" {
		lines = append(lines, v.More)
	}
	return strings.Join(lines, "\n")
}

func outcomeLead(kind string) string {
	switch kind {
	case OutcomeProposed:
		return "Proposed: "
	case OutcomeAwaitingTap:
		return "Waiting for your ✅: "
	case OutcomeDeliveryUncertain:
		return "Not sent: "
	case OutcomeStarted:
		return "Started: "
	case OutcomeSucceeded:
		return "Done: "
	case OutcomeFailed:
		return "Failed: "
	case OutcomeUncertain:
		return "Outcome uncertain: "
	case OutcomeCancelled:
		return "Cancelled, nothing ran: "
	case OutcomeExpired:
		return "Expired, nothing ran: "
	case OutcomeInvalidated:
		return "Withdrawn, nothing ran: "
	default:
		return ""
	}
}

func clock(t time.Time) string { return t.UTC().Format("15:04") + " UTC" }

func (v View) stepText(s Step) string {
	until := ""
	if !s.Until.IsZero() {
		until = ", until " + clock(s.Until)
	}
	switch s.Kind {
	case StepReplyYes:
		earliest := "at the earliest 5 seconds after this message"
		if !s.NotBefore.IsZero() {
			earliest = "from " + s.NotBefore.UTC().Format("15:04:05") + " UTC"
		}
		return "Reply yes to confirm (" + earliest + until + ")."
	case StepMCPConfirm:
		from := ""
		if !s.NotBefore.IsZero() {
			from = ", from " + s.NotBefore.UTC().Format("15:04:05") + " UTC"
		}
		return "Call action_confirm with proposal_id " + v.proposal + " to confirm (" + strings.TrimPrefix(from+until, ", ") + ")."
	case StepTapCheck:
		switch {
		case s.Later:
			return "Then tap ✅ on the approval request that follows in Discord" + until + "."
		case s.Here:
			return "Tap ✅ on this message to approve it" + until + "."
		default:
			return "Tap ✅ on the approval request in Discord to approve it" + until + "."
		}
	case StepMCPCancel:
		return "Call action_cancel with proposal_id " + v.proposal + " to cancel" + until + "."
	default:
		return "Reply no to cancel" + until + "."
	}
}

// excerpt shortens owner text for brief renderings.
func excerpt(s string, full bool) (string, bool) {
	s = strings.Join(strings.Fields(s), " ")
	if full || utf8.RuneCountInString(s) <= excerptRunes {
		return s, false
	}
	r := []rune(s)
	return strings.TrimSpace(string(r[:excerptRunes])) + "…", true
}

// repository renders a forge reference as owner/name.
func repository(ref string) string {
	if _, rest, ok := strings.Cut(ref, ":"); ok {
		return rest
	}
	return ref
}

// shortID keeps the distinguishing tail of generated session identifiers,
// whose prefixes can carry transport channel identities.
func shortID(id string) string {
	if i := strings.LastIndex(id, ":"); i >= 0 {
		return id[i+1:]
	}
	return id
}

// inRepo names the repository unless the target reference already does.
func inRepo(target, repo string) string {
	if repo == "" || strings.Contains(target, repo) {
		return target
	}
	return target + " in " + repo
}

func window(raw string) string {
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return raw
	}
	return t.UTC().Format("02 Jan 15:04")
}

var editFields = []string{"category_id", "rule", "description", "evidence", "scope", "check", "applies_to", "operations", "replace"}

// summary builds the action-specific readable summary and its terms.
func summary(f Facts, full bool) (string, []Term, bool) {
	args := f.Invocation.Arguments
	if args == nil {
		args = map[string]string{}
	}
	var terms []Term
	truncated := false
	quote := func(label, value string) string {
		if strings.TrimSpace(value) == "" {
			return ""
		}
		terms = append(terms, Term{Label: label, Value: strings.TrimSpace(value)})
		text, cut := excerpt(value, full)
		truncated = truncated || cut
		return "“" + text + "”"
	}
	repo := repository(f.RepositoryRef)
	if repo == "" {
		repo = strings.TrimSpace(args["project"])
	}
	version := func(prefix string) string {
		out := prefix
		if v := args["version"]; v != "" {
			out += " version " + v
		}
		return out
	}
	base := func() string {
		if b := args["base"]; b != "" {
			return " (base revision " + b + ")"
		}
		return ""
	}
	join := func(parts ...string) string {
		var kept []string
		for _, p := range parts {
			if p != "" {
				kept = append(kept, p)
			}
		}
		return strings.Join(kept, ": ")
	}
	edited := func() string {
		var names []string
		for _, name := range editFields {
			if args[name] != "" {
				names = append(names, strings.ReplaceAll(name, "_", " "))
			}
		}
		if len(names) == 0 {
			return "fields"
		}
		return strings.Join(names, ", ")
	}
	editValue := func() string {
		var parts []string
		for _, name := range editFields {
			if args[name] != "" {
				parts = append(parts, args[name])
				terms = append(terms, Term{Label: "New " + strings.ReplaceAll(name, "_", " "), Value: args[name]})
			}
		}
		text, cut := excerpt(strings.Join(parts, "; "), full)
		truncated = truncated || cut
		if text == "" {
			return ""
		}
		return "“" + text + "”"
	}
	session := "standards draft " + shortID(f.TargetID)
	var what string
	switch f.Invocation.Command + "." + args["action"] {
	case "standards-analysis.start":
		what = "Log analysis of " + repo + ", " + window(args["after"]) + " – " + window(args["before"]) + " UTC"
		if strings.EqualFold(args["dry_run"], "true") {
			what += " (dry run)"
		}
	case "standards-author.start":
		what = "New standard draft"
		if repo != "" {
			what += " for " + repo
		}
		what = join(what, quote("Idea", args["intent"]))
		if args["category_ids"] != "" {
			terms = append(terms, Term{Label: "Categories", Value: args["category_ids"]})
		}
	case "standards-author.answer":
		what = join(version("Answer to "+session), quote("Answer", args["text"]))
	case "standards-author.revise":
		what = join(version("Revision of "+session), quote("Change request", args["text"]))
	case "standards-author.publish":
		what = version("Publication of "+session) + " into the book"
		quote("Your words", args["text"])
	case "standards-author.edit":
		what = join(version("Edit of "+edited()+" in "+session), editValue())
	case "standards-author.continue":
		what = version("Next draft of " + session)
	case "standards-author.validate":
		what = version("Validation of " + session)
	case "standards-author.complete":
		what = version("Completion of " + session + " into a proposal")
	case "standards-author.rebase":
		what = version("Rebase of "+session) + " onto book revision " + args["base"]
	case "standards-proposal.edit":
		what = join(version("Edit of "+edited()+" in standards proposal "+f.TargetID)+base(), editValue())
	case "standards-proposal.approve":
		what = version("Approval of standards proposal "+f.TargetID) + base()
	case "standards-proposal.publish":
		what = version("Publication of standards proposal "+f.TargetID) + " into the book" + base()
	case "standards-exception.propose":
		what = "Exception from standard " + f.TargetID
		if repo != "" {
			what += " for " + repo
		}
		if args["task_ref"] != "" {
			what += ", task " + args["task_ref"]
		}
		if args["expires_at"] != "" {
			what += ", until " + window(args["expires_at"]) + " UTC"
		}
		what = join(what, quote("Departure", args["departure"]))
		quote("Reason", args["reason"])
		quote("Request", args["request"])
	case "standards-exception.approve":
		what = "Approval of standards exception " + f.TargetID
	case "standards-exception.revoke":
		what = join("Revocation of standards exception "+f.TargetID, quote("Reason", args["reason"]))
	default:
		switch f.Invocation.Command {
		case "draft":
			what = "Feature preview"
			if repo != "" {
				what += " for " + repo
			}
			what = join(what, quote("Idea", args["note"]))
		case "confirm":
			what = "Feature issue creation"
			if repo != "" {
				what += " in " + repo
			}
			what += " from preview " + args["code"]
		case "answer":
			what = join("Answer to the research question on "+inRepo(repository(f.TargetID), repo), quote("Answer", args["text"]))
		case "diagnose":
			what = join("Diagnosis of "+inRepo(repository(f.TargetID), repo), quote("Context", args["note"]))
		default:
			what = strings.TrimSpace(strings.ReplaceAll(f.Invocation.Command, "-", " ") + " " + args["action"])
			if repo != "" {
				what += " for " + repo
			}
			if f.TargetID != "" {
				what += ", target " + shortID(f.TargetID)
			}
		}
	}
	return what, terms, truncated
}
