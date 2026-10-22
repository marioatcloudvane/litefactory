package messaging

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/cloudvane/litefactory/internal/store"
	"maps"
	"sort"
	"strconv"
	"strings"
	"time"
)

// StandardsOperations is the shared domain adapter. Requests carry host-owned
// provenance; provider envelopes cannot manufacture authorization.
type StandardsOperations interface {
	Read(context.Context, StandardsRequest) (StandardsResult, error)
	Execute(context.Context, StandardsRequest) (StandardsResult, error)
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
	for _, name := range []string{"standards", "standards-author", "standards-proposal", "standards-exception", "standards-schedule"} {
		def := CommandDefinition{Name: name, Description: "Discover and inspect retained standards, authoring, proposals, exceptions and analyst schedules", NaturalLanguage: true, Effect: ChangeEffect}
		switch name {
		case "standards":
			def.Description = "Look up the book of standards. action: discover, book.current, book.history, book.categories, book.standards, book.show (revision), author.list, author.inspect (id, version), proposal.list, proposal.inspect or proposal.review (id, version; review shows before and after), exception.list, exception.show (id), schedule.list, schedule.show or schedule.activity (id), notice.list, operation.show (id)."
		case "standards-author":
			def.Description = "Propose action start, answer, edit, continue, validate, complete or rebase. Use exact id/version and owner spans for intent/text/title/body. Answer requires one exact question_id. Completion creates an unapproved proposal."
		case "standards-proposal":
			def.Description = "Propose action edit, approve or publish for an exact proposal id and version. Approve and publish need its digest; publish needs an approved version. Edit selects target_id and owner title/body/category_ids or exact owner JSON operations."
		case "standards-exception":
			def.Description = "Propose action propose, approve or revoke. Propose requires book_revision, standard_id, repository_id (or project), task_ref, departure, reason, expires_at; approve needs id and digest; revoke needs id and reason."
		case "standards-schedule":
			def.Description = "Propose action create, change, enable or disable. Create/change need name, kind, repository_id, interval_seconds and finite run_limit plus explicit scope. Changes need exact id/version. Create disabled; enable separately."
		}
		if name == "standards" {
			def.Effect = ReadEffect
		}
		for _, arg := range []string{"action", "id", "version", "base", "digest", "mode", "target_id", "intent", "category_ids", "question_id", "text", "replace", "title", "body", "operations", "request", "reason", "config", "after", "limit", "offset", "revision", "name", "kind", "repository_id", "project", "interval_seconds", "run_limit", "task_refs", "modes", "ref", "paths", "issues", "book_revision", "standard_id", "task_ref", "departure", "expires_at", "replaces"} {
			def.Arguments = append(def.Arguments, Argument{Name: arg, Type: TextArgument, Required: arg == "action", Description: "Exact retained target or owner supplied content; action selects an enabled operation"})
		}
		s.RegisterCommand(def, nil)
	}
}

func standardsInterval(value string) string {
	text := strings.ToLower(strings.TrimSpace(value))
	for _, entry := range []struct {
		phrases string
		seconds int64
	}{{"hourly|every hour", 3600}, {"daily|every day", 86400}, {"weekly|every week", 604800}} {
		for _, phrase := range strings.Split(entry.phrases, "|") {
			if text == phrase {
				return strconv.FormatInt(entry.seconds, 10)
			}
		}
	}
	if d, err := time.ParseDuration(text); err == nil && d > 0 && d%time.Second == 0 {
		return strconv.FormatInt(int64(d/time.Second), 10)
	}
	fields := strings.Fields(strings.TrimPrefix(text, "every "))
	if len(fields) == 2 {
		n, err := strconv.ParseInt(fields[0], 10, 32)
		if err == nil && n > 0 {
			var multiplier int64
			switch strings.TrimSuffix(fields[1], "s") {
			case "hour":
				multiplier = 3600
			case "day":
				multiplier = 86400
			case "week":
				multiplier = 604800
			}
			if multiplier > 0 {
				return strconv.FormatInt(n*multiplier, 10)
			}
		}
	}
	return value
}

func standardsCommand(command string) bool {
	return command == "standards" || strings.HasPrefix(command, "standards-")
}

func standardsOperation(inv Invocation) (string, error) {
	action := inv.Arguments["action"]
	if inv.Command != "standards" {
		action = strings.TrimPrefix(inv.Command, "standards-") + "." + action
	}
	allowed := "book.current book.history book.categories book.standards book.show author.list author.inspect author.start author.answer author.edit author.continue author.validate author.complete author.rebase proposal.list proposal.inspect proposal.review proposal.edit proposal.approve proposal.publish exception.propose exception.show exception.list exception.approve exception.revoke schedule.create schedule.show schedule.list schedule.change schedule.enable schedule.disable schedule.activity notice.list operation.show"
	if inv.Command == "standards" && (action == "discover" || action == "help") {
		return "discover", nil
	}
	for _, op := range strings.Fields(allowed) {
		if op != action {
			continue
		}
		read := strings.HasPrefix(op, "book.") || strings.HasSuffix(op, ".inspect") || strings.HasSuffix(op, ".review") || strings.HasSuffix(op, ".show") || strings.HasSuffix(op, ".list") || strings.HasSuffix(op, ".activity")
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
	return "Standards operations: inspect categories, the published book and history, proposals, authoring sessions, scoped exceptions, analyst schedules and retained notices. Authoring: start, answer a saved question, edit, continue, validate, complete or rebase. Completion creates an unapproved proposal. Approval and publication of one exact proposal version are separate confirmed actions. Schedules are created disabled; enabling is a separate confirmation. Inspect saved operation outcomes before repeating interrupted work."
}

func standardsMissingTerms(op string, args map[string]string) string {
	var required []string
	switch op {
	case "author.start":
		required = []string{"intent"}
	case "author.answer":
		required = []string{"id", "version", "question_id", "text"}
	case "author.edit", "author.continue", "author.validate", "author.complete", "author.rebase", "proposal.edit", "schedule.enable", "schedule.disable":
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
	case "schedule.create", "schedule.change":
		if op == "schedule.change" {
			required = append(required, "id", "version")
		}
		if args["config"] == "" {
			required = append(required, "name", "kind", "repository_id", "interval_seconds", "run_limit")
		}
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

func standardsError(err error) string {
	return fmt.Sprintf("Standards operation failed or conflicted: %v. Inspect the retained target before requesting a new change.", err)
}

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
