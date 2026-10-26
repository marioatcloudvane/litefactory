package messaging

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/instructionpackage"
	"github.com/cloudvane/litefactory/internal/messaging/presentation"
	"github.com/cloudvane/litefactory/internal/messaging/response"
	"github.com/cloudvane/litefactory/internal/meterview"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/retainedevidence"
	"github.com/cloudvane/litefactory/internal/store"
)

var errConversationBudget = errors.New("conversation budget exhausted")

type factoryConversationPrompt struct {
	RetainedEvidence []store.ConversationEvidence `json:"retained_evidence,omitempty"`
	ContextGaps      []string                     `json:"context_gaps,omitempty"`
	Instructions     string                       `json:"instructions"`
	OwnerInput       string                       `json:"owner_input,omitempty"`
	OwnerSources     []DialogueTurn               `json:"owner_sources"`
	Commands         []CommandDefinition          `json:"commands"`
	References       []ConversationReference      `json:"references"`
	Context          string                       `json:"context,omitempty"`
	RecentReplies    []conversationPriorReply     `json:"recent_factory_replies,omitempty"`
	Evidence         *conversationReadResult      `json:"evidence,omitempty"`
	// EarlierEvidence keeps this turn's previous lookup results, so an answer
	// that needs several lookups can use all of them.
	EarlierEvidence []conversationReadResult `json:"earlier_evidence,omitempty"`
	// Focus is the project and issue this conversation discussed last.
	Focus      *conversationFocus `json:"focus,omitempty"`
	Correction string             `json:"correction,omitempty"`
	// CorrectionReason is the factory's plain reason the previous output was
	// rejected, so a bounded correction can fix the actual defect.
	CorrectionReason string `json:"correction_reason,omitempty"`
	SavedOutcome     string `json:"saved_outcome,omitempty"`
	// ConsultNote says why a consult is not available now.
	ConsultNote string `json:"consult_note,omitempty"`
	// OpenProposal is the proposal awaiting the owner's yes, if any.
	OpenProposal *conversationOpenProposal `json:"open_proposal,omitempty"`
	// AuthorSession is the standards author session this conversation works on.
	AuthorSession *conversationAuthorSession `json:"author_session,omitempty"`
}

// conversationOpenProposal is what the agent needs to judge a confirmation:
// the reference to cite and what the owner was asked.
type conversationOpenProposal struct {
	Reference string    `json:"reference"`
	Text      string    `json:"text"`
	ExpiresAt time.Time `json:"expires_at"`
}

// addEvidence makes result the current evidence and keeps the previous one.
func (p *factoryConversationPrompt) addEvidence(result conversationReadResult) {
	if p.Evidence != nil {
		p.EarlierEvidence = append(p.EarlierEvidence, *p.Evidence)
	}
	p.Evidence = &result
}

type conversationReadResult struct {
	FactID         string            `json:"fact_id"`
	ResolvedRepoID int64             `json:"resolved_repo_id,omitempty"`
	Status         string            `json:"status"`
	Target         string            `json:"target"`
	Command        string            `json:"command"`
	Arguments      map[string]string `json:"arguments"`
	Source         string            `json:"source"`
	CapturedAt     time.Time         `json:"captured_at"`
	Result         string            `json:"result"`
	Complete       bool              `json:"complete"`
	Clarification  string            `json:"clarification,omitempty"`
	Gaps           []string          `json:"gaps"`
}

type factoryConversationRun struct {
	inboxResume    *conversationReadResult
	instructions   *instructionpackage.ConversationCapture
	s              *Service
	c              store.Conversation
	b              ConversationBinding
	t              store.ConversationTurn
	at             time.Time
	calls, ordinal int
	savedOutcomes  string
	safe           bool
	savedPrompt    string
	// focus is the newest project and issue this conversation resolved.
	focus *conversationFocus
	// consults counts this turn's reasoner consults; at most one runs.
	consults int
	// refSuffix keeps the references of reasoner lookups distinct.
	refSuffix string
	// authorRef cites the conversation's open author session.
	authorRef *ConversationReference
	// listed holds the references the last lookup saved for its listed tasks.
	listed []ConversationReference
}

// maxCalls is the turn's conversation invocation budget. A consult adds one
// invocation, so the model can still answer from the consult's evidence.
func (r *factoryConversationRun) maxCalls() int {
	if r.consults > 0 {
		return 5
	}
	return 4
}

// invoke journals every input before crossing the prompt-only native boundary.
// Each continuation resumes the exact identity durably reported by the prior call.
func (r *factoryConversationRun) invoke(ctx context.Context, prompt string) (_ []byte, err error) {
	phase, preStart := "preparation", true
	var restored factoryConversationPrompt
	if err := json.Unmarshal([]byte(prompt), &restored); err != nil {
		return nil, err
	}
	current, readErr := r.s.db.ReadConversationByID(ctx, r.c.ID)
	if readErr != nil {
		return nil, readErr
	}
	if current.OwnerID != r.s.owner || current.ChannelRef != r.c.ChannelRef || current.Transport != r.c.Transport || !r.s.channels[current.ChannelRef] {
		return nil, errors.New("conversation access changed")
	}
	if err := r.authorizeRestoredPrompt(ctx, &restored); err != nil {
		return nil, err
	}
	defer func() { err = agent.ConversationError(err, phase, preStart) }()
	instructions, restoreErr := r.s.restoreConversationInstructions(ctx, r.c.ID, r.at)
	if restoreErr != nil {
		return nil, restoreErr
	}
	r.instructions = instructions
	if len(prompt) > 256<<10 || len(instructions.Native()) > 256<<10 {
		return nil, errors.New("conversation prompt byte budget exceeded")
	}
	if err := r.s.validateConversationPrompt(ctx, r.c.ID, prompt, instructions, true, r.at); err != nil {
		return nil, err
	}
	if r.ordinal > 14 {
		return nil, fmt.Errorf("%w: exchange slot budget exhausted", errConversationBudget)
	}
	if r.calls >= r.maxCalls() {
		return nil, fmt.Errorf("%w: invocation budget exhausted", errConversationBudget)
	}
	runtime := r.s.conversations.Load()
	if err := conversationAccess(ctx, runtime, r.c.ChannelRef, r.at); err != nil {
		return nil, err
	}
	if err := conversationCapacity(runtime.root); err != nil {
		return nil, err
	}
	provider := runtime.providers[r.c.Provider]
	if provider == nil {
		return nil, agent.ErrConversationIsolation
	}
	protected, err := r.s.Protect(ctx, prompt)
	if err != nil {
		return nil, err
	}
	if r.savedPrompt != "" && protected != r.savedPrompt {
		return nil, errors.New("saved continuation changed during redaction; replay refused")
	}
	prompt = protected
	if err := r.s.validateConversationPrompt(ctx, r.c.ID, prompt, instructions, true, r.at); err != nil {
		return nil, err
	}
	if len(prompt) > 256<<10 {
		return nil, errors.New("conversation prompt byte budget exceeded")
	}
	r.calls++
	r.ordinal++
	r.safe = false
	exchange := store.ConversationExchange{TurnID: r.t.ID, Ordinal: r.ordinal, Kind: "provider", Request: prompt, ProviderAttempt: fmt.Sprintf("%s:%d", r.t.ID, r.calls), CapturedAt: r.at}
	if err := r.s.db.SaveConversationExchange(ctx, r.t, exchange); err != nil {
		return nil, err
	}
	attempt, err := r.s.db.StartPresentationMeterAttempt(ctx, 0, fmt.Sprintf("conversation-%s-%d", r.t.ID, r.calls), "generation", r.c.Provider)
	if err != nil {
		return nil, err
	}
	defer func() {
		outcome := "success"
		if err != nil {
			outcome = "failure"
		}
		if ctx.Err() != nil {
			outcome = "cancelled"
		}
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		meterErr := r.s.db.FinishMeterAttempt(cleanup, attempt.ID, outcome, "unknown", "", "", []string{"conversation_provider_usage_not_observed"})
		err = errors.Join(err, meterErr)
	}()
	ref, err := naming.ConversationSessionRef(r.c.ID)
	if err != nil {
		return nil, err
	}
	recorded := false
	phase, preStart = "provider", false
	result, err := provider.RunConversation(ctx, agent.ConversationRequest{ConversationID: r.c.ID, TurnID: r.t.ID, Model: r.b.Model, StateDirectory: r.c.StateDir, Prompt: prompt, NativeInstructions: instructions.Native(), Session: agent.SessionRef{Ref: ref, Resume: r.c.SessionHandle}, BeforeLaunch: func(cb context.Context) error {
		if err := r.authorizeRestoredPrompt(cb, &restored); err != nil {
			return err
		}
		if err := conversationAccess(cb, runtime, r.c.ChannelRef, r.at); err != nil {
			return err
		}
		return r.s.db.RenewConversationTurn(cb, r.t, r.at, 10*time.Minute)
	}, OnSession: func(cb context.Context, session agent.SessionRef) error {
		if session.Ref != ref || !agent.ConversationIdentity(session.Resume) || (r.c.SessionHandle != "" && session.Resume != r.c.SessionHandle) {
			return errors.New("invalid exact conversation identity")
		}
		if err := r.s.db.SaveConversationSession(cb, r.t, session.Resume); err != nil {
			return err
		}
		r.c.SessionHandle = session.Resume
		recorded = true
		return nil
	}})
	if err != nil {
		if conversationPreStartRetry(err) {
			exchange.Complete = true
			exchange.Gaps = "provider_not_started"
			if saveErr := r.s.db.SaveConversationExchange(ctx, r.t, exchange); saveErr != nil {
				return nil, errors.Join(err, saveErr)
			}
			r.safe = true
		}
		return nil, err
	}
	if !recorded || result.Session.Ref != ref || result.Session.Resume != r.c.SessionHandle {
		return nil, agent.ErrConversationSessionMissing
	}
	return result.Answer, nil
}

func (s *Service) runFactoryConversation(ctx context.Context, c store.Conversation, b ConversationBinding, t store.ConversationTurn, at time.Time) error {
	work, cancel := context.WithTimeout(ctx, response.Budget)
	cancels := []context.CancelFunc{cancel}
	defer func() {
		for _, cancel := range cancels {
			cancel()
		}
	}()
	instructions, err := s.restoreConversationInstructions(work, c.ID, at)
	if err != nil {
		return err
	}
	run := &factoryConversationRun{instructions: instructions, s: s, c: c, b: b, t: t, at: at, safe: true}
	exchanges, err := s.db.ListConversationExchanges(work, t.ID)
	if err != nil {
		return err
	}
	var savedOutcomes string
	if len(exchanges) == 0 {
		savedOutcomes, err = run.consumeOutcomes(work)
		if err != nil {
			return err
		}
	} else {
		for _, x := range exchanges {
			if x.Kind == "saved_outcome" {
				savedOutcomes += x.Result + "\n"
			}
		}
	}
	run.savedOutcomes = savedOutcomes
	// Confirmation and cancellation never enter model interpretation.
	if len(exchanges) == 0 {
		if handled, err := run.confirm(work); handled || err != nil {
			return err
		}
	}
	// A live proposal stays open: the agent judges whether this message
	// confirms it, cancels it or changes the request (a new proposal).
	open, err := run.openProposal(work)
	if err != nil {
		return err
	}
	proposal := ""
	if open != nil {
		proposal = open.Reference
	}
	sources, err := s.db.ListConversationSources(work, c.ID, at, 8)
	if err != nil {
		return err
	}
	if run.focus, err = run.loadFocus(work); err != nil {
		return err
	}
	sources, err = run.scopeOwnerSources(work, sources)
	if err != nil {
		return err
	}
	turns := make([]DialogueTurn, 0, len(sources))
	sourceBytes := 0
	for _, source := range sources {
		if sourceBytes+len(source.Source) > 32768 {
			continue
		}
		protected, e := s.Protect(work, source.Source)
		if e != nil {
			return e
		}
		if protected != source.Source {
			continue
		}
		turns = append(turns, DialogueTurn{SourceID: source.SourceID, Text: source.Source})
		sourceBytes += len(source.Source)
	}
	if len(turns) == 0 || len(t.Source) > 32768 || turns[0].SourceID != t.SourceID {
		return s.conversationHandled(ctx, t, "Please send a shorter request with the exact action and target. Nothing was authorized.", at)
	}
	slices.Reverse(turns)
	author, err := run.loadAuthor(work)
	if err != nil {
		return err
	}
	refs, err := run.references(work)
	if err != nil {
		return err
	}
	var contextData struct{ PreparedContext, RecoveryNotice string }
	if err = json.Unmarshal([]byte(t.Prompt), &contextData); err != nil {
		contextData.PreparedContext = "Retained prepared context is unavailable."
	}
	// A legacy prepared blob carries no repository grants. On restart use the
	// current factory hook, or omit the blob rather than disclose a stale grant.
	savedExchanges, err := s.db.ListConversationExchanges(work, t.ID)
	if err != nil {
		return err
	}
	if len(savedExchanges) > 0 && contextData.PreparedContext != "" {
		runtime := s.conversations.Load()
		if runtime.prepared != nil {
			contextData.PreparedContext, err = runtime.prepared(work, c.ChannelRef)
			if err != nil {
				return err
			}
		} else {
			contextData.PreparedContext = "Retained prepared context could not be reauthorized and was omitted; use a current factory lookup."
		}
	}
	contextBytes, err := json.Marshal(contextData)
	if err != nil {
		return err
	}
	prepared, err := s.Protect(work, string(contextBytes))
	if err != nil {
		return err
	}
	recentReplies, err := run.recentFactoryReplies(work, sources, turns)
	if err != nil {
		return err
	}
	if run.focus, err = run.loadFocus(work); err != nil {
		return err
	}
	repository, issue, err := run.recoveryTarget(work)
	if err != nil {
		return err
	}
	if err = run.retainView(work, 1, "owner", t.SourceID, t.ID, repository, issue, t.Source, t.Source, t.CreatedAt, retainedevidence.Unknown); err != nil {
		return err
	}
	if err = run.retainPreviousReplies(work, sources); err != nil {
		return err
	}
	prompt := factoryConversationPrompt{Focus: run.focus, RecentReplies: recentReplies, Instructions: instructions.Factory(), OwnerInput: t.Source, OwnerSources: turns, Commands: s.ConversationCommands(), References: refs, Context: prepared, SavedOutcome: savedOutcomes, OpenProposal: open, AuthorSession: author}
	corrections, lookups, pendingLookup, err := run.resumeBoundary(work, &prompt)
	if err != nil {
		return run.failed(ctx, err)
	}
	prompt.OwnerSources = turns
	prompt.OwnerInput = t.Source
	prompt.Context = prepared
	if repository != "" {
		prompt.Focus = &conversationFocus{Project: repository, Issue: issue}
	}
	if err = run.projectRetainedEvidence(work, &prompt); err != nil {
		return err
	}
	if err = run.retainCoverage(work, repository, issue, prompt.ContextGaps); err != nil {
		return err
	}
	if run.inboxResume != nil {
		result := run.inboxResume
		return s.saveConversationPresentation(ctx, t, "completed", []presentation.AnswerBlock{{Text: result.Result, FactRefs: []string{result.FactID}}}, "", at)
	}
	for run.calls < run.maxCalls() {
		var envelope ConversationEnvelope
		var inv Invocation
		var requestText string
		if pendingLookup != nil {
			envelope, inv, requestText = pendingLookup.Envelope, pendingLookup.Invocation, pendingLookup.Request
			pendingLookup = nil
		} else {
			encoded, err := json.Marshal(prompt)
			if err != nil {
				return err
			}
			var raw []byte
			requestPrompt := string(encoded)
			if run.savedPrompt != "" {
				requestPrompt = run.savedPrompt
			}
			raw, err = run.invoke(work, requestPrompt)
			run.savedPrompt = ""
			if err != nil {
				return run.failed(ctx, err)
			}
			envelope, inv, err = DecodeConversationEnvelope(raw, ConversationDecodeRequest{Turns: turns, Commands: s.ConversationCommands(), References: refs, OwnerID: s.owner, ChannelID: c.ChannelRef, Now: at, Proposal: proposal})
			if err == nil && corrections == 0 {
				// A standards change missing terms the agent can supply goes
				// back to it once, rather than to the owner as a question.
				if gap := standardsProposeGap(envelope, inv, t.SourceID); gap != "" {
					err = &ConversationEnvelopeError{Reason: gap}
				}
			}
			if err != nil {
				if corrections < 2 && run.calls < run.maxCalls() && run.ordinal <= 13 {
					corrections++
					prompt.Correction = instructions.Correction()
					prompt.CorrectionReason = conversationEnvelopeReason(err)
					// The provider completed, but its invalid envelope cannot serve as
					// the retry boundary. Retain the factory's next prompt instead.
					encoded, e := json.Marshal(prompt)
					if e != nil {
						return e
					}
					protected, e := s.Protect(work, string(encoded))
					if e != nil {
						return e
					}
					if e = s.validateConversationPrompt(work, c.ID, protected, instructions, true, at); e != nil {
						return run.failed(ctx, e)
					}
					run.ordinal++
					if e = s.db.SaveConversationExchange(work, t, store.ConversationExchange{TurnID: t.ID, Ordinal: run.ordinal, Kind: "correction", Request: protected, CapturedAt: at, Complete: true}); e != nil {
						return e
					}
					run.safe = true
					continue
				}
				s.logConversationFailure(ctx, c, t, agent.ConversationError(err, "validation", false))
				limit := "The response correction limit was reached. "
				if run.calls >= run.maxCalls() {
					limit = "The conversation invocation budget was reached. "
				} else if run.ordinal > 13 {
					limit = "The conversation exchange slot budget was reached. "
				}
				return run.limit(ctx, limit+conversationCauseReply(err, at))
			}
			prompt.Correction, prompt.CorrectionReason = "", ""
			run.ordinal++
			request, _ := json.Marshal(struct {
				Envelope   ConversationEnvelope `json:"envelope"`
				Invocation Invocation           `json:"normalized_invocation"`
			}{envelope, inv})
			requestText, err = s.Protect(work, string(request))
			if err != nil {
				return err
			}
			if err = s.db.SaveConversationExchange(work, t, store.ConversationExchange{TurnID: t.ID, Ordinal: run.ordinal, Kind: envelope.Kind, Request: requestText, CapturedAt: at, Complete: true}); err != nil {
				return err
			}
		}
		run.safe = true
		switch envelope.Kind {
		case "answer", "clarify":
			// A saved outcome reaches the owner through this answer; the
			// model was given it as saved_outcome.
			blocks := envelope.Blocks
			if len(blocks) == 0 {
				blocks = []presentation.AnswerBlock{{Text: envelope.Text}}
			}
			return s.saveConversationPresentation(ctx, t, "completed", blocks, "", at, envelope.Kind)
		case "propose":
			return run.propose(work, inv, envelope, turns)
		case "confirm":
			return run.confirmJudged(work, envelope)
		case "cancel":
			return run.cancelJudged(work, envelope)
		case "lookup":
			if lookups >= 3 || run.calls >= run.maxCalls() {
				break
			}
			if run.ordinal > 13 {
				return run.failed(ctx, fmt.Errorf("%w: exchange slot budget exhausted before lookup", errConversationBudget))
			}
			lookups++
			result, ref, err := run.lookup(work, inv)
			if err != nil {
				return run.failed(ctx, err)
			}
			run.ordinal++
			result.FactID = fmt.Sprintf("%s-fact-%d", t.ID, run.ordinal)
			result.Status = "complete"
			if !result.Complete {
				result.Status = "incomplete"
			}
			result.Target = conversationEvidenceTarget(result.Arguments)
			encoded, _ := json.Marshal(result)
			resultText, err := s.Protect(work, string(encoded))
			if err != nil {
				return err
			}
			if err = s.db.SaveConversationExchange(work, t, store.ConversationExchange{TurnID: t.ID, Ordinal: run.ordinal, Kind: "evidence", Request: requestText, Result: resultText, CapturedAt: at, Complete: result.Complete, Gaps: strings.Join(result.Gaps, "\n")}); err != nil {
				return err
			}
			if err = run.retainLookup(work, result, 32+run.ordinal); err != nil {
				return err
			}
			if ref != nil {
				refs = append(refs, *ref)
				if standardsCommand(inv.Command) {
					refs = expandStandardsReferences(refs)
				}
			}
			refs = append(refs, run.takeListed()...)
			if len(refs) > 32 {
				refs = refs[len(refs)-32:]
			}
			if result.Clarification != "" {
				return s.conversationOutcome(ctx, t, "completed", result.Clarification, at)
			}
			if inv.Command == "inbox" {
				return s.saveConversationPresentation(ctx, t, "completed", []presentation.AnswerBlock{{Text: result.Result, FactRefs: []string{result.FactID}}}, "", at)
			}
			prompt.References = refs
			prompt.Focus = run.focus
			prompt.addEvidence(result)
		case "consult":
			if run.ordinal > 13 {
				return run.failed(ctx, fmt.Errorf("%w: exchange slot budget exhausted before consult", errConversationBudget))
			}
			if run.consults > 0 {
				prompt.ConsultNote = conversationReasonerNote
				break
			}
			run.consults++
			// The consult has its own wall-clock limit; the answer afterwards
			// keeps the time the turn had left, and at least half a turn.
			deadline, _ := work.Deadline()
			remaining := time.Until(deadline)
			result, consulted := run.consult(ctx, envelope, &prompt, turns, refs)
			if err := ctx.Err(); err != nil {
				return run.failed(ctx, err)
			}
			cancels[len(cancels)-1]()
			next, cancelNext := context.WithTimeout(ctx, max(remaining, response.Budget/2))
			work, cancels = next, append(cancels, cancelNext)
			run.ordinal++
			result.FactID = fmt.Sprintf("%s-fact-%d", t.ID, run.ordinal)
			result.Status = "complete"
			if !result.Complete {
				result.Status = "incomplete"
			}
			result.Target = conversationEvidenceTarget(result.Arguments)
			encoded, _ := json.Marshal(result)
			resultText, err := s.Protect(work, string(encoded))
			if err != nil {
				return err
			}
			if err = s.db.SaveConversationExchange(work, t, store.ConversationExchange{TurnID: t.ID, Ordinal: run.ordinal, Kind: "evidence", Request: requestText, Result: resultText, CapturedAt: at, Complete: result.Complete, Gaps: strings.Join(result.Gaps, "\n")}); err != nil {
				return err
			}
			if err = run.retainLookup(work, result, 32+run.ordinal); err != nil {
				return err
			}
			refs = consulted
			prompt.References = refs
			prompt.Focus = run.focus
			prompt.ConsultNote = ""
			prompt.addEvidence(result)
		}
	}
	return run.limit(ctx, "The conversation invocation or lookup budget was reached before a completed answer. Current evidence is retained, but the answer is unavailable. Please narrow the question.")
}

func (r *factoryConversationRun) limit(ctx context.Context, notice string) error {
	if r.savedOutcomes != "" {
		notice = "Saved action outcome:\n" + r.savedOutcomes + "\n\n" + notice
	}
	return r.s.conversationOutcome(ctx, r.t, "completed", notice, r.at)
}

func (r *factoryConversationRun) failed(ctx context.Context, cause error) error {
	r.s.logConversationFailure(ctx, r.c, r.t, cause)
	if errors.Is(cause, errConversationInstructions) && r.savedOutcomes == "" {
		return cause
	}
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if errors.Is(cause, errConversationBudget) {
		return r.limit(cleanup, cause.Error()+". No further work was launched; retained outcomes and evidence may be incomplete.")
	}
	if r.savedOutcomes != "" {
		if err := r.s.conversationOutcome(cleanup, r.t, "interrupted", r.savedOutcomes+"\n\nThe follow-up answer could not be completed. No action was repeated; remaining context may be incomplete.", r.at); err != nil {
			return err
		}
		if r.c.SessionHandle == "" && !conversationPreStartRetry(cause) {
			return r.s.db.SetConversationHealth(cleanup, r.c.ID, "turn unfinished with no saved native identity; no replacement session created", r.at)
		}
		return nil
	}
	if accessRetry(cause) > 0 && (r.safe || r.calls == 0) {
		return r.s.db.DeferConversationTurn(cleanup, r.t)
	}
	if conversationPreStartRetry(cause) {
		if reply := conversationSpecificCause(cause, r.at); reply != "" {
			return r.s.conversationOutcome(cleanup, r.t, "interrupted", reply, r.at)
		}
		return r.s.conversationPreStartOutcome(cleanup, r.t, r.at)
	}
	if errors.Is(cause, agent.ErrConversationBusy) || errors.Is(cause, agent.ErrConversationSessionMissing) || errors.Is(cause, agent.ErrConversationIsolation) {
		return r.s.conversationUnavailable(cleanup, r.c, r.t, "native continuity or isolation unavailable; no replacement session created", r.at)
	}
	var access *ConversationAccessError
	if errors.As(cause, &access) && access.Permanent {
		return r.s.conversationUnavailable(cleanup, r.c, r.t, "conversation authorization unavailable", r.at)
	}
	if r.c.SessionHandle == "" {
		return r.s.conversationUnavailable(cleanup, r.c, r.t, "turn unfinished with no saved native identity; no replacement session created", r.at)
	}
	// Even a correction or continuation can have uncertain provider completion.
	if reply := conversationCauseReply(cause, r.at); reply != "" {
		return r.s.conversationOutcome(cleanup, r.t, "interrupted", reply+" The provider input will not be replayed. Ask here for the status of your previous request; your next message continues the retained conversation.", r.at)
	}
	return r.s.conversationOutcome(cleanup, r.t, "interrupted", "The conversation turn was interrupted or unavailable before a completed answer was saved. Its provider input will not be replayed. Ask here for the status of your previous request; your next message resumes the retained session.", r.at)
}

func (r *factoryConversationRun) references(ctx context.Context) ([]ConversationReference, error) {
	// Listed tasks are bounded, so older targets keep their place among them.
	saved, err := r.s.db.ListConversationReferences(ctx, r.c.ID, r.at, 64)
	if err != nil {
		return nil, err
	}
	var refs []ConversationReference
	listed := 0
	for _, v := range saved {
		if len(refs) >= 32 {
			break
		}
		if v.Kind == "listed" {
			if listed >= conversationListedLimit {
				continue
			}
			listed++
		}
		var args map[string]string
		if json.Unmarshal([]byte(v.Content), &args) != nil {
			continue
		}
		current, e := r.referenceCurrent(ctx, v, args)
		if e != nil {
			return nil, e
		}
		if !current {
			continue
		}
		// Re-resolve the registered project; a saved reference is never authority.
		repoID, _ := strconv.ParseInt(v.Repository, 10, 64)
		if repoID > 0 {
			repo, err := r.s.db.GetRepo(ctx, repoID)
			if err != nil || repo.Slug() != args["project"] {
				continue
			}
		}
		refs = append(refs, ConversationReference{ID: v.ID, OwnerID: r.s.owner, ChannelID: r.c.ChannelRef, OriginTurn: v.TurnID, RepoID: repoID, Revision: v.Revision, ExpiresAt: v.ExpiresAt, Arguments: args})
	}
	if r.authorRef != nil {
		refs = append([]ConversationReference{*r.authorRef}, refs...)
	}
	return expandStandardsReferences(refs), nil
}

func boundedConversationText(text string, limit int) (string, bool) {
	if len(text) <= limit {
		return text, false
	}
	text = text[:limit]
	for !utf8.ValidString(text) {
		text = text[:len(text)-1]
	}
	return text, true
}

func (r *factoryConversationRun) lookup(ctx context.Context, inv Invocation) (conversationReadResult, *ConversationReference, error) {
	if standardsCommand(inv.Command) {
		return r.lookupStandards(ctx, inv)
	}
	s := r.s
	result := conversationReadResult{Command: inv.Command, Arguments: inv.Arguments, Source: "factory:" + inv.Command, CapturedAt: r.at, Complete: true, Gaps: []string{}}
	if err := conversationAccess(ctx, s.conversations.Load(), r.c.ChannelRef, r.at); err != nil {
		return result, nil, err
	}
	validated, effect, err := s.ValidateInvocation(inv)
	if err != nil || effect != ReadEffect {
		return result, nil, ErrConversationEnvelope
	}
	if inv.Command == "inbox" {
		capture := &inboxCapture{Organize: r.organizerCall}
		text, e := s.readOwnerInbox(ctx, Message{Channel: r.c.ChannelRef, Arguments: validated.Arguments}, r.at, capture)
		result.Result = text
		result.Arguments = make(map[string]string)
		for k, v := range validated.Arguments {
			result.Arguments[k] = v
		}
		if capture.Snapshot != "" {
			result.Arguments["inbox_snapshot"] = capture.Snapshot
			sources, _ := json.Marshal(capture.Sources)
			result.Arguments["inbox_sources"] = string(sources)
		}
		if e != nil {
			return result, nil, e
		}
		if e = r.saveListed(ctx, capture.Issues); e != nil {
			return result, nil, e
		}
		navigation := capture.Navigation
		if navigation == "" {
			return result, nil, nil
		}
		args := map[string]string{"reference": navigation}
		raw, _ := json.Marshal(args)
		saved := store.ConversationReference{ID: fmt.Sprintf("%s-inbox-%d", r.t.ID, r.ordinal), TurnID: r.t.ID, Kind: "inbox", Content: string(raw), ExpiresAt: r.at.Add(24 * time.Hour)}
		if e = s.db.SaveConversationReference(ctx, r.t, saved); e != nil {
			return result, nil, e
		}
		return result, &ConversationReference{ID: saved.ID, OwnerID: s.owner, ChannelID: r.c.ChannelRef, OriginTurn: r.t.ID, ExpiresAt: saved.ExpiresAt, Arguments: args}, nil
	}
	p := naturalPayload{Invocation: validated, Channel: r.c.ChannelRef}
	if r.focus != nil {
		p.focus = r.focus.Project
	}
	clarification, err := s.resolveNatural(ctx, &p, r.at)
	if err != nil {
		return result, nil, err
	}
	result.Arguments = p.Invocation.Arguments
	result.ResolvedRepoID = p.RepoID
	if clarification != "" {
		result.Result = clarification
		result.Clarification = clarification
		result.Complete = false
		result.Gaps = append(result.Gaps, "Target unavailable or ambiguous; clarification required.")
		return result, nil, nil
	}
	var text string
	switch inv.Command {
	case "ask":
		if s.ask == nil {
			result.Gaps = append(result.Gaps, "Project facts unavailable.")
		} else {
			prepared, e := s.ask.Prepare(ctx, p.Invocation.Arguments["question"], p.Project, r.c.ChannelRef, r.at)
			switch {
			case e != nil:
				result.Gaps = append(result.Gaps, "Project facts could not be read."+githubBudgetGap(e, r.at))
			case prepared.Refusal != "":
				text = prepared.Refusal
				result.Clarification = prepared.Refusal
				result.Gaps = append(result.Gaps, "Project facts unavailable or target unclear.")
			default:
				var gaps []string
				text, gaps, e = conversationAskContext(prepared)
				if e != nil {
					return result, nil, e
				}
				result.Gaps = append(result.Gaps, gaps...)
			}
		}
	case "meter":
		request, _, requestErr := meterRequest(p.Invocation.Arguments)
		if requestErr != nil {
			result.Gaps = append(result.Gaps, "Invalid cost request.")
			break
		}
		bounded, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		if len(request.By) > 0 {
			breakdown, readErr := meterview.ReadBreakdown(bounded, s.db, request, request.By)
			if readErr != nil {
				result.Gaps = append(result.Gaps, "Current cost breakdown unavailable.")
				break
			}
			lines := breakdown.Lines()
			if request.Task > 0 && breakdown.Records > 0 {
				lines = append([]string{"Scope: the task including its stories."}, lines...)
			}
			text = strings.Join(lines, "\n")
			if _, known := breakdown.TotalAmount(); !known && breakdown.Records > 0 {
				result.Gaps = append(result.Gaps, "No amount available.")
			}
			break
		}
		view, readErr := meterview.Read(bounded, s.db, request)
		if readErr != nil {
			result.Gaps = append(result.Gaps, "Current cost unavailable.")
			break
		}
		if len(view.Groups) == 0 {
			// No recorded usage is a fact about the filter, not a gap.
			text = "No usage is recorded for this filter."
			break
		}
		amount := view.Summary.EstimatedUSD
		if amount == nil {
			amount = view.Summary.PricedSubtotalUSD
		}
		if amount == nil {
			text = "Cost unavailable."
			result.Gaps = append(result.Gaps, "No amount available.")
		} else {
			text = "Feature total including parent and stories (USD): " + *amount
		}

	case "question":
		ref, refusal := s.researchTarget(ctx, Message{Arguments: p.Invocation.Arguments}, true)
		if refusal != "" || s.research == nil {
			text = refusal
			result.Gaps = append(result.Gaps, "Current research question unavailable.")
		} else {
			q, e := s.research.Current(ctx, ref)
			if e != nil {
				result.Gaps = append(result.Gaps, "Current research question could not be read.")
			} else {
				text = questionText(q)
				if q.Text == "" {
					result.Gaps = append(result.Gaps, "No current question revision available.")
				} else {
					p.Revision = q.Token
					p.Invocation.Arguments["revision"] = q.Token
				}
			}
		}
	default:
		s.mu.RLock()
		handler := s.commands[inv.Command]
		s.mu.RUnlock()
		if handler == nil {
			result.Gaps = append(result.Gaps, "Enabled reader unavailable.")
		} else {
			read, assessment := presentation.WithAssessment(ctx)
			read, listed := withListedIssues(read)
			text, err = handler(read, Message{Transport: s.transport, Channel: r.c.ChannelRef, Sender: s.owner, Arguments: p.Invocation.Arguments, Command: inv.Command}, r.at)
			if err != nil {
				text = ""
				result.Gaps = append(result.Gaps, "Factory lookup failed; no result available."+githubBudgetGap(err, r.at))
			} else if assessment.Status != presentation.Success {
				result.Gaps = append(result.Gaps, "Lookup coverage may be incomplete.")
			}
			if err == nil {
				if err = r.saveListed(ctx, *listed); err != nil {
					return result, nil, err
				}
			}
			if (inv.Command == "health" || inv.Command == "status") && strings.Contains(text, "Heartbeat age: unknown.") {
				result.Gaps = append(result.Gaps, "Current daemon heartbeat is unknown; default status values are not established observations.")
			}
			if strings.Contains(text, "unavailable") || strings.Contains(text, "left out") || strings.Contains(text, "could not be read") {
				result.Gaps = append(result.Gaps, "Some factory source data was unavailable or omitted.")
			}
		}
	}
	text, err = s.Protect(ctx, text)
	if err != nil {
		return result, nil, err
	}
	result.Result = text
	if bounded, cut := boundedConversationText(text, 16384); cut {
		result.Result = bounded
		result.Gaps = append(result.Gaps, "Result exceeded the conversation evidence limit; additional data omitted.")
	}
	result.Complete = len(result.Gaps) == 0
	if p.RepoID == 0 {
		return result, nil, nil
	}
	args := map[string]string{"project": p.Project}
	for _, name := range []string{"issue", "task", "revision", "code"} {
		if value := p.Invocation.Arguments[name]; value != "" {
			args[name] = value
		}
	}
	raw, _ := json.Marshal(args)
	saved := store.ConversationReference{ID: fmt.Sprintf("%s-target-%d%s", r.t.ID, r.ordinal, r.refSuffix), TurnID: r.t.ID, Kind: "target", Repository: strconv.FormatInt(p.RepoID, 10), Revision: p.Revision, Content: string(raw), ExpiresAt: r.at.Add(24 * time.Hour)}
	if err = s.db.SaveConversationReference(ctx, r.t, saved); err != nil {
		return result, nil, err
	}
	r.focus = &conversationFocus{Project: p.Project, Issue: args["task"]}
	if r.focus.Issue == "" {
		r.focus.Issue = args["issue"]
	}
	return result, &ConversationReference{ID: saved.ID, OwnerID: s.owner, ChannelID: r.c.ChannelRef, OriginTurn: r.t.ID, RepoID: p.RepoID, Revision: p.Revision, ExpiresAt: saved.ExpiresAt, Arguments: args}, nil
}

// A decoded lookup or saved evidence proves the previous provider input completed.
// Only these factory boundaries (or affirmative pre-launch proof) may be resumed.
type conversationPendingLookup struct {
	Envelope   ConversationEnvelope `json:"envelope"`
	Invocation Invocation           `json:"normalized_invocation"`
	Request    string               `json:"-"`
}

func (r *factoryConversationRun) resumeBoundary(ctx context.Context, prompt *factoryConversationPrompt) (int, int, *conversationPendingLookup, error) {
	if r.instructions == nil {
		capture, err := r.s.restoreConversationInstructions(ctx, r.c.ID, r.at)
		if err != nil {
			return 0, 0, nil, err
		}
		r.instructions = capture
	}
	exchanges, err := r.s.db.ListConversationExchanges(ctx, r.t.ID)
	if err != nil {
		return 0, 0, nil, err
	}
	if len(exchanges) == 0 {
		return 0, 0, nil, nil
	}
	current := *prompt
	corrections, lookups := 0, 0
	for i, x := range exchanges {
		if x.Ordinal != i+1 || x.TurnID != r.t.ID || x.Ordinal > 16 {
			return 0, 0, nil, errors.New("invalid conversation journal sequence")
		}
		if x.Kind == "correction" {
			if err := r.s.validateConversationPrompt(ctx, r.c.ID, x.Request, r.instructions, true, r.at); err != nil {
				return 0, 0, nil, err
			}
			var saved factoryConversationPrompt
			if err := json.Unmarshal([]byte(x.Request), &saved); err != nil {
				return 0, 0, nil, err
			}
			if saved.Correction == "" || saved.CorrectionReason == "" {
				return 0, 0, nil, errors.New("missing saved correction feedback")
			}
			corrections++
			if !x.Complete {
				return 0, 0, nil, errors.New("incomplete conversation correction boundary")
			}
			if corrections > 2 {
				return 0, 0, nil, fmt.Errorf("%w: correction limit exceeded in saved journal", errConversationBudget)
			}
		}
		if !validConversationTransition(exchanges, i) {
			return 0, 0, nil, errors.New("inconsistent conversation journal boundary")
		}
		r.ordinal = x.Ordinal
		if x.Kind == "provider" {
			r.calls++
			if x.ProviderAttempt != fmt.Sprintf("%s:%d", r.t.ID, r.calls) {
				return 0, 0, nil, errors.New("invalid saved provider attempt identity")
			}
			if x.Complete && x.Gaps == "provider_not_started" {
				if i != len(exchanges)-1 {
					return 0, 0, nil, errors.New("invalid pre-start journal boundary")
				}
				r.calls--
			}
			if err := r.s.validateConversationPrompt(ctx, r.c.ID, x.Request, r.instructions, true, r.at); err != nil {
				return 0, 0, nil, err
			}
			*prompt = factoryConversationPrompt{}
			if err := json.Unmarshal([]byte(x.Request), prompt); err != nil {
				return 0, 0, nil, err
			}
		}
		if x.Kind == "lookup" {
			lookups++
		}
		if x.Kind == "consult" {
			r.consults++
		}
	}
	if r.calls > r.maxCalls() || lookups > 3 || r.consults > 1 {
		return 0, 0, nil, fmt.Errorf("%w: saved journal exceeds turn limits", errConversationBudget)
	}
	last := exchanges[len(exchanges)-1]
	switch last.Kind {
	case "saved_outcome":
		if r.calls == 0 {
			return corrections, lookups, nil, nil
		}
	case "lookup", "consult":
		if r.c.SessionHandle == "" {
			return 0, 0, nil, agent.ErrConversationSessionMissing
		}
		if err := r.validateContinuationContext(ctx, prompt); err != nil {
			return 0, 0, nil, err
		}
		// A saved consult without its evidence runs again: the reasoner is
		// stateless and read-only, so a repeat cannot double an effect.
		var pending conversationPendingLookup
		if !last.Complete {
			return 0, 0, nil, errors.New("incomplete saved lookup")
		}
		if err := json.Unmarshal([]byte(last.Request), &pending); err != nil {
			return 0, 0, nil, err
		}
		if pending.Envelope.Kind != last.Kind {
			return 0, 0, nil, errors.New("invalid saved lookup")
		}
		encoded, err := json.Marshal(pending.Envelope)
		if err != nil {
			return 0, 0, nil, err
		}
		if pending.Envelope.Kind == "lookup" && len(pending.Envelope.Arguments) == 0 {
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(encoded, &fields); err != nil {
				return 0, 0, nil, err
			}
			fields["arguments"] = json.RawMessage("[]")
			encoded, err = json.Marshal(fields)
			if err != nil {
				return 0, 0, nil, err
			}
		}
		_, inv, err := DecodeConversationEnvelope(encoded, ConversationDecodeRequest{Turns: current.OwnerSources, Commands: r.s.ConversationCommands(), References: current.References, OwnerID: r.s.owner, ChannelID: r.c.ChannelRef, Now: r.at})
		if err != nil || !reflect.DeepEqual(inv, pending.Invocation) {
			return 0, 0, nil, errors.New("saved lookup no longer matches current sources or target")
		}
		pending.Request = last.Request
		prompt.References = current.References
		prompt.OwnerSources = current.OwnerSources
		prompt.Correction, prompt.CorrectionReason = "", ""
		if last.Kind == "consult" {
			r.consults--
			return corrections, lookups, &pending, nil
		}
		return corrections, lookups - 1, &pending, nil
	case "evidence":
		if err := r.validateContinuationContext(ctx, prompt); err != nil {
			return 0, 0, nil, err
		}
		result, err := decodeConversationEvidence(last)
		if err != nil {
			return 0, 0, nil, err
		}
		if result.Command == "inbox" {
			if id := result.Arguments["inbox_snapshot"]; id != "" {
				if err = r.s.authorizeInboxResult(ctx, r.c.ChannelRef, result, r.at); err != nil {
					return 0, 0, nil, err
				}
			}
			r.inboxResume = &result
			return corrections, lookups, nil, nil
		}
		if err := r.retainLookup(ctx, result, 32+last.Ordinal); err != nil {
			return 0, 0, nil, err
		}
		prompt.addEvidence(result)
		prompt.Correction, prompt.CorrectionReason = "", ""
		refs, err := r.references(ctx)
		if err != nil {
			return 0, 0, nil, err
		}
		prompt.References = refs
		return corrections, lookups, nil, nil
	case "correction":
		if !last.Complete {
			return 0, 0, nil, errors.New("incomplete correction boundary")
		}
		*prompt = factoryConversationPrompt{}
		if err := json.Unmarshal([]byte(last.Request), prompt); err != nil {
			return 0, 0, nil, err
		}
		if err := r.validateSavedContinuation(ctx, last.Request, prompt); err != nil {
			return 0, 0, nil, err
		}
		return corrections, lookups, nil, nil
	case "provider":
		if last.Complete && last.Gaps == "provider_not_started" {
			if err := r.validateSavedContinuation(ctx, last.Request, prompt); err != nil {
				return 0, 0, nil, err
			}
			// Reuse the identical pre-launch journal slot and invocation count.
			r.ordinal--
			return corrections, lookups, nil, nil
		}
	}
	r.safe = false
	return 0, 0, nil, errors.New("provider completion uncertain; replay refused")
}
