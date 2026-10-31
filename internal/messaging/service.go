// Package messaging admits enabled owner commands and manages transport-neutral replies.
package messaging

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cloudvane/litefactory/internal/messaging/ask"
	"github.com/cloudvane/litefactory/internal/messaging/feature"
	"github.com/cloudvane/litefactory/internal/messaging/presentation"
	"github.com/cloudvane/litefactory/internal/messaging/prompts"
	"github.com/cloudvane/litefactory/internal/messaging/response"
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

// Status contains the current read-only factory view.
type Status struct {
	Running, DiscoveryPaused, HeartbeatKnown bool
	HeartbeatAge                             time.Duration
	RunsInFlight                             int
	// InFlightSessions are the sessions behind RunsInFlight; SlotsUsed and
	// SlotsTotal are the worker pool's occupancy, zero total when unknown.
	InFlightSessions []int64
	// InFlightLabels names the dispatch source of an in-flight session's
	// worker, for example "backlog+graph_owner_wait"; absent when unknown.
	InFlightLabels        map[int64]string
	SlotsUsed, SlotsTotal int
	PauseReason, Polling  string
	ExpectedResumeAt      time.Time
	// RunsHeld says why new runs wait, for example on low free disk; empty
	// while nothing holds them.
	RunsHeld string
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
	v := Status{Running: snap.Found && !snap.Listener.DaemonStartedAt.IsZero() && at.Sub(snap.Listener.WrittenAt) <= 5*time.Minute, DiscoveryPaused: snap.Listener.DiscoveryPaused, RunsInFlight: snap.Listener.RunsInFlight, InFlightSessions: snap.Listener.InFlightSessions, InFlightLabels: snap.Listener.InFlightLabels, SlotsUsed: snap.Listener.SlotsUsed, SlotsTotal: snap.Listener.SlotsTotal, PauseReason: snap.Listener.PauseReason, ExpectedResumeAt: snap.Listener.ExpectedResumeAt, RunsHeld: snap.Listener.RunsHeld}
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
	inboxReader            OwnerInboxReader
	Instructions           prompts.Resolver
	InstructionDiagnostics prompts.Sink
	stepMu                 sync.Mutex
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
	drafter          *feature.Drafter
	confirmer        FeatureConfirmer
	conversations    atomic.Pointer[conversationRuntime]
	standards        StandardsOperations
	ownerActions     *owneraction.Service
	ownerPrincipal   func(context.Context, string) (owneraction.Principal, error)
}

// New copies admission rules and registers help, health, status and details.
// Only /details is a transport command; the others serve the conversation.
func New(db *store.DB, transport, owner string, channels []string, sender Transport, status StatusReader) *Service {
	s := &Service{db: db, transport: transport, owner: owner, channels: make(map[string]bool), sender: sender, status: status, wake: make(chan struct{}, 1), commands: make(map[string]Handler), definitions: make(map[string]CommandDefinition)}
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
	r := store.ChatReply{Transport: m.Transport, MessageID: m.ID, ChannelRef: m.Channel, Command: m.Command, Arguments: m.Arguments, CreatedAt: at}
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
	return errors.Join(s.db.PurgeInboxSnapshots(ctx, at), s.db.PurgeChatReplies(ctx, at), s.db.PurgeChatIssuePreviews(ctx, at), s.db.PurgeChatNotices(ctx, at), s.db.PurgeNaturalChat(ctx, at), s.db.PurgeConversationBodies(ctx, at), s.db.PurgeConversationEvidence(ctx, at))
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
		tasks, err := (ask.StoreReader{Reader: s.db}).ReadTaskSessions(ctx, []store.Session{session})
		if err != nil {
			return "", err
		}
		text, err := s.taskStatus(ctx, session, tasks[0], at)
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

// inFlightSource words the dispatch source of an in-flight worker that has no
// run or graph step. Ordinary candidate sources are the worker checking the
// task for input; the factory's own dispatches are named as they are.
func inFlightSource(label string) string {
	switch label {
	case "due_schedule", "graph_step", "automatic_continuation", "stopped_result":
		return label
	case "":
		return "checking for input"
	}
	return "checking for input (" + strings.ReplaceAll(label, "+", ", ") + ")"
}

func (s *Service) finish(ctx context.Context, r store.ChatReply, failed bool, at time.Time) error {
	err := s.db.FinishChatReply(ctx, r, failed, at)
	if err == nil && failed && s.Logger != nil {
		s.Logger.WarnContext(ctx, "chat reply failed permanently or exceeded its retry window", slog.String("transport", r.Transport), slog.String("message_id", r.MessageID))
	}
	return err
}

// taskSummary lists, from the store, what is running now, what is queued for a
// slot or a retry, and what waits on the owner. The Running now count is the
// number of listed lines. An in-flight task with no run or graph step is named
// by its task and the dispatch source that started its worker; an in-flight
// session that cannot be resolved is counted in the header.
func (s *Service) taskSummary(ctx context.Context, v Status, at time.Time) string {
	repos, err := s.db.ListRepos(ctx)
	if err != nil {
		return "\nTask listing unavailable: registered projects could not be read."
	}
	githubRepo := map[int64]store.Repo{}
	for _, repo := range repos {
		if repo.Forge == "github" {
			githubRepo[repo.ID] = repo
		}
	}
	// task records a listed task so the owner can act on "that one" later.
	task := func(repoID int64, issue int, text string) statusEntry {
		return statusEntry{text: text, item: listedIssue{RepoID: repoID, Project: githubRepo[repoID].Slug(), Issue: issue}}
	}

	var running, queued []statusEntry
	busy := map[int64]bool{}
	active, err := s.db.ListActiveTaskWork(ctx)
	if err != nil {
		running = append(running, statusEntry{text: "Running task data could not be read."})
	}
	for _, w := range active {
		busy[w.SessionID] = true
		if _, ok := githubRepo[w.RepoID]; !ok {
			continue
		}
		mode := w.Mode
		if mode == "" {
			mode = "unavailable"
		}
		line := w.TaskRef + " · " + mode
		if w.Node != "" {
			line += " · step " + w.Node
		}
		running = append(running, task(w.RepoID, w.IssueNumber, line+" · since "+statusClock(w.Since, at)))
	}
	background, err := s.db.ListBackgroundWork(ctx, at)
	if err != nil {
		running = append(running, statusEntry{text: "Background work data could not be read."})
	}
	for _, w := range background {
		line := w.Kind + " · " + w.What
		if w.Queued {
			queued = append(queued, statusEntry{text: line + " · waiting since " + statusClock(w.Since, at)})
		} else {
			running = append(running, statusEntry{text: line + " · since " + statusClock(w.Since, at)})
		}
	}
	// Only a live daemon's slots count. A slot with no task work is named by
	// its task and real dispatch source; one that cannot be resolved is counted.
	unidentified := 0
	inFlight := map[int64]bool{}
	if v.Running {
		if len(v.InFlightSessions) > 0 {
			for _, id := range v.InFlightSessions {
				if busy[id] {
					continue
				}
				session, err := s.db.GetSession(ctx, id)
				if _, ok := githubRepo[session.RepoID]; err != nil || !ok {
					unidentified++
					continue
				}
				inFlight[id] = true
				running = append(running, task(session.RepoID, session.IssueNumber, session.TaskRef+" · "+inFlightSource(v.InFlightLabels[id])))
			}
		} else {
			unidentified = max(0, v.RunsInFlight-len(busy))
		}
	}

	slot := "waiting for the next heartbeat"
	if v.SlotsTotal > 0 && v.SlotsUsed >= v.SlotsTotal {
		slot = fmt.Sprintf("waiting for a free slot (%d/%d busy)", v.SlotsUsed, v.SlotsTotal)
	}
	waits, err := s.db.ListQueuedTaskWork(ctx, at)
	if err != nil {
		queued = append(queued, statusEntry{text: "Queued task data could not be read."})
	}
	listed := map[int64]bool{}
	for _, w := range waits {
		if _, ok := githubRepo[w.RepoID]; busy[w.SessionID] || inFlight[w.SessionID] || listed[w.SessionID] || !ok {
			continue
		}
		listed[w.SessionID] = true
		line := w.TaskRef + " · "
		if w.Node != "" {
			line += "step " + w.Node + " · "
		}
		if w.DueAt.After(at) {
			line += "retry at " + statusClock(w.DueAt, at)
		} else {
			line += slot
		}
		queued = append(queued, task(w.RepoID, w.IssueNumber, line))
	}

	waiting, complete := s.ownerWaits(ctx, repos, busy, at)
	if complete && len(waiting) == 0 {
		waiting = append(waiting, statusEntry{text: "none."})
	}

	header := fmt.Sprintf("Running now: %d", len(running))
	if unidentified > 0 {
		header += fmt.Sprintf(" (+%d unidentified)", unidentified)
	}
	sections := []struct {
		header string
		lines  []statusEntry
	}{{header + ".", running}, {fmt.Sprintf("Queued: %d.", len(queued)), queued}, {"Waiting on you:", waiting}}
	// Keep the factual source bounded; presentation retains it as details when needed.
	text := "\nScope: registered GitHub code projects and chat background work; running, queued and owner-waiting work, including parked work. Idle tasks require a task-specific status lookup.\n"
	for _, section := range sections {
		text += section.header + "\n"
		for i, line := range section.lines {
			if len(text)+len(line.text) > 16000 {
				text += fmt.Sprintf("Task listing: %d additional entries left out.\n", len(section.lines)-i)
				break
			}
			text += "- " + line.text + "\n"
			// Only shown tasks are recorded.
			if line.item.RepoID > 0 {
				recordListedIssue(ctx, line.item)
			}
		}
	}
	return strings.TrimRight(text, "\n")
}

// statusEntry is one listed line and, for a task, the issue it shows.
type statusEntry struct {
	text string
	item listedIssue
}

// ownerWaits lists tasks that wait on the owner: a published question or
// review, or a parked story. Busy tasks are listed as running instead.
func (s *Service) ownerWaits(ctx context.Context, repos []store.Repo, busy map[int64]bool, at time.Time) ([]statusEntry, bool) {
	reader := ask.StoreReader{Reader: s.db}
	var lines []statusEntry
	complete := true
	for _, repo := range repos {
		if repo.Forge != "github" {
			continue
		}
		var after int64
		for {
			sessions, err := s.db.ListTaskSessionsPage(ctx, repo.ID, after, 100)
			if err != nil {
				complete = false
				lines = append(lines, statusEntry{text: repo.Slug() + ": task data could not be read."})
				break
			}
			if len(sessions) == 0 {
				break
			}
			after = sessions[len(sessions)-1].ID
			tasks, err := reader.ReadTaskSessions(ctx, sessions)
			if err != nil {
				complete = false
				lines = append(lines, statusEntry{text: repo.Slug() + ": task data could not be read."})
				break
			}
			for index, task := range tasks {
				if busy[sessions[index].ID] || task.OwnerWait == nil && task.OwnerBlocked == "" {
					continue
				}
				text, err := s.taskStatus(ctx, sessions[index], task, at)
				if err != nil {
					complete = false
					lines = append(lines, statusEntry{text: task.Reference + ": task data could not be read."})
				} else {
					lines = append(lines, statusEntry{text: text, item: listedIssue{RepoID: repo.ID, Project: repo.Slug(), Issue: sessions[index].IssueNumber}})
				}
			}
		}
	}
	return lines, complete
}

// statusClock shows a time in the reply's zone: the clock alone today, with
// the day otherwise.
func statusClock(t, at time.Time) string {
	t = t.In(at.Location())
	if y, m, d := t.Date(); y == at.Year() && m == at.Month() && d == at.Day() {
		return t.Format("15:04")
	}
	return t.Format("Mon 2 Jan 15:04")
}

// taskStatus uses retained state; absent facts stay explicitly unavailable.
func (s *Service) taskStatus(ctx context.Context, session store.Session, task ask.Task, at time.Time) (string, error) {
	mode, stage, waiting, state := "unavailable", "unavailable", "none recorded", task.State
	runs, err := s.db.RecentTaskRuns(ctx, session.ID, 3)
	if err != nil {
		return "", err
	}
	if len(runs) > 0 {
		mode = runs[0].Mode
		if mode == "" {
			mode = runs[0].Workflow
		}
		if mode == "" {
			mode = "unavailable"
		}
	}
	instance, err := s.db.LatestSessionGraphInstance(ctx, session.ID)
	if err == nil {
		stage = instance.CurrentNode + " (" + instance.Status + ")"
		definition, err := s.db.GetGraphDefinition(ctx, instance.DefinitionID)
		if err != nil {
			return "", err
		}
		mode = definition.Mode
		if mode == "" {
			mode = definition.Workflow
		}
		if mode == "" {
			mode = "unavailable"
		}
		if instance.Status == store.GraphRunning {
			// A running graph supersedes the failed flag an earlier attempt left.
			if state == string(store.SessionFailed) {
				state = "in progress (an earlier attempt failed; work resumed)"
			}
			dispatch, err := s.db.LatestGraphDispatch(ctx, instance.ID)
			switch {
			case err == nil && dispatch.Status == "pending":
				stage += ", next step due " + dispatch.DueAt.UTC().Format(time.RFC3339)
			case err == nil && (dispatch.Status == "reserved" || dispatch.Status == "running"):
				stage += ", next step starting"
			case err != nil && !errors.Is(err, store.ErrNotFound):
				return "", err
			}
		}
	} else if !errors.Is(err, store.ErrNotFound) {
		return "", err
	}
	var stories []store.ImplementStory
	if mode == "implement" {
		if stories, err = s.db.LatestImplementStories(ctx, session.TaskRef); err != nil {
			return "", err
		}
	}
	progress := ""
	if len(stories) > 0 {
		var parked string
		progress, parked, err = s.implementProgress(ctx, stories, at)
		if err != nil {
			return "", err
		}
		if parked != "" {
			waiting = "owner — " + parked
		}
	} else if len(task.PlanStages) > 0 {
		stage += "; plan stories: " + strings.Join(task.PlanStages, "; ")
	}
	if task.OwnerWait != nil {
		waiting = "owner — " + task.OwnerWait.Workflow + " " + task.OwnerWait.Kind
	}
	if task.OwnerBlocked != "" {
		waiting = "owner — parked: " + task.OwnerBlocked
		story, err := s.db.ImplementStoryByWorkerSession(ctx, session.ID)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return "", err
		}
		if err == nil && story.State == store.ImplementParked {
			waiting = "owner — " + parkedWait(story, "story "+story.Key, at)
		}
	}
	text := fmt.Sprintf("%s: %s. Mode: %s. Stage: %s.%s Waiting on: %s. Recent runs on this task (newest first, limit 3):", task.Reference, state, mode, stage, progress, waiting)
	if len(runs) == 0 {
		text += " unavailable."
	}
	for _, run := range runs {
		text += fmt.Sprintf(" %d: %s (%s);", run.Ordinal, run.Status, run.Mode)
	}
	return text, nil
}

// implementProgress reports a feature's Implement ledger from the local store:
// per-story state of the newest generation with story issue numbers, failed
// attempts and the latest failure, plus active runs on the story tasks. It
// also returns the first parked story, which waits on the owner.
func (s *Service) implementProgress(ctx context.Context, stories []store.ImplementStory, at time.Time) (string, string, error) {
	counts := map[string]int{}
	var lines, active []string
	parked := ""
	for _, story := range stories {
		counts[story.State]++
		label, latest := story.Key, ""
		if story.WorkerSession > 0 {
			worker, err := s.db.GetSession(ctx, story.WorkerSession)
			if err != nil && !errors.Is(err, store.ErrNotFound) {
				return "", "", err
			}
			if err == nil {
				label += fmt.Sprintf(" (#%d)", worker.IssueNumber)
				run, err := s.db.LatestTaskRun(ctx, worker.ID)
				if err != nil && !errors.Is(err, store.ErrNotFound) {
					return "", "", err
				}
				switch {
				case err == nil && (run.Status == store.RunRunning || run.Status == store.RunPrelaunch):
					active = append(active, fmt.Sprintf("#%d run %d (%s) running since %s", worker.IssueNumber, run.Ordinal, run.Mode, run.StartedAt.UTC().Format(time.RFC3339)))
				case err == nil && story.State == store.ImplementRunning:
					latest = fmt.Sprintf(", no active run (latest run %d %s)", run.Ordinal, run.Status)
				}
			}
		}
		line := label + " " + story.State + latest
		if story.Attempts > 0 {
			line += fmt.Sprintf(", %d failed attempts", story.Attempts)
		}
		if story.State == store.ImplementParked {
			line += ", parked: " + statusExcerpt(story.Reason)
			if parked == "" {
				parked = parkedWait(story, label, at)
			}
		}
		if story.State != store.ImplementParked && story.State != store.ImplementMerged && story.GuidanceQuestion != "" && story.Guidance == "" {
			line += ", restarted after: " + statusExcerpt(story.GuidanceQuestion)
		}
		if story.LastFailure != "" && story.State != store.ImplementMerged {
			line += ", last failure: " + statusExcerpt(story.LastFailure)
		}
		lines = append(lines, line)
	}
	var tally []string
	for _, state := range []string{store.ImplementMerged, store.ImplementDone, store.ImplementRunning, store.ImplementWaiting, store.ImplementParked} {
		if counts[state] > 0 {
			tally = append(tally, fmt.Sprintf("%d %s", counts[state], state))
		}
	}
	text := fmt.Sprintf(" Implementation stories (generation %d, %d total): %s. Per story: %s.", stories[0].Generation, len(stories), strings.Join(tally, ", "), strings.Join(lines, "; "))
	if len(active) == 0 {
		text += " Active story runs: none."
	} else {
		text += " Active story runs: " + strings.Join(active, "; ") + "."
	}
	return text, parked, nil
}

// parkedWait is a parked story's owner wait: since when, why, and the reply on
// the parent issue that restarts it, in the owner question's own words.
func parkedWait(story store.ImplementStory, label string, at time.Time) string {
	since := story.StateAt
	if since.IsZero() {
		since = story.UpdatedAt
	}
	return fmt.Sprintf("parked since %s (%s): %s. To resume: reply on %s to restart the stopped stories. %s", statusClock(since, at), label, strings.TrimSuffix(statusExcerpt(story.Reason), "."), story.Parent, store.ImplementResumeGuidance)
}

// statusExcerpt keeps a ledger reason on one bounded line.
func statusExcerpt(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	if bounded, cut := boundedConversationText(text, 200); cut {
		return bounded + "…"
	}
	return text
}
