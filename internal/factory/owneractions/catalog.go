package owneractions

import (
	"fmt"
	"github.com/cloudvane/litefactory/internal/owneraction"
	"sort"
)

// Availability contains trusted host facts about installed command families.
// Individual flags allow optional hosts to expose only their configured effects.
type Availability struct {
	Features, Research, Standards    bool
	Draft, Confirm, Answer, Diagnose bool
	// Brainstorm enables brainstorm_create_tasks and brainstorm_task_recreate.
	Brainstorm bool
}

// Allows reports availability of an existing changing command.
func (a Availability) Allows(command string) bool {
	switch command {
	case "draft":
		return a.Features || a.Draft
	case "confirm":
		return a.Features || a.Confirm
	case "answer":
		return a.Research || a.Answer
	case "diagnose":
		return a.Research || a.Diagnose
	case BrainstormCreateTasks, BrainstormTaskRecreate:
		return a.Brainstorm
	default:
		return a.Standards && StandardsCommand(command) && command != "standards"
	}
}

// ChangeDefinition returns a fresh copy of the factory's fixed changing schema.
func ChangeDefinition(command string) (CommandDefinition, bool) {
	base := []Argument{{Name: "issue", Type: IntegerArgument, Required: true, Description: "Positive issue number"}}
	project := Argument{Name: "project", Type: TextArgument, Description: "Managed owner/repo or unique repo; defaults to the only registered project"}
	switch command {
	case "draft":
		return CommandDefinition{Name: "draft", NaturalLanguage: true, Effect: ChangeEffect, Description: "Preview a feature issue", Arguments: []Argument{
			{Name: "project", Type: TextArgument, Description: "Registered owner/name or unique name"},
			{Name: "note", Type: TextArgument, Description: "Feature idea or change, up to 2000 bytes"},
		}}, true
	case "confirm":
		return CommandDefinition{Name: "confirm", NaturalLanguage: true, Effect: ChangeEffect, Description: "Create the previewed feature issue", Arguments: []Argument{{Name: "code", Type: TextArgument, Required: true, Description: "8-character preview code"}}}, true
	case "diagnose":
		return CommandDefinition{Name: "diagnose", NaturalLanguage: true, Effect: ChangeEffect, Description: "Explain what is wrong or stuck", Arguments: append(append([]Argument(nil), base...), project, Argument{Name: "note", Type: TextArgument, Description: "Optional diagnosis context"})}, true
	case "answer":
		return CommandDefinition{Name: "answer", NaturalLanguage: true, Effect: ChangeEffect, Description: "Answer the current research question", Arguments: append(append([]Argument(nil), base...), Argument{Name: "revision", Type: TextArgument, Required: true, Description: "Current 12-character question token"}, Argument{Name: "text", Type: TextArgument, Required: true, Description: "Answer, up to 65536 bytes"}, project)}, true
	case BrainstormCreateTasks:
		return CommandDefinition{Name: command, NaturalLanguage: true, Effect: ChangeEffect, Description: "Create GitHub issues from kept brainstorm drafts and start Plan for each, or continue the batch that already holds them", Arguments: []Argument{
			{Name: "brainstorm", Type: TextArgument, Required: true, Description: "Brainstorm id"},
			{Name: "draft_set", Type: TextArgument, Required: true, Description: "Draft set id"},
			{Name: "drafts", Type: TextArgument, Required: true, Description: "Comma-separated kept draft keys"},
		}}, true
	case BrainstormTaskRecreate:
		return CommandDefinition{Name: command, NaturalLanguage: true, Effect: ChangeEffect, Description: "Create a second issue for a draft whose first create stayed uncertain; the first issue may still appear", Arguments: []Argument{
			{Name: "batch", Type: TextArgument, Required: true, Description: "Brainstorm task batch id"},
			{Name: "draft", Type: TextArgument, Required: true, Description: "Draft key in that batch"},
		}}, true
	default:
		if command != "standards" && StandardsCommand(command) {
			return StandardsDefinition(command)
		}
		return CommandDefinition{}, false
	}
}

// StandardsDefinition returns the existing standards command presentation and schema.
func StandardsDefinition(name string) (CommandDefinition, bool) {
	if !StandardsCommand(name) {
		return CommandDefinition{}, false
	}
	def := CommandDefinition{Name: name, Description: "Discover and inspect retained standards, authoring, proposals, exceptions and log analyses", NaturalLanguage: true, Effect: ChangeEffect}
	switch name {
	case "standards":
		def.Description = "Look up the book of standards. action: discover, book.current, book.history, book.categories, book.standards (rendered, with standards per role and category), book.show (revision), author.list, author.inspect (id, version), proposal.list, proposal.inspect or proposal.review (id, version; review shows before and after), exception.list, exception.show (id), analysis.list (recent log analyses with status), analysis.show (id), operation.show (id)."
	case "standards-author":
		def.Description = "Work out one standard with the standards author, from draft to the book. action start takes the owner's idea as intent, optionally project (the author then reads its code and recent incidents) and target_id to sharpen an existing standard; it requires conversational confirmation (it costs money), then the author produces its recorded draft. answer takes the owner's reply as text: answers to the open questions (question_id optional: without it the reply answers all of them) or, with no open question, any change the owner wants (\"make it shorter\", \"category X\", \"suggest something\"); the author then drafts again by itself. edit sets one field to the owner's literal value: category_id (an existing category or the draft's new one), rule (starts with MUST), description, evidence, scope, check (automated, semi_automated or manual) or applies_to (comma-separated roles). continue asks the author for its next draft. publish takes the draft into the book in one step once it has no open questions and nothing blocking: it completes it into a proposal, approves and publishes it; give the owner's words that say so (\"publish it\", \"yes, good\") from the current message as text; it refuses with the reason while something is open. revise takes the owner's change request as text for a completed proposal that is not yet published (stage proposal); the author drafts a new version. validate, complete and rebase are kept for repairs. Every action freezes the selected author session, version and book revision while proposing. All author changes require separate conversational confirmation; publication also requires the configured owner's Discord reaction."
	case "standards-proposal":
		def.Description = "Propose action edit, approve or publish for an exact proposal id and version, for proposals that did not come from this conversation's author (a draft goes into the book with standards-author publish). Approve and publish need its digest; publish needs an approved version. Edit selects target_id and owner category_id/rule/description/evidence/scope/check/applies_to or exact owner JSON operations."
	case "standards-exception":
		def.Description = "Propose action propose, approve or revoke. Propose requires book_revision, standard_id, repository_id (or project), task_ref, departure, reason, expires_at; approve needs id and digest; revoke needs id and reason."
	case "standards-analysis":
		def.Description = "Propose action start: a log analysis of one project's runs. Give project and window in the factory's local time (today, today since 2 pm, since 14:00, since 2 pm yesterday, yesterday, last N days, last N hours, this week, last week) or exact after and before times. dry_run true previews what would be filed. It runs in the background and posts its result here."
	}
	if name == "standards" {
		def.Effect = ReadEffect
	}
	for _, arg := range []string{"action", "id", "version", "base", "digest", "mode", "target_id", "intent", "category_ids", "question_id", "text", "replace", "category_id", "rule", "description", "evidence", "scope", "check", "applies_to", "operations", "request", "reason", "after", "before", "window", "dry_run", "limit", "offset", "revision", "repository_id", "project", "book_revision", "standard_id", "task_ref", "departure", "expires_at", "replaces"} {
		def.Arguments = append(def.Arguments, Argument{Name: arg, Type: TextArgument, Required: arg == "action", Description: "Exact retained target or owner supplied content; action selects an enabled operation"})
	}
	return def, true
}

// ValidateInvocation checks a fixed changing schema against trusted availability facts.
// It preserves field diagnostics for conversational correction without granting execution.
func (a Availability) ValidateInvocation(in Invocation) (Invocation, CommandEffect, error) {
	def, ok := ChangeDefinition(in.Command)
	if !ok || !a.Allows(in.Command) {
		return Invocation{}, "", fmt.Errorf("%w: command unavailable", ErrInvalidInvocation)
	}
	return ValidateDefinition(def, in)
}

// ValidateChange checks the existing owner action input error boundary.
func (a Availability) ValidateChange(in owneraction.Invocation) (owneraction.Invocation, error) {
	validated, effect, err := a.ValidateInvocation(Invocation{Command: in.Command, Arguments: in.Arguments})
	if err != nil || effect != ChangeEffect {
		return owneraction.Invocation{}, ErrInvalidInvocation
	}
	return owneraction.Invocation{Command: validated.Command, Arguments: validated.Arguments}, nil
}

// ActionCatalog derives action discovery from the same fixed schemas as validation.
func (a Availability) ActionCatalog() []owneraction.ActionDefinition {
	var out []owneraction.ActionDefinition
	names := []string{"draft", "confirm", "diagnose", "answer", BrainstormCreateTasks, BrainstormTaskRecreate, "standards-author", "standards-proposal", "standards-exception", "standards-analysis"}
	sort.Strings(names)
	for _, name := range names {
		if !a.Allows(name) {
			continue
		}
		def, _ := ChangeDefinition(name)
		actions := []string{""}
		switch def.Name {
		case "standards-author":
			actions = []string{"start", "answer", "revise", "edit", "continue", "validate", "complete", "rebase", "publish"}
		case "standards-proposal":
			actions = []string{"edit", "approve", "publish"}
		case "standards-exception":
			actions = []string{"propose", "approve", "revoke"}
		case "standards-analysis":
			actions = []string{"start"}
		}
		for _, action := range actions {
			tier, scope, err := owneraction.Classify(owneraction.Invocation{Command: def.Name, Arguments: map[string]string{"action": action}})
			if err != nil {
				continue
			}
			entry := owneraction.ActionDefinition{Command: def.Name, Subaction: action, Tier: tier, Scope: scope}
			for _, arg := range def.Arguments {
				entry.Arguments = append(entry.Arguments, owneraction.ArgumentDefinition{Name: arg.Name, Type: string(arg.Type), Required: arg.Required, MaxBytes: ArgumentLimit(def.Name, arg.Name)})
			}
			out = append(out, entry)
		}
	}
	return out
}
