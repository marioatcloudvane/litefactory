package messaging

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/cloudvane/litefactory/internal/store"
	"maps"
	"sort"
	"strings"
	"time"
)

// StandardsOperations is the shared domain adapter. Requests carry host-owned
// provenance; provider envelopes cannot manufacture authorization.
type StandardsOperations interface {
	Read(context.Context, StandardsRequest) (StandardsResult, error)
	Execute(context.Context, StandardsRequest) (StandardsResult, error)
}

// StandardsAuthors optionally reports the author session a conversation
// works on, so the owner's answers reach it without a lookup.
type StandardsAuthors interface {
	AuthorSession(ctx context.Context, channel string) (StandardsAuthorSession, bool, error)
}

// StandardsAuthorSession is the newest unfinished author session started in
// a conversation: its draft rule, its open questions as shown to the owner,
// what still blocks it, and whether the author is drafting right now. Stage
// is "draft", or "proposal" for a completed draft not yet in the book.
type StandardsAuthorSession struct {
	ID                     string
	Version                int64
	Stage                  string
	Running                bool
	Rule                   string
	Questions, QuestionIDs []string
	// Blocking says in plain words what keeps the draft from publishing.
	Blocking []string
	// Ready marks a draft with no open question and nothing blocking.
	Ready bool
}

// StandardsRecovery reconciles retained operations before any attempt to replay.
type StandardsRecovery interface {
	Recover(context.Context, StandardsRequest) (StandardsResult, bool, error)
}

// StandardsRequest identifies one exact retained operation.
type StandardsRequest struct {
	Operation                                              string
	Arguments                                              map[string]string
	Transport, OwnerID, ChannelID, MessageID, OperationKey string
	At                                                     time.Time
}

// StandardsResult contains factual domain output and factory-issued targets.
type StandardsResult struct {
	Text        string
	References  map[string]string
	Interrupted bool
}

// EnableStandards installs the typed adapter before conversation workers start.
func (s *Service) EnableStandards(operations StandardsOperations) {
	s.standards = operations
	if operations == nil {
		return
	}
	for _, name := range []string{"standards", "standards-author", "standards-proposal", "standards-exception", "standards-analysis"} {
		def := CommandDefinition{Name: name, Description: "Discover and inspect retained standards, authoring, proposals, exceptions and log analyses", NaturalLanguage: true, Effect: ChangeEffect}
		switch name {
		case "standards":
			def.Description = "Look up the book of standards. action: discover, book.current, book.history, book.categories, book.standards (rendered, with standards per role and category), book.show (revision), author.list, author.inspect (id, version), proposal.list, proposal.inspect or proposal.review (id, version; review shows before and after), exception.list, exception.show (id), analysis.list (recent log analyses with status), analysis.show (id), operation.show (id)."
		case "standards-author":
			def.Description = "Work out one standard with the standards author, from draft to the book. action start takes the owner's idea as intent, optionally project (the author then reads its code and recent incidents) and target_id to sharpen an existing standard; it is confirmed (it costs money), then the author drafts in the background and the draft is posted here. answer takes the owner's reply as text: answers to the open questions (question_id optional: without it the reply answers all of them) or, with no open question, any change the owner wants (\"make it shorter\", \"category X\", \"suggest something\"); the author then drafts again by itself. edit sets one field to the owner's literal value: category_id (an existing category or the draft's new one), rule (starts with MUST), description, evidence, scope, check (automated, semi_automated or manual) or applies_to (comma-separated roles). continue asks the author for its next draft. publish takes the draft into the book in one step once it has no open questions and nothing blocking: it completes it into a proposal, approves and publishes it; give the owner's words that say so (\"publish it\", \"yes, good\") from the current message as text; it refuses with the reason while something is open. revise takes the owner's change request as text for a completed proposal that is not yet published (stage proposal); the author drafts a new version. validate, complete and rebase are kept for repairs. Every action runs on this conversation's author session and its newest version unless id and version are given; never ask the owner for ids or versions. All but start and complete run without a confirmation."
		case "standards-proposal":
			def.Description = "Propose action edit, approve or publish for an exact proposal id and version, for proposals that did not come from this conversation's author (a draft goes into the book with standards-author publish). Approve and publish need its digest; publish needs an approved version. Edit selects target_id and owner category_id/rule/description/evidence/scope/check/applies_to or exact owner JSON operations."
		case "standards-exception":
			def.Description = "Propose action propose, approve or revoke. Propose requires book_revision, standard_id, repository_id (or project), task_ref, departure, reason, expires_at; approve needs id and digest; revoke needs id and reason."
		case "standards-analysis":
			def.Description = "Propose action start: a log analysis of one project's runs. Give project and window (for example last 7 days, yesterday or last week) or exact after and before times. dry_run true previews what would be filed. It runs in the background and posts its result here."
		}
		if name == "standards" {
			def.Effect = ReadEffect
		}
		for _, arg := range []string{"action", "id", "version", "base", "digest", "mode", "target_id", "intent", "category_ids", "question_id", "text", "replace", "category_id", "rule", "description", "evidence", "scope", "check", "applies_to", "operations", "request", "reason", "after", "before", "window", "dry_run", "limit", "offset", "revision", "repository_id", "project", "book_revision", "standard_id", "task_ref", "departure", "expires_at", "replaces"} {
			def.Arguments = append(def.Arguments, Argument{Name: arg, Type: TextArgument, Required: arg == "action", Description: "Exact retained target or owner supplied content; action selects an enabled operation"})
		}
		s.RegisterCommand(def, nil)
	}
}

func standardsCommand(command string) bool {
	return command == "standards" || strings.HasPrefix(command, "standards-")
}

func standardsOperation(inv Invocation) (string, error) {
	action := inv.Arguments["action"]
	if inv.Command != "standards" {
		action = strings.TrimPrefix(inv.Command, "standards-") + "." + action
	}
	allowed := "book.current book.history book.categories book.standards book.show author.list author.inspect author.start author.answer author.revise author.edit author.continue author.validate author.complete author.rebase author.publish proposal.list proposal.inspect proposal.review proposal.edit proposal.approve proposal.publish exception.propose exception.show exception.list exception.approve exception.revoke analysis.start analysis.list analysis.show operation.show"
	if inv.Command == "standards" && (action == "discover" || action == "help") {
		return "discover", nil
	}
	for _, op := range strings.Fields(allowed) {
		if op != action {
			continue
		}
		read := strings.HasPrefix(op, "book.") || strings.HasSuffix(op, ".inspect") || strings.HasSuffix(op, ".review") || strings.HasSuffix(op, ".show") || strings.HasSuffix(op, ".list")
		if read == (inv.Command == "standards") {
			return op, nil
		}
	}
	return "", errors.New("standards action is unavailable; inspect operations and select one exact action")
}

func (s *Service) standardsRequest(inv Invocation, channel, message string, at time.Time) (StandardsRequest, error) {
	op, err := standardsOperation(inv)
	return StandardsRequest{Operation: op, Arguments: maps.Clone(inv.Arguments), Transport: s.transport, OwnerID: s.owner, ChannelID: channel, MessageID: message, OperationKey: s.transport + ":" + channel + ":" + message, At: at}, err
}

func standardsDiscovery() string {
	return "Standards operations: inspect categories, the published book and history, proposals, authoring sessions, scoped exceptions and log analyses. Authoring: start (confirmed), then answer, edit or continue the draft; once it has no open questions and nothing blocking, publish takes it into the book in one step: it completes it into a proposal, approves and publishes it. revise sends the owner's change request for a completed, unpublished proposal back to the author. Approval and publication of a proposal from elsewhere are separate confirmed actions. A log analysis of one project is started as a confirmed action and posts its result here when it finishes. Inspect saved operation outcomes before repeating interrupted work."
}

func standardsMissingTerms(op string, args map[string]string) string {
	var required []string
	switch op {
	case "author.start":
		required = []string{"intent"}
	case "author.answer", "author.revise", "author.publish":
		// The session defaults to this conversation's; the text is the
		// owner's own words (for publish: the words that say to publish).
		required = []string{"text"}
	case "proposal.edit":
		required = []string{"id", "version"}
	case "proposal.approve", "proposal.publish":
		required = []string{"id", "version", "digest"}
	case "exception.approve":
		required = []string{"id", "digest"}
	case "exception.revoke":
		required = []string{"id", "reason"}
	case "exception.propose":
		if args["request"] == "" {
			required = []string{"book_revision", "standard_id", "repository_id", "task_ref", "departure", "reason", "expires_at"}
		}
	case "analysis.start":
		if args["repository_id"] == "" {
			required = append(required, "project")
		}
		required = append(required, "after", "before")
	}
	var missing []string
	for _, field := range required {
		if strings.TrimSpace(args[field]) == "" {
			missing = append(missing, field)
		}
	}
	if len(missing) > 0 {
		return "Please specify the exact standards terms before changing state: " + strings.Join(missing, ", ") + "."
	}
	return ""
}

// standardsProposeGap is why a standards change proposal cannot run as given,
// in terms the conversation agent can fix, or "" when it can. Terms the
// factory resolves itself (a project, a window) count as given.
func standardsProposeGap(envelope ConversationEnvelope, inv Invocation, current string) string {
	if envelope.Kind != "propose" || !standardsCommand(inv.Command) || inv.Command == "standards" {
		return ""
	}
	op, err := standardsOperation(inv)
	if err != nil {
		return ""
	}
	args := maps.Clone(inv.Arguments)
	if args["project"] != "" {
		args["repository_id"] = args["project"]
	}
	if args["window"] != "" {
		args["after"], args["before"] = args["window"], args["window"]
	}
	if op == "author.publish" && args["text"] != "" && !spansCurrent(envelope, "text", current) {
		return "publish needs the owner's words in the current message that say to publish, spanned as text"
	}
	if missing := standardsMissingTerms(op, args); missing != "" {
		return op + " is missing terms; supply them from owner spans or factory references, never ask the owner for ids or versions. " + missing
	}
	return ""
}

func standardsError(err error) string {
	return fmt.Sprintf("That didn't go through: %s. Nothing else was changed.", strings.TrimPrefix(err.Error(), "standards: "))
}

// standardsDisplay is the saved outcome of a standards operation, with the
// exact operation and references kept for storage and details; the owner
// reads result.Text.
func standardsDisplay(result StandardsResult, key string) string {
	refs, _ := json.Marshal(result.References)
	text, cut := boundedConversationText(result.Text, 16384)
	if cut {
		text += "\nAdditional result content remains in the durable operation receipt."
	}
	return "Saved standards operation: " + key + "\nExact retained references: " + string(refs) + "\n" + text
}

func expandStandardsReferences(refs []ConversationReference) []ConversationReference {
	var out []ConversationReference
	seen := map[string]bool{}
	for _, ref := range refs {
		if seen[ref.ID] {
			continue
		}
		seen[ref.ID] = true
		out = append(out, ref)
		if ref.Arguments["kind"] != "author" {
			continue
		}
		var names []string
		for name := range ref.Arguments {
			if strings.HasPrefix(name, "question_id_") {
				names = append(names, name)
			}
		}
		sort.Strings(names)
		for _, name := range names {
			q := ref
			q.ID = ref.ID + "-" + name
			q.Arguments = map[string]string{"id": ref.Arguments["id"], "version": ref.Arguments["version"], "question_id": ref.Arguments[name]}
			if seen[q.ID] {
				continue
			}
			seen[q.ID] = true
			out = append(out, q)
		}
	}
	if len(out) > 32 {
		out = out[:32]
	}
	return out
}

// conversationAuthorSession tells the agent about the author session this
// conversation works on: its rule, open questions, whether the author is
// drafting right now, and the reference to cite for answers.
type conversationAuthorSession struct {
	Reference string   `json:"reference"`
	Stage     string   `json:"stage,omitempty"`
	Running   bool     `json:"running"`
	Rule      string   `json:"rule,omitempty"`
	Questions []string `json:"questions,omitempty"`
	Blocking  []string `json:"blocking,omitempty"`
	Ready     bool     `json:"ready_to_publish"`
}

// loadAuthor finds the conversation's open author session, if any.
func (r *factoryConversationRun) loadAuthor(ctx context.Context) (*conversationAuthorSession, error) {
	authors, ok := r.s.standards.(StandardsAuthors)
	if !ok {
		return nil, nil
	}
	session, found, err := authors.AuthorSession(ctx, r.c.ChannelRef)
	if err != nil || !found {
		return nil, err
	}
	args := map[string]string{"kind": "author", "id": session.ID, "version": fmt.Sprint(session.Version)}
	for i, id := range session.QuestionIDs {
		args[fmt.Sprintf("question_id_%d", i+1)] = id
	}
	r.authorRef = &ConversationReference{ID: r.t.ID + "-author", OwnerID: r.s.owner, ChannelID: r.c.ChannelRef, OriginTurn: r.t.ID, ExpiresAt: r.at.Add(time.Hour), Arguments: args}
	return &conversationAuthorSession{Reference: r.authorRef.ID, Stage: session.Stage, Running: session.Running, Rule: session.Rule, Questions: session.Questions, Blocking: session.Blocking, Ready: session.Ready}, nil
}

func (r *factoryConversationRun) lookupStandards(ctx context.Context, inv Invocation) (conversationReadResult, *ConversationReference, error) {
	result := conversationReadResult{Command: inv.Command, Arguments: inv.Arguments, Source: "shared standards service", CapturedAt: r.at, Complete: true, Gaps: []string{}}
	if err := conversationAccess(ctx, r.s.conversations.Load(), r.c.ChannelRef, r.at); err != nil {
		return result, nil, err
	}
	request, err := r.s.standardsRequest(inv, r.c.ChannelRef, r.t.SourceID, r.at)
	if err != nil || r.s.standards == nil {
		result.Result = "Standards operations unavailable or ambiguous."
		result.Complete = false
		return result, nil, nil //nolint:nilerr // Invalid requests become visible incomplete evidence, never execution.
	}
	var output StandardsResult
	switch request.Operation {
	case "discover":
		output.Text = standardsDiscovery()
	case "operation.show":
		var receipt store.StandardsConversationOperation
		receipt, err = r.s.db.GetStandardsConversationOperation(ctx, request.Arguments["id"])
		if err == nil && (receipt.OwnerID != r.s.owner || receipt.ChannelID != r.c.ChannelRef || receipt.Transport != r.s.transport) {
			err = store.ErrNotFound
		}
		if err == nil {
			raw, e := json.Marshal(receipt)
			err = e
			output.Text = string(raw)
		}
	default:
		output, err = r.s.standards.Read(ctx, request)
	}
	if err != nil {
		output.Text = standardsError(err)
		result.Complete = false
		result.Gaps = append(result.Gaps, "Shared standards read failed")
	}
	result.Result = output.Text
	if bounded, cut := boundedConversationText(result.Result, 16384); cut {
		result.Result = bounded + "\nAdditional retained content omitted from this bounded lookup; inspect the exact saved target through the shared CLI or request a narrower page."
		result.Complete = false
		result.Gaps = append(result.Gaps, "Standards result exceeded conversation evidence limit")
	}
	ref, err := r.saveStandardsReference(ctx, output.References)
	return result, ref, err
}

func (r *factoryConversationRun) saveStandardsReference(ctx context.Context, args map[string]string) (*ConversationReference, error) {
	if len(args) == 0 {
		return nil, nil
	}
	raw, err := json.Marshal(args)
	if err != nil {
		return nil, err
	}
	ref := store.ConversationReference{ID: r.t.ID + "-standards-" + fmt.Sprint(r.ordinal), TurnID: r.t.ID, Kind: "standards", Content: string(raw), ExpiresAt: r.at.Add(24 * time.Hour)}
	if err = r.s.db.SaveConversationReference(ctx, r.t, ref); err != nil {
		return nil, err
	}
	return &ConversationReference{ID: ref.ID, OwnerID: r.s.owner, ChannelID: r.c.ChannelRef, OriginTurn: r.t.ID, ExpiresAt: ref.ExpiresAt, Arguments: maps.Clone(args)}, nil
}

func (s *Service) recoverStandardsConversationAction(ctx context.Context, t store.ConversationTurn, at time.Time) error {
	recovery, ok := s.standards.(StandardsRecovery)
	if !ok {
		return nil
	}
	link, err := s.db.GetConversationActionLink(ctx, t.ID)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if link.ExecutionID == "" {
		return nil
	}
	execution, err := s.db.ReadNaturalExecution(ctx, link.ExecutionID)
	if errors.Is(err, store.ErrNotFound) {
		// A step run without confirmation keeps its receipt in the domain.
		return nil
	}
	if err != nil {
		return err
	}
	if execution.State != "executing" && execution.State != "authorized" {
		return nil
	}
	var p naturalPayload
	if err = json.Unmarshal([]byte(execution.Payload), &p); err != nil {
		return err
	}
	if !standardsCommand(p.Invocation.Command) {
		return nil
	}
	request, err := s.standardsRequest(p.Invocation, execution.ChannelRef, t.SourceID, at)
	request.OperationKey = s.transport + ":" + execution.ChannelRef + ":" + link.ExecutionID
	if err != nil {
		return err
	}
	result, found, err := recovery.Recover(ctx, request)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	protected, err := s.Protect(ctx, result.Text)
	if err != nil {
		return err
	}
	return s.db.FinishNaturalExecution(ctx, link.ExecutionID, protected, result.Interrupted, at)
}
