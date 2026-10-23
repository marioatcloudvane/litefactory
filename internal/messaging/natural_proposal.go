package messaging

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/cloudvane/litefactory/internal/store"
)

// naturalProposal is what the owner reads before confirming: one or two plain
// sentences and a short footer. The exact arguments and the full code are
// shown on request (naturalDetails); what runs is always the stored payload.
func naturalProposal(p naturalPayload, code string, at time.Time) string {
	return naturalProposalText(p, at) + "\nSay yes to go ahead, or cancel. (code " + code[:min(4, len(code))] + "…)"
}

func naturalProposalText(p naturalPayload, now time.Time) string {
	args := p.Invocation.Arguments
	project := projectName(p.Project)
	if project == "" {
		project = projectName(args["project"])
	}
	switch p.Invocation.Command {
	case "standards-analysis":
		text := fmt.Sprintf("I'll go through the runs of %s from %s and look for repeated, costly mistakes. That takes about 5 minutes and roughly $1–2; I'll post what I find here.", project, windowPhrase(p.window, args["after"], args["before"], now))
		if args["dry_run"] == "true" {
			text += " It's a dry run, so nothing gets filed."
		}
		switch strings.ToLower(strings.TrimSpace(p.window)) {
		case "last week":
			text += " If you meant the last 7 days instead, just say so."
		case "last month":
			text += " If you meant the last 30 days instead, just say so."
		}
		return text
	case "standards-author", "standards-proposal", "standards-exception":
		return standardsProposalText(p.Invocation.Command+"."+args["action"], args, project)
	case "draft":
		return fmt.Sprintf("I'll draft a feature for %s from your note %s. You'll see the preview first; nothing is created until you confirm it.", project, quoted(args["note"]))
	case "diagnose":
		return fmt.Sprintf("I'll diagnose what's wrong with #%s in %s. Any repair still goes through review on the issue.", args["issue"], project)
	case "answer":
		return fmt.Sprintf("I'll record your answer %s to the research question %s on #%s in %s.", quoted(args["text"]), quoted(p.Question), args["issue"], project)
	case "confirm":
		title, _, _ := strings.Cut(p.Question, "\n")
		return fmt.Sprintf("I'll create the issue %s in %s from the saved preview.", quoted(title), project)
	}
	if project != "" {
		return fmt.Sprintf("I'll run %s for %s.", p.Invocation.Command, project)
	}
	return fmt.Sprintf("I'll run %s.", p.Invocation.Command)
}

func standardsProposalText(op string, args map[string]string, project string) string {
	id := args["id"]
	switch op {
	case "standards-author.start":
		text := "I'll ask the standards author to work out a standard from your idea " + quoted(args["intent"])
		if project != "" {
			text += "; it reads " + project + "'s code and recent incidents"
		}
		return text + ". It takes a few minutes; I'll post the draft here."
	case "standards-author.continue":
		return "I'll ask the standards author for its next draft. It takes a few minutes; I'll post it here."
	case "standards-author.answer":
		return "I'll pass your answer " + quoted(args["text"]) + " to the standards author."
	case "standards-author.edit":
		return "I'll update the draft with your changes."
	case "standards-author.validate":
		return "I'll check the draft against the book."
	case "standards-author.complete":
		return "I'll turn the draft into a proposal. It still needs your approval before it goes into the book."
	case "standards-author.rebase":
		return "I'll move the draft onto the current book."
	case "standards-proposal.edit":
		return "I'll edit proposal " + id + " with your changes."
	case "standards-proposal.approve":
		return "I'll approve proposal " + id + " (version " + args["version"] + "). Publishing it is a separate step."
	case "standards-proposal.publish":
		return "I'll publish proposal " + id + " (version " + args["version"] + ") into the book of standards."
	case "standards-exception.propose":
		return "I'll record an exception request " + quoted(args["departure"]) + ". It still needs your approval."
	case "standards-exception.approve":
		return "I'll approve exception " + id + "."
	case "standards-exception.revoke":
		return "I'll revoke exception " + id + " because " + quoted(args["reason"]) + "."
	}
	return "I'll run " + strings.ReplaceAll(op, ".", " ") + "."
}

// naturalDetails shows the exact stored request on the owner's request.
func naturalDetails(p naturalPayload, d store.NaturalDialogue) string {
	args, _ := json.Marshal(p.Invocation.Arguments)
	return fmt.Sprintf("Exact request: %s\nArguments: %s\nConfirmation code: %s, valid until %s.\nSay yes or send the code to go ahead; cancel drops it.", p.Invocation.Command, args, d.Code, d.ExpiresAt.In(time.Local).Format("15:04"))
}

func projectName(slug string) string {
	if i := strings.LastIndexByte(slug, '/'); i >= 0 {
		return slug[i+1:]
	}
	return slug
}

// quoted keeps owner text short in a sentence; details show all of it.
func quoted(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	if len(text) > 160 {
		cut := 157
		for !utf8.ValidString(text[:cut]) {
			cut--
		}
		text = text[:cut] + "…"
	}
	return "“" + text + "”"
}

// windowPhrase names an analysis window in the owner's local terms, for
// example "last week (Mon 12 – Sun 18 Oct)".
func windowPhrase(phrase, after, before string, now time.Time) string {
	plain := PlainWindow(after, before, now)
	if phrase = strings.TrimSpace(phrase); phrase != "" {
		if f := strings.Fields(phrase); len(f) == 3 && f[1][0] >= '0' && f[1][0] <= '9' {
			phrase = "the " + phrase
		}
		return phrase + " (" + plain + ")"
	}
	return plain
}

// PlainWindow spells out an RFC3339 window in local time: whole days as
// "Mon 12 – Sun 18 Oct", other bounds with the time of day, and a window
// ending about now as "since …".
func PlainWindow(after, before string, now time.Time) string {
	from, e1 := time.Parse(time.RFC3339, after)
	to, e2 := time.Parse(time.RFC3339, before)
	if e1 != nil || e2 != nil {
		return after + " to " + before
	}
	from, to = from.In(time.Local), to.In(time.Local)
	midnight := func(t time.Time) bool { return t.Hour() == 0 && t.Minute() == 0 && t.Second() == 0 }
	if midnight(from) && midnight(to) {
		last := to.AddDate(0, 0, -1)
		switch {
		case !last.After(from):
			return from.Format("Mon 2 Jan")
		case from.Month() == last.Month() && from.Year() == last.Year():
			return from.Format("Mon 2") + " – " + last.Format("Mon 2 Jan")
		}
		return from.Format("Mon 2 Jan") + " – " + last.Format("Mon 2 Jan")
	}
	if d := now.Sub(to); d > -time.Minute && d < 5*time.Minute {
		return "since " + from.Format("Mon 2 Jan 15:04")
	}
	return from.Format("Mon 2 Jan 15:04") + " – " + to.Format("Mon 2 Jan 15:04")
}
