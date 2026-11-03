package messaging

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/cloudvane/litefactory/internal/factory/contracts"
	"github.com/cloudvane/litefactory/internal/messagingstate"
	"maps"
	"sort"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/factory/owneractions"
	"github.com/cloudvane/litefactory/internal/owneraction"
	"github.com/cloudvane/litefactory/internal/store"
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

// StandardsRequest and StandardsResult retain the existing adapter contract.
type StandardsRequest = owneractions.StandardsRequest

// StandardsResult retains the factual standards adapter output.
type StandardsResult = owneractions.StandardsResult

// EnableStandards installs the typed adapter before conversation workers start.
func (s *Service) EnableStandards(operations StandardsOperations) {
	s.standards = operations
	if operations == nil {
		return
	}
	for _, name := range []string{"standards", "standards-author", "standards-proposal", "standards-exception", "standards-analysis"} {
		def, _ := owneractions.StandardsDefinition(name)
		s.RegisterCommand(def, nil)
	}
}

func standardsCommand(command string) bool { return owneractions.StandardsCommand(command) }
func standardsOperation(inv Invocation) (string, error) {
	return owneractions.StandardsOperation(owneraction.Invocation{Command: inv.Command, Arguments: inv.Arguments})
}

func (s *Service) standardsRequest(inv Invocation, channel, message string, at time.Time) (StandardsRequest, error) {
	op, err := standardsOperation(inv)
	return StandardsRequest{Operation: op, Arguments: maps.Clone(inv.Arguments), Transport: s.transport, OwnerID: s.owner, ChannelID: channel, MessageID: message, OperationKey: s.transport + ":" + channel + ":" + message, At: at}, err
}

func standardsDiscovery() string {
	return "Standards reads: categories, published book and history, proposals, authoring sessions, exceptions, analyses and operation receipts. Every change is a fixed proposal. Conversational tier: analysis start; author start, answer, revise, edit, continue, validate, complete and rebase; proposal edit; exception propose. Binding tier: author publish, proposal approve/publish and exception approve/revoke; conversational confirmation plus the configured owner's Discord tap. Feature preview, diagnosis and Research answers are conversational; feature issue creation is binding. Reads, inspect/history and direct confirm/cancel controls require no nested proposal. The minimum delay is five seconds after availability; expiry is fifteen minutes after creation. Uncertain effects retain their recovery receipts."
}

func standardsMissingTerms(op string, args map[string]string) string {
	return owneractions.StandardsMissingTerms(op, args)
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
		result.Result = "Standards operations are unavailable or ambiguous. Ask the factory operator to inspect standards service availability on the factory host; no standards action was performed."
		result.Complete = false
		return result, nil, nil //nolint:nilerr // Invalid requests become visible incomplete evidence, never execution.
	}
	var output StandardsResult
	switch request.Operation {
	case "discover":
		output.Text = standardsDiscovery()
	case "operation.show":
		var receipt contracts.StandardsConversationOperation
		receipt, err = r.s.factory.StandardsOperation(ctx, request.Arguments["id"])
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
	ref := messagingstate.ConversationReference{ID: r.t.ID + "-standards-" + fmt.Sprint(r.ordinal), TurnID: r.t.ID, Kind: "standards", Content: string(raw), ExpiresAt: r.at.Add(24 * time.Hour)}
	if err = r.s.db.SaveConversationReference(ctx, r.t, ref); err != nil {
		return nil, err
	}
	return &ConversationReference{ID: ref.ID, OwnerID: r.s.owner, ChannelID: r.c.ChannelRef, OriginTurn: r.t.ID, ExpiresAt: ref.ExpiresAt, Arguments: maps.Clone(args)}, nil
}

func (s *Service) recoverStandardsConversationAction(ctx context.Context, t messagingstate.ConversationTurn, at time.Time) error {
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
