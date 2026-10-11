package messaging

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/messaging/ask"
	"github.com/cloudvane/litefactory/internal/messaging/presentation"
	"github.com/cloudvane/litefactory/internal/store"
)

type naturalPayload struct {
	Turns                                      []DialogueTurn `json:"turns,omitempty"`
	Invocation                                 Invocation     `json:"invocation"`
	Prompt                                     string         `json:"prompt,omitempty"`
	PreviewID, PreviewHash, Question, Revision string
	RepoID                                     int64
	Project, Channel                           string
}

// EnableNatural installs suggestion-only interpretation before the worker starts.
func (s *Service) EnableNatural(i Interpreter) { s.interpreter = i }

// NaturalStep processes one ordered owner message. Delivery and execution stay
// in Step, so a slow model cannot hold up intake's independent polling loop.
func (s *Service) NaturalStep(ctx context.Context, at time.Time) (bool, error) {
	s.stepMu.Lock()
	defer s.stepMu.Unlock()
	if s.interpreter == nil {
		return false, nil
	}
	if err := s.db.PurgeNaturalChat(ctx, at); err != nil {
		return false, err
	}
	channels := make([]string, 0, len(s.channels))
	for c := range s.channels {
		channels = append(channels, c)
	}
	sort.Strings(channels)
	for _, c := range channels {
		messages, err := s.db.PendingNaturalMessages(ctx, c, 1)
		if err != nil {
			return false, err
		}
		if len(messages) == 0 {
			continue
		}
		m := messages[0]
		if m.OwnerID != s.owner || !s.channels[m.ChannelRef] || (m.Transport != "" && m.Transport != s.transport) || !at.Before(m.CreatedAt.Add(24*time.Hour)) {
			return true, s.db.FinishNaturalMessage(ctx, c, m.SourceID, at)
		}
		if err = s.naturalTurn(ctx, m, at); err != nil {
			return true, err
		}
		return true, s.db.FinishNaturalMessage(ctx, c, m.SourceID, at)
	}
	return false, nil
}

func (s *Service) naturalReply(ctx context.Context, m store.NaturalMessage, text string, status presentation.Status, at time.Time) error {
	if s.Protect != nil {
		var err error
		text, err = s.Protect(ctx, text)
		if err != nil {
			return err
		}
	}
	_, err := s.db.AdmitChatReply(ctx, store.ChatReply{Transport: s.transport, MessageID: "nl:" + m.SourceID, ChannelRef: m.ChannelRef, Command: "natural", ReplyText: text, CreatedAt: at, PresentationState: "pending", OutcomeStatus: string(status)})
	return err
}
func naturalDigest(p naturalPayload) (string, string, error) {
	raw, err := json.Marshal(p)
	if err != nil {
		return "", "", err
	}
	h := sha256.Sum256(raw)
	return string(raw), hex.EncodeToString(h[:]), nil
}
func naturalLater(id, previous string) bool {
	n, e := strconv.ParseUint(id, 10, 64)
	p, f := strconv.ParseUint(previous, 10, 64)
	return e == nil && f == nil && n > p
}
func naturalConfirmation(text string, d store.NaturalDialogue, reply string) bool {
	text = strings.ToLower(strings.TrimSpace(text))
	if text == "confirm "+strings.ToLower(d.Code) {
		return true
	}
	return reply == d.PromptID && d.PromptID != "" && (text == "yes" || text == "yes please" || text == "confirm" || text == "do it")
}
func naturalCancel(text string) bool {
	switch strings.ToLower(strings.TrimSpace(text)) {
	case "cancel", "no", "never mind", "nevermind":
		return true
	}
	return false
}
func (s *Service) naturalTurn(ctx context.Context, m store.NaturalMessage, at time.Time) error {
	d, err := s.db.ReadNaturalDialogue(ctx, s.transport, s.owner, m.ChannelRef)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}
	if errors.Is(err, store.ErrNotFound) {
		d = store.NaturalDialogue{Transport: s.transport, OwnerID: s.owner, ChannelRef: m.ChannelRef}
	}
	var p naturalPayload
	if d.Payload != "" {
		if err = json.Unmarshal([]byte(d.Payload), &p); err != nil {
			return err
		}
	}
	// Restart after a saved revision reuses its exact prompt and never calls the model again.
	if d.SourceID == m.SourceID {
		if p.Prompt != "" {
			return s.naturalReply(ctx, m, p.Prompt, presentation.Success, at)
		}
		return s.naturalReply(ctx, m, "This request was interrupted before a proposal was saved. Nothing was authorized; please send it again or use /help.", presentation.Uncertain, at)
	}
	if d.State == "authorized" || d.State == "executing" {
		return s.naturalReply(ctx, m, "The confirmed action is already in progress. Check its outcome before sending another request.", presentation.Uncertain, at)
	}
	if naturalCancel(m.Text) {
		if d.State == "clarifying" || d.State == "awaiting_confirmation" {
			if err = s.db.EndNaturalDialogue(ctx, d, "cancelled", at); err != nil {
				return err
			}
		}
		return s.naturalReply(ctx, m, "Cancelled the pending request.", presentation.Success, at)
	}
	if d.State == "awaiting_confirmation" && naturalConfirmation(m.Text, d, m.ReplyTo) {
		if d.PromptID == "" || !at.Before(d.ExpiresAt) || !naturalLater(m.SourceID, d.SourceID) {
			return s.naturalReply(ctx, m, "This proposal is not available for confirmation. Please request a fresh proposal.", presentation.Refusal, at)
		}
		_, digest, e := naturalDigest(p)
		if e != nil {
			return e
		}
		if digest != d.Digest {
			return s.naturalReply(ctx, m, "The proposal changed; please request it again.", presentation.Refusal, at)
		}
		if refusal, e := s.recheckNatural(ctx, p, at); e != nil {
			return e
		} else if refusal != "" {
			if e = s.db.EndNaturalDialogue(ctx, d, "cancelled", at); e != nil {
				return e
			}
			return s.naturalReply(ctx, m, refusal, presentation.Refusal, at)
		}
		invocation, effect, e := s.ValidateInvocation(p.Invocation)
		if e != nil || effect != ChangeEffect {
			return s.naturalReply(ctx, m, "This action is no longer available; try /help.", presentation.Refusal, at)
		}
		r := store.ChatReply{Transport: s.transport, ChannelRef: m.ChannelRef, MessageID: "nl-exec:" + d.Code, Command: invocation.Command, Arguments: invocation.Arguments, CreatedAt: at, PresentationState: "pending", ResolvedRepoID: p.RepoID}
		return s.db.AuthorizeNaturalDialogue(ctx, d, m.SourceID, r, at)
	}
	// Bare, quoted and stale confirmations cannot become a new interpreted action.
	lower := strings.ToLower(strings.TrimSpace(m.Text))
	if lower == "yes" || lower == "confirm" || strings.HasPrefix(lower, "confirm ") {
		return s.naturalReply(ctx, m, "Reply directly to the current delivered proposal with yes, or use its exact confirmation code. Nothing was authorized.", presentation.Refusal, at)
	}
	if d.State != "clarifying" && d.State != "awaiting_confirmation" {
		p = naturalPayload{}
	}
	p.Turns = append(p.Turns, DialogueTurn{SourceID: m.SourceID, Text: m.Text})
	if len(p.Turns) > 8 {
		p.Turns = p.Turns[len(p.Turns)-8:]
	}
	// Invalidating a proposal happens before a correction reaches any provider.
	p.Invocation = Invocation{}
	p.Prompt = ""
	p.PreviewID = ""
	p.PreviewHash = ""
	p.Question = ""
	p.Revision = ""
	p.RepoID = 0
	payload, _, err := naturalDigest(p)
	if err != nil {
		return err
	}
	if len(payload) > 32768 {
		if d.State == "clarifying" || d.State == "awaiting_confirmation" {
			if err = s.db.EndNaturalDialogue(ctx, d, "cancelled", at); err != nil {
				return err
			}
		}
		return s.naturalReply(ctx, m, "This dialogue is too large to retain safely. Please send one shorter request or use /help.", presentation.Refusal, at)
	}
	old := d.Revision
	d.Revision++
	d.State = "clarifying"
	d.SourceID = m.SourceID
	d.Payload = payload
	d.Digest = ""
	d.Code = ""
	d.PromptID = ""
	d.UpdatedAt = at
	d.ExpiresAt = at.Add(15 * time.Minute)
	if err = s.db.SaveNaturalDialogue(ctx, d, old); err != nil {
		return err
	}
	request := InterpretationRequest{Turns: p.Turns, Commands: s.NaturalCommands()}
	work, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	raw, modelErr := s.interpreter.Interpret(work, request)
	interpretation, invocation, decodeErr := DecodeInterpretation(raw, request)
	if modelErr != nil || decodeErr != nil || work.Err() != nil {
		if err = s.db.EndNaturalDialogue(ctx, d, "finished", at); err != nil {
			return err
		}
		return s.naturalReply(ctx, m, "I could not safely understand this request. Nothing was applied; please use /help or send one clear action and target.", presentation.Refusal, at)
	}
	if interpretation.Kind == "ignore" || interpretation.Kind == "unsupported" {
		if err = s.db.EndNaturalDialogue(ctx, d, "finished", at); err != nil {
			return err
		}
		if interpretation.Kind == "ignore" {
			return nil
		}
		return s.naturalReply(ctx, m, "That action is not enabled here. Try /help.", presentation.Refusal, at)
	}
	if interpretation.Kind == "clarify" {
		p.Prompt = interpretation.Question
	} else {
		p.Invocation = invocation
		p.Channel = m.ChannelRef
		question, e := s.resolveNatural(ctx, &p, at)
		if e != nil {
			return e
		}
		if question != "" {
			p.Prompt = question
		} else {
			_, effect, e := s.ValidateInvocation(p.Invocation)
			if e != nil {
				return e
			}
			if effect == ReadEffect {
				if e = s.db.EndNaturalDialogue(ctx, d, "finished", at); e != nil {
					return e
				}
				_, _, e = s.Admit(ctx, Message{Transport: s.transport, ID: "nl-read:" + m.SourceID, Channel: m.ChannelRef, Sender: s.owner, Command: p.Invocation.Command, Arguments: p.Invocation.Arguments}, at)
				return e
			}
			var code [8]byte
			if _, e = rand.Read(code[:]); e != nil {
				return e
			}
			d.Code = hex.EncodeToString(code[:])
			d.State = "awaiting_confirmation"
			p.Prompt = s.naturalProposal(p, d.Code)
			if replySize(p.Prompt) > s.ReplyLimit()-32 {
				if e = s.db.EndNaturalDialogue(ctx, d, "finished", at); e != nil {
					return e
				}
				return s.naturalReply(ctx, m, "The exact proposal is too large to show safely here. Please shorten the request or use the slash command.", presentation.Refusal, at)
			}
		}
	}
	if s.Protect != nil {
		protected, protectErr := s.Protect(ctx, p.Prompt)
		err = protectErr
		if err != nil {
			return err
		}
		if d.State == "awaiting_confirmation" && protected != p.Prompt {
			// Whole-prompt protection may not silently change displayed payload.
			if err = s.db.EndNaturalDialogue(ctx, d, "cancelled", at); err != nil {
				return err
			}
			return s.naturalReply(ctx, m, "I could not preserve the exact protected proposal. Nothing was authorized; please shorten the request.", presentation.Refusal, at)
		}
		p.Prompt = protected
	}
	old = d.Revision
	d.Revision++
	d.Payload, d.Digest, err = naturalDigest(p)
	if err != nil {
		return err
	}
	if err = s.db.SaveNaturalDialogue(ctx, d, old); err != nil {
		return err
	}
	return s.naturalReply(ctx, m, p.Prompt, presentation.Success, at)
}

func (s *Service) resolveNatural(ctx context.Context, p *naturalPayload, at time.Time) (string, error) {
	args := p.Invocation.Arguments
	if p.Invocation.Command == "ask" || p.Invocation.Command == "draft" || p.Invocation.Command == "diagnose" || p.Invocation.Command == "question" || p.Invocation.Command == "answer" || args["project"] != "" {
		resolution, err := ask.ResolveProject(ctx, s.db, args["project"], nil)
		if err != nil {
			return "", err
		}
		if resolution.Refusal != "" {
			return resolution.Refusal, nil
		}
		args["project"] = resolution.Repository.Slug()
		p.Project = args["project"]
		p.RepoID = resolution.Repository.ID
	}
	for _, field := range []string{"note", "text", "question"} {
		if args[field] != "" && s.Protect != nil {
			v, err := s.Protect(ctx, args[field])
			if err != nil {
				return "", err
			}
			args[field] = v
		}
	}
	switch p.Invocation.Command {
	case "answer":
		ref, refusal := s.researchTarget(ctx, Message{Arguments: args}, true)
		if refusal != "" {
			return refusal, nil
		}
		if s.research == nil {
			return "No research answer is available here.", nil
		}
		q, err := s.research.Current(ctx, ref)
		if err != nil {
			return "", err
		}
		if q.Text == "" || q.Token != args["revision"] {
			return "Please provide the current research question revision from /question.", nil
		}
		p.Question, p.Revision = q.Text, q.Token
	case "confirm":
		preview, err := s.db.ChatIssuePreviewByCode(ctx, s.transport, p.Channel, args["code"])
		if errors.Is(err, store.ErrNotFound) {
			return "Please provide the current preview code from /draft.", nil
		}
		if err != nil {
			return "", err
		}
		if preview.State != "previewed" || !at.Before(preview.ExpiresAt) {
			return "The saved preview is no longer available; create a new draft.", nil
		}
		repos, err := s.db.ListRepos(ctx)
		if err != nil {
			return "", err
		}
		for _, repo := range repos {
			if repo.ID == preview.RepoID {
				p.Project = repo.Slug()
			}
		}
		if p.Project == "" {
			return "The preview project is no longer registered.", nil
		}
		p.RepoID = preview.RepoID
		p.PreviewID = preview.ID
		p.PreviewHash = preview.ContentHash
		p.Question = preview.Title + "\n" + preview.Body
	}
	return "", nil
}

func (s *Service) naturalProposal(p naturalPayload, code string) string {
	args, err := json.Marshal(p.Invocation.Arguments)
	if err != nil {
		return ""
	}
	scope := ""
	switch p.Invocation.Command {
	case "draft":
		scope = "Create or replace a feature preview only. Issue creation needs a separate confirmation."
	case "diagnose":
		scope = "Queue diagnosis work. Any repair remains subject to review on the issue."
	case "answer":
		scope = "Record this answer to the current research question: " + p.Question + "\nRevision: " + p.Revision
	case "confirm":
		scope = "Create an issue from this exact saved feature preview: " + p.Question
	}
	return fmt.Sprintf("Proposed action: %s\nProject: %s\n%s\nExact protected arguments: %s\nConfirmation code: %s\nReply directly to this message with yes, or send confirm %s. Send cancel to cancel. Expires in 15 minutes.", p.Invocation.Command, p.Project, scope, args, code, code)
}
func (s *Service) recheckNatural(ctx context.Context, p naturalPayload, at time.Time) (string, error) {
	_, _, err := s.ValidateInvocation(p.Invocation)
	if err != nil {
		return "This action is no longer enabled; request a fresh proposal.", nil //nolint:nilerr // A stale catalogue entry is a saved refusal, not retryable execution work.
	}
	if p.RepoID > 0 {
		resolution, e := ask.ResolveProject(ctx, s.db, p.Project, nil)
		if e != nil {
			return "", e
		}
		if resolution.Refusal != "" || resolution.Repository.ID != p.RepoID {
			return "The project changed; request a fresh proposal.", nil
		}
	}
	if p.Invocation.Command == "answer" {
		ref, refusal := s.researchTarget(ctx, Message{Arguments: p.Invocation.Arguments}, true)
		if refusal != "" {
			return refusal, nil
		}
		if s.research == nil {
			return "Research answers are unavailable.", nil
		}
		q, e := s.research.Current(ctx, ref)
		if e != nil {
			return "", e
		}
		if q.Token != p.Revision || q.Text != p.Question {
			return "The research question changed; request a fresh proposal.", nil
		}
	}
	if p.Invocation.Command == "confirm" {
		preview, e := s.db.ChatIssuePreviewByCode(ctx, s.transport, p.Channel, p.Invocation.Arguments["code"])
		if errors.Is(e, store.ErrNotFound) {
			return "The preview changed; request a fresh proposal.", nil
		}
		if e != nil {
			return "", e
		}
		if preview.ID != p.PreviewID || preview.ContentHash != p.PreviewHash || preview.Title+"\n"+preview.Body != p.Question || preview.RepoID != p.RepoID || preview.State != "previewed" || !at.Before(preview.ExpiresAt) {
			return "The preview changed; request a fresh proposal.", nil
		}
	}
	if s.Protect != nil {
		for _, field := range []string{"note", "text", "question"} {
			if value := p.Invocation.Arguments[field]; value != "" {
				protected, e := s.Protect(ctx, value)
				if e != nil {
					return "", e
				}
				if protected != value {
					return "Protection changed; request a fresh proposal.", nil
				}
			}
		}
	}
	return "", nil
}
