// Package messaging admits enabled owner commands and manages transport-neutral replies.
package messaging

import (
	"context"
	"errors"
	"fmt"
	"github.com/cloudvane/litefactory/internal/messagingstate"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cloudvane/litefactory/internal/factory"
	"github.com/cloudvane/litefactory/internal/factory/contracts"
	"github.com/cloudvane/litefactory/internal/messaging/ask"
	"github.com/cloudvane/litefactory/internal/messaging/feature"
	"github.com/cloudvane/litefactory/internal/messaging/presentation"
	"github.com/cloudvane/litefactory/internal/messaging/prompts"
	"github.com/cloudvane/litefactory/internal/messaging/response"
	"github.com/cloudvane/litefactory/internal/messagingstorage"
	"github.com/cloudvane/litefactory/internal/owneraction"
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
	// NaturalLanguage and Effect opt into suggestion routing, never execution.
	NaturalLanguage bool
	Effect          CommandEffect
}

// Target identifies a reply; Handle is opaque and credentials remain in the adapter.
type Target struct {
	Transport, MessageID, Channel, Handle string
	Protected                             bool
	// Notice marks an unsolicited channel notice; transports suppress link
	// previews for it.
	Notice bool
}

// Delivery describes confirmation or permanent/temporary failure. Route and
// Status describe the last transport call for logging only.
type Delivery struct {
	Confirmed, Permanent bool
	// PromptID is the platform message identity, returned only on known delivery.
	PromptID           string
	AttachmentRejected bool
	RetryAfter         time.Duration
	Route              string
	Status             int
	// EditStatus, EditCode and EditMessage describe a refused interaction
	// edit: its HTTP status and Discord's redacted JSON error code and message.
	EditStatus  int
	EditCode    int
	EditMessage string
}

// Transport supplies delivery and its outcome without service-specific types.
type Transport interface {
	Deliver(context.Context, Target, string) Delivery
}

// Status is the bounded factory health view retained under its existing consumer name.
type Status = contracts.Health

// StatusReader supplies existing factory health at reply time.
type StatusReader = contracts.HealthReader

// Handler builds a command reply from the current status.
type Handler func(context.Context, Message, time.Time) (string, error)

// Service owns one transport. A single daemon sender calls Step.
type Service struct {
	providerObserverMu     sync.RWMutex
	providerObserver       func(context.Context, string, string, string, string)
	inboxReader            OwnerInboxReader
	Instructions           prompts.Resolver
	InstructionDiagnostics prompts.Sink
	stepMu                 sync.Mutex
	// Protect sanitizes generated text before persistence and delivery.
	Protect func(context.Context, string) (string, error)
	// Logger records terminal failures without remote response bodies.
	Logger                   *slog.Logger
	db                       messagingstorage.Port
	factory                  *factory.Messaging
	transport, owner         string
	channels                 map[string]bool
	sender                   Transport
	status                   StatusReader
	wake                     chan struct{}
	mu                       sync.RWMutex
	commands                 map[string]Handler
	definitions              map[string]CommandDefinition
	ask                      *ask.Service
	diagnosis                Diagnosis
	research                 ResearchAnswers
	drafter                  *feature.Drafter
	confirmer                FeatureConfirmer
	conversations            atomic.Pointer[conversationRuntime]
	standards                StandardsOperations
	ownerActions             OwnerActions
	ownerPrincipal           func(context.Context, string) (owneraction.Principal, error)
	workflowNoticeAuthorizer func(context.Context, string, string) error
	alertControl             func(context.Context, string, string, time.Time) (string, error)
}

// SetWorkflowNoticeAuthorizer installs the current authority and destination check.
// Workflow notices fail closed until daemon composition provides this callback.
func (s *Service) SetWorkflowNoticeAuthorizer(authorize func(context.Context, string, string) error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.workflowNoticeAuthorizer = authorize
}

// NewWithServices receives messaging persistence and bounded factory operations.
func NewWithServices(db messagingstorage.Port, core *factory.Messaging, transport, owner string, channels []string, sender Transport, status StatusReader) *Service {
	s := &Service{db: db, factory: core, transport: transport, owner: owner, channels: make(map[string]bool), sender: sender, status: status, wake: make(chan struct{}, 1), commands: make(map[string]Handler), definitions: make(map[string]CommandDefinition)}
	for _, c := range channels {
		s.channels[c] = true
	}
	s.RegisterCommand(CommandDefinition{Name: "help", NaturalLanguage: true, Effect: ReadEffect, Description: "List factory commands"}, s.help)
	s.RegisterCommand(CommandDefinition{Name: "health", NaturalLanguage: true, Effect: ReadEffect, Description: "Read factory health"}, s.health)
	s.RegisterCommand(CommandDefinition{Name: "status", NaturalLanguage: true, Effect: ReadEffect, Description: "Read a selected task, or factory health and running/owner-waiting tasks", Arguments: []Argument{{Name: "project", Type: TextArgument, Description: "Registered project"}, {Name: "issue", Type: IntegerArgument, Description: "Known GitHub issue number"}, {Name: "task", Type: IntegerArgument, Description: "Known GitHub task number"}}}, s.summary)
	s.RegisterCommand(CommandDefinition{Name: "details", NaturalLanguage: true, Effect: ReadEffect, Description: "Show saved raw details for a conversation answer", Arguments: []Argument{{Name: "answer", Type: TextArgument, Description: "Saved answer reference; omit for latest answer"}}}, nil)
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

// SlashCommands returns the only transport command, /details. Ordinary
// messages in a bound channel go to the conversation instead.
func (s *Service) SlashCommands() []CommandDefinition {
	s.mu.RLock()
	defer s.mu.RUnlock()
	def, ok := s.definitions["details"]
	if !ok {
		return nil
	}
	def.Arguments = append([]Argument(nil), def.Arguments...)
	return []CommandDefinition{def}
}

// ReplyLimit is the transport's message size used to paginate command results.
func (s *Service) ReplyLimit() int {
	limit := presentation.MessageLimit
	if sender, ok := s.sender.(interface{ ReplyLimit() int }); ok && sender.ReplyLimit() > 0 {
		limit = sender.ReplyLimit()
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

// Admit rejects channel before owner and persists only /details requests.
// Other commands from the owner are allowed but never inserted.
func (s *Service) Admit(ctx context.Context, m Message, at time.Time) (allowed, inserted bool, err error) {
	if m.Transport != s.transport || !s.channels[m.Channel] || m.Sender != s.owner {
		return false, false, nil
	}
	if m.Command != "details" {
		return true, false, nil
	}
	r := messagingstate.ChatReply{Transport: m.Transport, MessageID: m.ID, ChannelRef: m.Channel, Command: m.Command, Arguments: m.Arguments, CreatedAt: at}
	if !validArguments(m.Arguments, []string{"answer"}) || len(m.Arguments["answer"]) > 128 || strings.ContainsAny(m.Arguments["answer"], "\r\n") {
		r.ReplyText = "Please give the answer reference printed on the reply."
		r.OutcomeStatus = string(presentation.Refusal)
		inserted, err = s.db.AdmitChatReply(ctx, r)
		return true, inserted, err
	}
	inserted, err = s.db.AdmitConversationDetails(ctx, r, s.owner, m.Arguments["answer"], at)
	return true, inserted, err
}

// Step performs one due /details delivery without sleeping. Ambiguous sends are
// retried best effort; the adapter's deduplication window is not a permanent
// guarantee. Rows of retired commands are closed without delivery.
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
	if r.Command != "details" || !at.Before(r.CreatedAt.Add(24*time.Hour)) {
		return true, s.finish(ctx, r, true, at)
	}
	if r.ReplyText == "" {
		selected, e := s.db.ConversationDetailsSelection(ctx, r)
		if e != nil && !errors.Is(e, store.ErrNotFound) {
			return true, e
		}
		if selected != "" {
			runtime := s.conversations.Load()
			if runtime != nil && runtime.bindings[r.ChannelRef].Provider != "" {
				// The request lease and attempt fence protect export persistence; slow
				// access checks must not hold the ordinary command coordinator's lock.
				s.stepMu.Unlock()
				defer s.stepMu.Lock()
				work, cancel := context.WithTimeout(ctx, response.Budget)
				defer cancel()
				return true, s.answerDetails(work, r, selected, at)
			}
			r.ReplyText = detailsDenied
		} else {
			r.ReplyText = detailsRefusal
		}
		r.OutcomeStatus = string(presentation.Refusal)
		if err := s.db.SaveChatReply(ctx, r, r.ReplyText); err != nil {
			return true, err
		}
	}
	target := Target{Transport: r.Transport, MessageID: r.MessageID, Channel: r.ChannelRef, Handle: r.MessageID}
	d := s.sender.Deliver(ctx, target, r.ReplyText)
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
		attrs := []any{slog.String("transport", r.Transport), slog.String("message_id", r.MessageID), slog.String("command", r.Command), slog.String("outcome", outcome), slog.String("route", d.Route), slog.Int("status", d.Status)}
		if d.EditStatus != 0 {
			attrs = append(attrs, slog.Int("edit_status", d.EditStatus), slog.Int("edit_code", d.EditCode), slog.String("edit_message", d.EditMessage))
		}
		s.Logger.Log(ctx, level, "chat reply delivery", attrs...)
	}
	return true, err
}
func (s *Service) retry(ctx context.Context, r messagingstate.ChatReply, at time.Time, hint time.Duration) error {
	return s.db.RetryChatReply(ctx, r, retryAt(r, at, hint), at)
}

func retryAt(r messagingstate.ChatReply, at time.Time, hint time.Duration) time.Time {
	wait := min(time.Second<<min(max(r.Attempts-1, 0), 10), 15*time.Minute)
	wait = max(wait, max(hint, 0))
	next := at.Add(wait)
	if deadline := r.CreatedAt.Add(24 * time.Hour); deadline.Before(next) {
		next = deadline
	}
	return next
}

// Purge removes finished reply state after the retention window.
func (s *Service) Purge(ctx context.Context, at time.Time) error {
	return errors.Join(s.factory.ExpireInboxes(ctx, at), s.db.PurgeChatReplies(ctx, at), s.db.PurgeChatIssuePreviews(ctx, at), s.db.PurgeChatNotices(ctx, at), s.db.PurgeNaturalChat(ctx, at), s.db.PurgeConversationBodies(ctx, at), s.db.PurgeConversationEvidence(ctx, at))
}
func (s *Service) help(ctx context.Context, _ Message, _ time.Time) (string, error) {
	// Only /details is a slash command; everything else is asked for in the conversation.
	return assessed(ctx, "Ask me in plain words. I can look up or do: "+strings.Join(s.Commands(), ", ")+". For the raw details of an answer, use /details.", presentation.Success)
}
func (s *Service) health(ctx context.Context, _ Message, at time.Time) (string, error) {
	v, err := s.status.Read(ctx, at)
	if err != nil {
		return "", err
	}
	return assessed(ctx, healthText(v)+s.conversationHealth(ctx, at), presentation.Success)
}
func healthText(v Status) string {
	age := "unknown"
	if v.HeartbeatKnown {
		age = v.HeartbeatAge.String()
	}
	return fmt.Sprintf("Running: %t. Heartbeat age: %s. Discovery paused: %t.", v.Running, age, v.DiscoveryPaused)
}
func (s *Service) summary(ctx context.Context, m Message, at time.Time) (string, error) {
	if m.Arguments["issue"] != "" || m.Arguments["task"] != "" {
		resolution, err := s.resolveNaturalProject(ctx, m.Arguments)
		if err != nil {
			return "", err
		}
		if resolution.Refusal != "" {
			return assessed(ctx, resolution.Refusal, presentation.Refusal)
		}
		number, _ := strconv.Atoi(m.Arguments["issue"])
		if number == 0 {
			number, _ = strconv.Atoi(m.Arguments["task"])
		}
		session, err := s.knownNumericTask(ctx, resolution.Repository, number)
		if errors.Is(err, store.ErrNotFound) {
			return assessed(ctx, "Requested task unavailable in that project.", presentation.Refusal)
		}
		if err != nil {
			return "", err
		}
		text, err := s.factory.Status.Task(ctx, session.ID, at)
		if err != nil {
			return "", err
		}
		return assessed(ctx, text, presentation.Success)
	}

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
	text := fmt.Sprintf("%s\nPause reason: %s. Expected resume: %s.\n", healthText(v), reason, resume)
	if v.RunsHeld != "" {
		text += "Runs held: " + v.RunsHeld + "\n"
	}
	text += v.Polling
	text += s.taskSummary(ctx, v, at)
	return assessed(ctx, text, presentation.Success)
}

func (s *Service) finish(ctx context.Context, r messagingstate.ChatReply, failed bool, at time.Time) error {
	err := s.db.FinishChatReply(ctx, r, failed, at)
	if err == nil && failed && s.Logger != nil {
		s.Logger.WarnContext(ctx, "chat reply failed permanently or exceeded its retry window", slog.String("transport", r.Transport), slog.String("message_id", r.MessageID))
	}
	return err
}

// taskSummary records only the task references actually shown by the factory view.
func (s *Service) taskSummary(ctx context.Context, v Status, at time.Time) string {
	view := s.factory.Status.Summary(ctx, v, at)
	for _, shown := range view.Shown {
		recordListedIssue(ctx, listedIssue{RepoID: shown.RepoID, Project: shown.Project, Issue: shown.Issue})
	}
	return view.Text
}
