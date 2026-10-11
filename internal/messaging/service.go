// Package messaging admits read-only commands and manages transport-neutral replies.
package messaging

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cloudvane/litefactory/internal/messaging/ask"
	"github.com/cloudvane/litefactory/internal/messaging/feature"
	"github.com/cloudvane/litefactory/internal/messaging/presentation"
	"github.com/cloudvane/litefactory/internal/store"
)

// Message holds neutral command and identity references. Free text is not routed.
type Message struct {
	Transport, ID, Channel, Sender, Command string
	Arguments                               map[string]string
}

// ArgumentType describes values without transport-specific types.
type ArgumentType string

// Supported argument kinds map to scalar text, whole numbers and yes/no values.
const (
	TextArgument    ArgumentType = "text"
	IntegerArgument ArgumentType = "integer"
	BooleanArgument ArgumentType = "boolean"
)

// Argument declares an accepted command option.
type Argument struct {
	Name        string
	Type        ArgumentType
	Required    bool
	Description string
}

// CommandDefinition is the transport-neutral command catalogue entry.
type CommandDefinition struct {
	Name, Description string
	Arguments         []Argument
}

// Target identifies a reply; Handle is opaque and credentials remain in the adapter.
type Target struct{ Transport, MessageID, Channel, Handle string }

// Delivery describes confirmation or permanent/temporary failure. Route and
// Status describe the last transport call for logging only.
type Delivery struct {
	Confirmed, Permanent bool
	AttachmentRejected   bool
	RetryAfter           time.Duration
	Route                string
	Status               int
}

// Transport supplies delivery and its outcome without service-specific types.
type Transport interface {
	Deliver(context.Context, Target, string) Delivery
}

// Status contains the current read-only factory view.
type Status struct {
	Running, DiscoveryPaused, HeartbeatKnown bool
	HeartbeatAge                             time.Duration
	RunsInFlight                             int
	PauseReason, Polling                     string
	ExpectedResumeAt                         time.Time
}

// StatusReader supplies existing factory health at reply time.
type StatusReader interface {
	Read(context.Context, time.Time) (Status, error)
}

// FactoryStatus maps the store's published health record, tracking nothing new.
type FactoryStatus struct{ DB *store.DB }

// Read returns a neutral view. Running means the snapshot is at most five minutes old.
func (f FactoryStatus) Read(ctx context.Context, at time.Time) (Status, error) {
	snap, err := f.DB.ReadHealthSnapshot(ctx)
	if err != nil {
		return Status{}, err
	}
	v := Status{Running: snap.Found && !snap.Listener.DaemonStartedAt.IsZero() && at.Sub(snap.Listener.WrittenAt) <= 5*time.Minute, DiscoveryPaused: snap.Listener.DiscoveryPaused, RunsInFlight: snap.Listener.RunsInFlight, PauseReason: snap.Listener.PauseReason, ExpectedResumeAt: snap.Listener.ExpectedResumeAt}
	if !snap.Listener.LastHeartbeatStartedAt.IsZero() {
		v.HeartbeatKnown = true
		v.HeartbeatAge = max(time.Duration(0), at.Sub(snap.Listener.LastHeartbeatStartedAt)).Round(time.Second)
	}
	owed := 0
	for _, r := range snap.Repos {
		if r.PollOwed {
			owed++
		}
	}
	v.Polling = fmt.Sprintf("Polling: %d repositories, %d awaiting a poll.", len(snap.Repos), owed)
	return v, nil
}

// Handler builds a command reply from the current status.
type Handler func(context.Context, Message, time.Time) (string, error)

// Service owns one transport. A single daemon sender calls Step.
type Service struct {
	stepMu              sync.Mutex
	presentationEnabled bool
	replyModel          ReplyModel
	replyHistory        ask.HistoryReader
	// Protect sanitizes generated text before persistence and delivery.
	Protect func(context.Context, string) (string, error)
	// Logger records terminal failures without remote response bodies.
	Logger           *slog.Logger
	db               *store.DB
	transport, owner string
	channels         map[string]bool
	sender           Transport
	status           StatusReader
	wake             chan struct{}
	mu               sync.RWMutex
	commands         map[string]Handler
	definitions      map[string]CommandDefinition
	ask              *ask.Service
	diagnosis        Diagnosis
	research         ResearchAnswers
	privateOptions   map[string]map[string]string
	drafter          *feature.Drafter
	confirmer        FeatureConfirmer
}

// New copies admission rules and registers help, health and status.
func New(db *store.DB, transport, owner string, channels []string, sender Transport, status StatusReader) *Service {
	s := &Service{db: db, transport: transport, owner: owner, channels: make(map[string]bool), sender: sender, status: status, wake: make(chan struct{}, 1), commands: make(map[string]Handler), definitions: make(map[string]CommandDefinition), privateOptions: make(map[string]map[string]string)}
	for _, c := range channels {
		s.channels[c] = true
	}
	s.RegisterCommand(CommandDefinition{Name: "help", Description: "List factory commands"}, s.help)
	s.RegisterCommand(CommandDefinition{Name: "health", Description: "Read factory health"}, s.health)
	s.RegisterCommand(CommandDefinition{Name: "status", Description: "Read factory status"}, s.summary)
	s.registerMeter()
	return s
}

// Register adds a command without changing transport routing.
func (s *Service) Register(name string, h Handler) {
	s.RegisterCommand(CommandDefinition{Name: name, Description: "Read factory " + name}, h)
}

// RegisterCommand adds a handler and its neutral option declarations.
func (s *Service) RegisterCommand(def CommandDefinition, h Handler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	def.Arguments = append([]Argument(nil), def.Arguments...)
	s.commands[def.Name] = h
	s.definitions[def.Name] = def
}

// Declarations returns a stable copy for transport registration.
func (s *Service) Declarations() []CommandDefinition {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]CommandDefinition, 0, len(s.definitions))
	for _, def := range s.definitions {
		def.Arguments = append([]Argument(nil), def.Arguments...)
		out = append(out, def)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// ReplyLimit reserves space for a raw label before paginating command results.
func (s *Service) ReplyLimit() int {
	limit := presentation.MessageLimit
	if sender, ok := s.sender.(interface{ ReplyLimit() int }); ok && sender.ReplyLimit() > 0 {
		limit = sender.ReplyLimit()
	}
	if s.presentationEnabled {
		limit = max(1, limit-replySize("\n\nRaw result."))
	}
	return limit
}

// Commands returns stable registry names.
func (s *Service) Commands() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	names := make([]string, 0, len(s.commands))
	for name := range s.commands {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Wake tells the daemon about new admissions. Waiting belongs to the daemon.
func (s *Service) Wake() <-chan struct{} { return s.wake }

// Notify wakes the sender after the adapter has acknowledged a new command.
func (s *Service) Notify() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// Admit rejects channel before owner and persists only accepted commands.
func (s *Service) Admit(ctx context.Context, m Message, at time.Time) (allowed, inserted bool, err error) {
	if m.Transport != s.transport || !s.channels[m.Channel] || m.Sender != s.owner {
		return false, false, nil
	}
	r := store.ChatReply{Transport: m.Transport, MessageID: m.ID, ChannelRef: m.Channel, Command: m.Command, Arguments: m.Arguments, CreatedAt: at}
	if s.presentationEnabled {
		r.PresentationState = "pending"
	}
	if m.Command == "ask" && (strings.TrimSpace(m.Arguments["question"]) == "" || len(m.Arguments["question"]) > 8192 || len(m.Arguments["project"]) > 256 || !validArguments(m.Arguments, []string{"question", "project"})) {
		r.ReplyText = ask.TurnPrefix(m.Arguments["question"], m.Arguments["project"]) + "Please give a question up to 8192 bytes and an optional project up to 256 bytes."
		r.Arguments = nil
		r.OutcomeStatus = string(presentation.Refusal)
	}
	if m.Command == "answer" || m.Command == "diagnose" {
		s.mu.Lock()
		defer s.mu.Unlock()
		r.Arguments = make(map[string]string)
		for k, v := range m.Arguments {
			if k == "project" || k == "issue" || (m.Command == "answer" && k == "revision") {
				r.Arguments[k] = v
			}
		}
		inserted, err = s.db.AdmitChatReply(ctx, r)
		if inserted && err == nil {
			options := make(map[string]string)
			for k, v := range m.Arguments {
				options[k] = v
			}
			s.privateOptions[m.Transport+":"+m.ID] = options
		}
		return true, inserted, err
	}

	if m.Command == "draft" && (len(m.Arguments["note"]) > 2000 || len(m.Arguments["project"]) > 256 || !validArguments(m.Arguments, []string{"note", "project"})) {
		r.ReplyText = "Please give a note up to 2000 bytes and optional project up to 256 bytes."
		r.Arguments = nil
		r.OutcomeStatus = string(presentation.Refusal)
	}
	if m.Command == "confirm" && (len(m.Arguments["code"]) != 8 || !validArguments(m.Arguments, []string{"code"})) {
		r.ReplyText = "Please give the 8-character preview code with /confirm."
		r.Arguments = nil
		r.OutcomeStatus = string(presentation.Refusal)
	}
	if r.ReplyText != "" && s.Protect != nil {
		r.ReplyText, err = s.Protect(ctx, r.ReplyText)
		if err != nil {
			return true, false, err
		}
	}
	inserted, err = s.db.AdmitChatReply(ctx, r)
	return true, inserted, err
}

// Step performs one due delivery without sleeping. Ambiguous sends are retried
// best effort; the adapter's deduplication window is not a permanent guarantee.
func (s *Service) Step(ctx context.Context, at time.Time) (bool, error) {
	s.stepMu.Lock()
	defer s.stepMu.Unlock()
	r, err := s.db.ClaimChatReply(ctx, s.transport, at)
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if r.Command == "ask" || r.Command == "draft" || r.Command == "confirm" {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 5*time.Minute)
		defer cancel()
	}
	if !at.Before(r.CreatedAt.Add(24 * time.Hour)) {
		s.mu.Lock()
		delete(s.privateOptions, r.Transport+":"+r.MessageID)
		s.mu.Unlock()
		return true, s.finish(ctx, r, true, at)
	}
	text := r.ReplyText
	if text == "" {
		s.mu.RLock()
		h := s.commands[r.Command]
		s.mu.RUnlock()
		switch {
		case r.Command == "draft" || r.Command == "confirm":
			r, text, err = s.featureReply(ctx, r, at)
			if err != nil {
				return true, err
			}
		case r.Command == "ask":
			r, text, err = s.answer(ctx, r, at)
			if err != nil {
				return true, err
			}
		case h == nil:
			text = "unknown command, try /help"
			r.OutcomeStatus = string(presentation.Refusal)
		default:
			s.mu.Lock()
			if options, ok := s.privateOptions[r.Transport+":"+r.MessageID]; ok {
				r.Arguments = options
				delete(s.privateOptions, r.Transport+":"+r.MessageID)
			}
			s.mu.Unlock()
			handlerCtx, source := presentation.WithAssessment(ctx)
			text, err = h(handlerCtx, Message{Transport: r.Transport, ID: r.MessageID, Channel: r.ChannelRef, Command: r.Command, Arguments: r.Arguments}, at)
			r.OutcomeStatus = string(source.Status)
			if source.RepoID > 0 {
				r.ResolvedRepoID = source.RepoID
			}
			if errors.Is(err, errDeferredDiagnosis) {
				return true, nil
			}
			if err != nil {
				return true, s.retry(ctx, r, at, 0)
			}
		}
		if s.Protect != nil {
			text, err = s.Protect(ctx, text)
			if err != nil {
				return true, s.retry(ctx, r, at, 0)
			}
		}
		if err := s.db.SaveChatReply(ctx, r, text); err != nil {
			return true, err
		}
	}
	if r.PresentationState == "pending" {
		r, err = s.db.ReadChatReply(ctx, r.Transport, r.MessageID)
		if err != nil {
			return true, err
		}
		r, err = s.present(ctx, r, text)
		if err != nil {
			return true, err
		}
		text = r.ReplyText
	}
	target := Target{Transport: r.Transport, MessageID: r.MessageID, Channel: r.ChannelRef, Handle: r.MessageID}
	var d Delivery
	if _, ok := s.sender.(ReplyTransport); !ok && r.ReplyAttachment != "" {
		raw, e := inlineRaw(r)
		if e != nil {
			return true, s.finish(ctx, r, true, at)
		}
		if e = s.db.InlineChatPresentation(ctx, r, raw); e != nil {
			return true, e
		}
		r.ReplyAttachment = ""
		r.ReplyText = raw
		text = raw
	}
	if rich, ok := s.sender.(ReplyTransport); ok && r.ReplyAttachment != "" {
		d = rich.DeliverReply(ctx, target, presentation.Payload{Content: text, Attachment: &presentation.Attachment{Name: presentation.RawFilename, Content: r.ReplyAttachment}})
	} else {
		d = s.sender.Deliver(ctx, target, text)
	}
	if d.AttachmentRejected && r.ReplyAttachment != "" {
		raw, inlineErr := inlineRaw(r)
		if inlineErr != nil {
			return true, s.finish(ctx, r, true, at)
		}
		if err = s.db.InlineChatPresentation(ctx, r, raw); err != nil {
			return true, err
		}
		return true, s.retry(ctx, r, at, 0)
	}
	outcome, level := "retry", slog.LevelInfo
	switch {
	case d.Confirmed:
		outcome, err = "confirmed", s.db.FinishChatReply(ctx, r, false, at)
	case d.Permanent:
		outcome, level, err = "failed", slog.LevelWarn, s.db.FinishChatReply(ctx, r, true, at)
	default:
		err = s.retry(ctx, r, at, d.RetryAfter)
	}
	// One line per delivery attempt; the reply text is never logged.
	if s.Logger != nil {
		s.Logger.Log(ctx, level, "chat reply delivery", slog.String("transport", r.Transport), slog.String("message_id", r.MessageID), slog.String("command", r.Command), slog.String("outcome", outcome), slog.String("route", d.Route), slog.Int("status", d.Status))
	}
	return true, err
}
func (s *Service) retry(ctx context.Context, r store.ChatReply, at time.Time, hint time.Duration) error {
	return s.db.RetryChatReply(ctx, r, retryAt(r, at, hint), at)
}

func retryAt(r store.ChatReply, at time.Time, hint time.Duration) time.Time {
	wait := min(time.Second<<min(max(r.Attempts-1, 0), 10), 15*time.Minute)
	wait = max(wait, min(max(hint, 0), time.Hour))
	next := at.Add(wait)
	if deadline := r.CreatedAt.Add(24 * time.Hour); deadline.Before(next) {
		next = deadline
	}
	return next
}

// Purge removes finished reply state after the retention window.
func (s *Service) Purge(ctx context.Context, at time.Time) error {
	return errors.Join(s.db.PurgeChatReplies(ctx, at), s.db.PurgeChatIssuePreviews(ctx, at), s.db.PurgeChatNotices(ctx, at))
}
func (s *Service) help(ctx context.Context, _ Message, _ time.Time) (string, error) {
	return assessed(ctx, "/"+strings.Join(s.Commands(), ", /"), presentation.Success)
}
func (s *Service) health(ctx context.Context, _ Message, at time.Time) (string, error) {
	v, err := s.status.Read(ctx, at)
	if err != nil {
		return "", err
	}
	return assessed(ctx, healthText(v), presentation.Success)
}
func healthText(v Status) string {
	age := "unknown"
	if v.HeartbeatKnown {
		age = v.HeartbeatAge.String()
	}
	return fmt.Sprintf("Running: %t. Heartbeat age: %s. Discovery paused: %t.", v.Running, age, v.DiscoveryPaused)
}
func (s *Service) summary(ctx context.Context, _ Message, at time.Time) (string, error) {
	v, err := s.status.Read(ctx, at)
	if err != nil {
		return "", err
	}
	reason, resume := v.PauseReason, "not scheduled"
	if reason == "" {
		reason = "none"
	}
	if !v.ExpectedResumeAt.IsZero() {
		resume = v.ExpectedResumeAt.UTC().Format(time.RFC3339)
	} else if v.DiscoveryPaused {
		resume = "when fresh rate-limit data arrives"
	}
	text := fmt.Sprintf("%s\nRuns in flight: %d. Pause reason: %s. Expected resume: %s.\n%s", healthText(v), v.RunsInFlight, reason, resume, v.Polling)
	return assessed(ctx, text, presentation.Success)
}

func (s *Service) finish(ctx context.Context, r store.ChatReply, failed bool, at time.Time) error {
	err := s.db.FinishChatReply(ctx, r, failed, at)
	if err == nil && failed && s.Logger != nil {
		s.Logger.WarnContext(ctx, "chat reply failed permanently or exceeded its retry window", slog.String("transport", r.Transport), slog.String("message_id", r.MessageID))
	}
	return err
}
