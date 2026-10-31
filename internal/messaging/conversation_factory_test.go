package messaging

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/messaging/presentation"
	"github.com/cloudvane/litefactory/internal/owneraction"
	"github.com/cloudvane/litefactory/internal/store"
)

type factoryUnitProvider struct {
	outputs []string
	calls   int
	fail    bool
	prompts []string
}

func (p *factoryUnitProvider) RunConversation(ctx context.Context, r agent.ConversationRequest) (agent.ConversationResult, error) {
	if err := r.BeforeLaunch(ctx); err != nil {
		return agent.ConversationResult{}, agent.ConversationError(err, "before_launch", true)
	}
	p.calls++
	p.prompts = append(p.prompts, r.Prompt)
	session := r.Session
	if session.Resume == "" {
		session.Resume = "unit-session"
	}
	if err := r.OnSession(ctx, session); err != nil {
		return agent.ConversationResult{}, err
	}
	if p.fail {
		return agent.ConversationResult{}, errors.New("provider completion unavailable")
	}
	value := p.outputs[min(p.calls-1, len(p.outputs)-1)]
	return agent.ConversationResult{Session: session, Answer: []byte(value)}, nil
}

func factoryUnitRun(t *testing.T, p *factoryUnitProvider, input ...string) (*factoryConversationRun, time.Time) {
	t.Helper()
	text := "How is the factory?"
	if len(input) > 0 {
		text = input[0]
	}
	db := naturalExecutionDB(t)
	at := time.Now().UTC()
	service := New(db, "discord", "1", []string{"2"}, nil, FactoryStatus{DB: db})
	service.Protect = func(_ context.Context, text string) (string, error) { return text, nil }
	root := filepath.Join(t.TempDir(), "sessions")
	binding := ConversationBinding{Channel: "2", Name: "owner", Provider: "claude", Model: "fixture"}
	if err := service.EnableConversations(t.Context(), ConversationOptions{Bindings: []ConversationBinding{binding}, StateRoot: root, Providers: map[string]ConversationProvider{"claude": p}, CheckAccess: func(context.Context, string) error { return nil }, Factory: true}, at); err != nil {
		t.Fatal(err)
	}
	c, err := db.ReadConversation(t.Context(), "discord", "2")
	if err != nil {
		t.Fatal(err)
	}
	if err = db.ActivateNaturalScan(t.Context(), "2", "100"); err != nil {
		t.Fatal(err)
	}
	scan, err := db.ReadNaturalScan(t.Context(), "2")
	if err != nil {
		t.Fatal(err)
	}
	if err = db.StageNaturalScan(t.Context(), scan, store.NaturalScan{ChannelRef: "2", Cursor: "100", HighWater: "101"}, []store.NaturalMessage{{ChannelRef: "2", SourceID: "101", OwnerID: "1", Text: text, CreatedAt: at}}, true); err != nil {
		t.Fatal(err)
	}
	instructions, err := service.restoreConversationInstructions(t.Context(), c.ID, at)
	if err != nil {
		t.Fatal(err)
	}
	prompt, err := agent.ConversationPrompt(instructions.Native(), text, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.AdmitConversationTurn(t.Context(), c.ID, "101", prompt, at); err != nil {
		t.Fatal(err)
	}
	turn, err := db.ClaimConversationTurn(t.Context(), c.ID, "unit-worker", at, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	return &factoryConversationRun{s: service, c: c, b: binding, t: turn, at: at}, at
}

// factoryFixedAction supplies the already reviewed domain facts for unit tests
// of execution outcomes. Execution still uses the production shared backend.
type factoryFixedAction struct {
	service *Service
	payload naturalPayload
}

func (d factoryFixedAction) Resolve(ctx context.Context, _ owneraction.Principal, in owneraction.Invocation, _ time.Time) (owneraction.Facts, error) {
	repo, err := d.service.db.GetRepo(ctx, d.payload.RepoID)
	if err != nil {
		return owneraction.Facts{}, err
	}
	raw, err := json.Marshal(ownerActionPayload{Natural: d.payload})
	if err != nil {
		return owneraction.Facts{}, err
	}
	target := d.payload.PreviewID
	if target == "" {
		target = "github:" + d.payload.Project + "#" + in.Arguments["issue"]
	}
	revision := d.payload.PreviewHash
	if revision == "" {
		revision = fmt.Sprint(repo.ID)
	}
	if in.Command == "diagnose" {
		session, err := d.service.db.GetOrCreateSession(ctx, repo, mustNumber(in.Arguments["issue"]))
		if err != nil {
			return owneraction.Facts{}, err
		}
		target, revision = session.TaskRef, store.OwnerActionSessionRevision(session)
	}
	return owneraction.Facts{Invocation: in, RepositoryID: repo.ID, RepositoryRef: repo.Forge + ":" + repo.Slug(), TargetKind: in.Command, TargetID: target, TargetRevision: revision, Summary: in.Command, Estimate: "unknown", Payload: string(raw)}, nil
}
func (d factoryFixedAction) Recheck(ctx context.Context, p owneraction.Principal, f owneraction.Facts, at time.Time) error {
	return (OwnerActionDomain{Service: d.service}).Recheck(ctx, p, f, at)
}
func (d factoryFixedAction) Execute(ctx context.Context, p owneraction.Principal, f owneraction.Facts, key string, at time.Time) (owneraction.Result, error) {
	return (OwnerActionDomain{Service: d.service}).Execute(ctx, p, f, key, at)
}
func (d factoryFixedAction) Recover(ctx context.Context, p owneraction.Principal, f owneraction.Facts, key string, at time.Time) (owneraction.Result, bool, error) {
	return (OwnerActionDomain{Service: d.service}).Recover(ctx, p, f, key, at)
}

func executeFactoryAction(t *testing.T, run *factoryConversationRun, payload naturalPayload, at *time.Time) owneraction.Proposal {
	t.Helper()
	ctx := t.Context()
	if _, err := run.s.db.UpsertIdentity(ctx, store.Identity{Forge: "github", Role: store.RoleOwner, UserID: 42, Handle: "owner", Source: store.SourceGH}); err != nil {
		t.Fatal(err)
	}
	owner, err := run.s.db.ConnectorOwner(ctx, 42)
	if err != nil {
		t.Fatal(err)
	}
	principal := owneraction.Principal{OwnerID: "42", OwnerGeneration: owner.Generation, Transport: "discord", ChannelID: "2", DiscordOwnerID: "1"}
	actions := owneraction.New(run.s.db, factoryFixedAction{run.s, payload}, func() time.Time { return *at })
	actions.SetDiscord("1", []string{"2"})
	actions.SetDelivery(gateApproval{})
	run.s.EnableOwnerActions(actions, func(context.Context, string) (owneraction.Principal, error) { return principal, nil })
	proposal, err := actions.Propose(ctx, principal, owneraction.Invocation{Command: payload.Invocation.Command, Arguments: payload.Invocation.Arguments}, "fixed-unit-action")
	if err != nil {
		t.Fatal(err)
	}
	if err = actions.Available(ctx, proposal.ID, "delivered"); err != nil {
		t.Fatal(err)
	}
	*at = at.Add(5 * time.Second)
	run.at = *at
	run.t.Source = "yes"
	run.t.ReplyTo = "delivered"
	if handled, err := run.confirm(ctx); !handled || err != nil {
		t.Fatalf("shared confirmation: %v %v", handled, err)
	}
	if proposal.Tier == owneraction.Binding {
		if _, err = actions.Step(ctx, proposal.ID); err != nil {
			t.Fatal(err)
		}
		if _, err = actions.Reaction(ctx, owneraction.Reaction{ChannelID: "2", MessageID: "approval:" + proposal.ID, UserID: "1", Emoji: "✅"}); err != nil {
			t.Fatal(err)
		}
	}
	proposal, err = actions.Step(ctx, proposal.ID)
	if err != nil {
		t.Fatal(err)
	}
	return proposal
}

func TestFactoryConversationLookupAndCorrectionShareBudget(t *testing.T) {
	provider := &factoryUnitProvider{outputs: []string{`{"version":1,"kind":"lookup","command":"health","arguments":[]}`, `{"version":1,"kind":"lookup","command":"health","arguments":[]}`, `{"version":1,"kind":"execute"}`, `{"version":1,"kind":"lookup","command":"health","arguments":[]}`}}
	run, at := factoryUnitRun(t, provider)
	if err := run.s.runFactoryConversation(t.Context(), run.c, run.b, run.t, at); err != nil {
		t.Fatal(err)
	}
	if provider.calls != 4 {
		t.Fatalf("provider calls=%d", provider.calls)
	}
	exchanges, err := run.s.db.ListConversationExchanges(t.Context(), run.t.ID)
	if err != nil {
		t.Fatal(err)
	}
	evidence := 0
	for _, exchange := range exchanges {
		if exchange.Kind == "evidence" {
			evidence++
		}
	}
	if evidence != 2 {
		t.Fatalf("read was performed after the fourth invocation: evidence=%d", evidence)
	}
	reply, err := run.s.db.ClaimConversationReply(t.Context(), "discord", at)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply.ReplyText, "budget") {
		t.Fatalf("unexpected result %q", reply.ReplyText)
	}
}

func TestFactoryConversationNarrationFailureRetainsOutcome(t *testing.T) {
	provider := &factoryUnitProvider{fail: true}
	run, at := factoryUnitRun(t, provider)
	const saved = "Answer saved for acme/app#42."
	if err := run.narrate(t.Context(), saved); err != nil {
		t.Fatal(err)
	}
	reply, err := run.s.db.ClaimConversationReply(t.Context(), "discord", at)
	if err != nil {
		t.Fatal(err)
	}
	if reply.ReplyText != "Answer saved for acme/app\\#42." {
		t.Fatalf("saved outcome changed: %q", reply.ReplyText)
	}
	if provider.calls != 1 {
		t.Fatalf("uncertain provider input replayed: calls=%d", provider.calls)
	}
}

type factoryUnitDiagnosis struct {
	calls   int
	request DiagnosisRequest
}

func (d *factoryUnitDiagnosis) Queue(_ context.Context, request DiagnosisRequest) error {
	d.calls++
	d.request = request
	return nil
}

func (d *factoryUnitDiagnosis) OwnerActionDiagnosis(ctx context.Context, target, note, key string) (string, error) {
	presentation.Assess(ctx, presentation.Success)
	d.calls++
	d.request = DiagnosisRequest{Target: target, Note: note, SourceID: key}
	return "Findings: the current task is waiting on the owner.", nil
}

func TestFactoryConversationFirstCancellationSkipsProvider(t *testing.T) {
	provider := &factoryUnitProvider{fail: true}
	run, at := factoryUnitRun(t, provider, "cancel")
	if err := run.s.runFactoryConversation(t.Context(), run.c, run.b, run.t, at); err != nil {
		t.Fatal(err)
	}
	if provider.calls != 0 {
		t.Fatalf("cancel invoked provider %d times", provider.calls)
	}
	c, err := run.s.db.ReadConversation(t.Context(), "discord", "2")
	if err != nil {
		t.Fatal(err)
	}
	if c.HealthReason != "" || c.SessionHandle != "" {
		t.Fatalf("cancel paused or created a session: %+v", c)
	}
	reply, err := run.s.db.ClaimConversationReply(t.Context(), "discord", at)
	if err != nil {
		t.Fatal(err)
	}
	if reply.ReplyText == "" || strings.Contains(reply.ReplyText, "Saved action outcome") {
		t.Fatalf("cancel response %q", reply.ReplyText)
	}
}

func TestFactoryConversationDiagnosisSavedOutcome(t *testing.T) {
	provider := &factoryUnitProvider{fail: true}
	run, at := factoryUnitRun(t, provider, "yes")
	diagnosis := &factoryUnitDiagnosis{}
	run.s.EnableResearch(diagnosis, nil)
	repo, err := run.s.db.UpsertRepo(t.Context(), store.Repo{Path: t.TempDir(), Forge: "github", Owner: "acme", Name: "app"})
	if err != nil {
		t.Fatal(err)
	}
	payload := naturalPayload{Invocation: Invocation{Command: "diagnose", Arguments: map[string]string{"project": "acme/app", "issue": "42", "note": "check current state"}}, Channel: "2", Project: "acme/app", RepoID: repo.ID}
	proposal := executeFactoryAction(t, run, payload, &at)
	if proposal.State != "succeeded" || proposal.Result.Summary != "Findings: the current task is waiting on the owner." || diagnosis.calls != 1 || diagnosis.request.Target != "github:acme/app#42" || diagnosis.request.Note != "check current state" || provider.calls != 0 {
		t.Fatalf("saved diagnosis: %+v calls=%d request=%+v", proposal, diagnosis.calls, diagnosis.request)
	}
	if _, err = run.s.ownerActions.Step(t.Context(), proposal.ID); err != nil {
		t.Fatal(err)
	}
	inspected, err := run.s.ownerActions.Inspect(t.Context(), mustFactoryPrincipal(t, run.s), proposal.ID)
	if err != nil || inspected.Result != proposal.Result || diagnosis.calls != 1 {
		t.Fatalf("diagnosis replayed or changed: %+v %v", inspected, err)
	}
	history, err := run.s.ownerActions.History(t.Context(), mustFactoryPrincipal(t, run.s), "", 10)
	if err != nil || len(history.Proposals) != 1 || history.Proposals[0].Result.Summary != proposal.Result.Summary {
		t.Fatalf("history lost diagnosis: %+v %v", history, err)
	}
}

func mustFactoryPrincipal(t *testing.T, s *Service) owneraction.Principal {
	t.Helper()
	p, err := s.ownerPrincipal(t.Context(), "2")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestFactoryConversationJournalProtectsDecodedOutput(t *testing.T) {
	const secret = "private-output-value"
	provider := &factoryUnitProvider{outputs: []string{`{"version":1,"kind":"answer","text":"private-output-value"}`}}
	run, at := factoryUnitRun(t, provider)
	run.s.Protect = func(_ context.Context, text string) (string, error) {
		return strings.ReplaceAll(text, secret, "[redacted]"), nil
	}
	if err := run.s.runFactoryConversation(t.Context(), run.c, run.b, run.t, at); err != nil {
		t.Fatal(err)
	}
	exchanges, err := run.s.db.ListConversationExchanges(t.Context(), run.t.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, exchange := range exchanges {
		if strings.Contains(exchange.Request+exchange.Result, secret) {
			t.Fatal("decoded provider output bypassed journal protection")
		}
	}
	reply, err := run.s.db.ClaimConversationReply(t.Context(), "discord", at)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(reply.ReplyText, `\[redacted\]`) || strings.Contains(reply.ReplyText, "private-token") {
		t.Fatalf("unprotected output %q", reply.ReplyText)
	}
}

type uncertainFactoryConfirmer struct {
	calls int
	err   error
}

func (f *uncertainFactoryConfirmer) Confirm(ctx context.Context, _ string, _ string, _ string, _ string, _ time.Time) (string, error) {
	f.calls++
	if f.err != nil {
		return "", f.err
	}
	presentation.Assess(ctx, presentation.Uncertain)
	return "Issue creation outcome is uncertain.", nil
}
func TestFactoryConversationPreservesUncertainDomainOutcome(t *testing.T) {
	run, at := factoryUnitRun(t, &factoryUnitProvider{fail: true}, "yes")
	confirmer := &uncertainFactoryConfirmer{}
	run.s.EnableFeatures(nil, confirmer)
	repo, err := run.s.db.UpsertRepo(t.Context(), store.Repo{Path: t.TempDir(), Forge: "github", Owner: "acme", Name: "app"})
	if err != nil {
		t.Fatal(err)
	}
	preview := store.ChatIssuePreview{ID: "preview", Transport: "discord", ChannelRef: "2", SourceMessageID: "draft", Code: "12345678", ContentHash: "hash", Title: "Feature", Body: "Exact content", RepoID: repo.ID, CreatedAt: at, ExpiresAt: at.Add(24 * time.Hour)}
	if err = run.s.db.SaveChatIssuePreview(t.Context(), preview); err != nil {
		t.Fatal(err)
	}
	payload := naturalPayload{Invocation: Invocation{Command: "confirm", Arguments: map[string]string{"code": preview.Code}}, Channel: "2", Project: "acme/app", RepoID: repo.ID, PreviewID: preview.ID, PreviewHash: preview.ContentHash, Question: preview.Title + "\n" + preview.Body}
	proposal := executeFactoryAction(t, run, payload, &at)
	if confirmer.calls != 1 || proposal.State != "uncertain" || proposal.Result.Summary != "Issue creation outcome is uncertain." {
		t.Fatalf("uncertainty lost: %+v calls=%d", proposal, confirmer.calls)
	}
	execution, err := run.s.db.ReadNaturalExecution(t.Context(), proposal.ExecutionID)
	if err != nil || execution.State != "uncertain" {
		t.Fatalf("journal uncertainty lost: %+v %v", execution, err)
	}
	if _, err = run.s.ownerActions.Step(t.Context(), proposal.ID); err != nil {
		t.Fatal(err)
	}
	if confirmer.calls != 1 {
		t.Fatal("uncertain effect was replayed")
	}
}

type failedConversationProvider struct{ err error }

func (p failedConversationProvider) RunConversation(context.Context, agent.ConversationRequest) (agent.ConversationResult, error) {
	return agent.ConversationResult{}, p.err
}

func TestConversationFailureDiagnosticsAndRetry(t *testing.T) {
	for _, factory := range []bool{false, true} {
		for _, phase := range []string{"validation", "isolation_probe", "directory_preparation", "before_launch", "launch", "stream", "terminal", "unknown", "busy", "missing", "denied", "temporary_unknown"} {
			t.Run(fmt.Sprintf("factory=%t/%s", factory, phase), func(t *testing.T) {
				run, at := factoryUnitRun(t, &factoryUnitProvider{})
				run.s.conversations.Load().factory = factory
				preStart := phase == "validation" || phase == "isolation_probe" || phase == "directory_preparation" || phase == "before_launch" || phase == "launch"
				cause := errors.New("actionable cause secret-sentinel")
				switch phase {
				case "busy":
					cause = agent.ErrConversationBusy
				case "missing":
					cause = agent.ErrConversationSessionMissing
				case "denied":
					cause = &ConversationAccessError{Permanent: true}
				case "temporary_unknown":
					cause = &ConversationAccessError{}
				}
				err := agent.ConversationError(cause, phase, preStart)
				if phase == "unknown" {
					err = cause
				}
				run.s.conversations.Load().providers["claude"] = failedConversationProvider{err: err}
				var logs bytes.Buffer
				run.s.Logger = slog.New(slog.NewTextHandler(&logs, nil))
				run.s.Protect = func(_ context.Context, text string) (string, error) {
					return strings.ReplaceAll(text, "secret-sentinel", "[redacted]"), nil
				}
				if err := run.s.runConversation(t.Context(), run.c, run.b, run.t, at); err != nil {
					t.Fatal(err)
				}
				expectedPhase := phase
				if factory && phase == "unknown" {
					expectedPhase = "provider"
				}
				for _, want := range []string{"conversation_id=" + run.c.ID, "turn_id=" + run.t.ID, "provider=claude", "phase=" + expectedPhase} {
					if !strings.Contains(logs.String(), want) {
						t.Fatalf("missing %s: %s", want, logs.String())
					}
				}
				if strings.Contains(logs.String(), "secret-sentinel") {
					t.Fatal("secret leaked")
				}
				saved, err := run.s.db.ReadConversation(t.Context(), "discord", "2")
				if err != nil {
					t.Fatal(err)
				}
				if (saved.HealthReason == "") != preStart {
					t.Fatalf("incorrect health: %+v", saved)
				}
				latest, err := run.s.db.LatestConversationTurn(t.Context(), run.c.ID)
				if err != nil || latest.State == "running" || latest.State == "pending" {
					t.Fatalf("failed input not settled: %+v %v", latest, err)
				}
				if worked, err := run.s.ConversationStep(t.Context(), at); worked || err != nil {
					t.Fatalf("failed input retried: %t %v", worked, err)
				}
			})
		}
	}
}

func TestFactoryConversationSavedOutcomeRetainsMissingIdentityPause(t *testing.T) {
	run, at := factoryUnitRun(t, &factoryUnitProvider{})
	run.savedOutcomes = "previous action recorded"
	if err := run.failed(t.Context(), errors.New("unknown process outcome")); err != nil {
		t.Fatal(err)
	}
	c, err := run.s.db.ReadConversation(t.Context(), "discord", "2")
	if err != nil || c.HealthReason == "" {
		t.Fatalf("uncertain continuity released: %+v %v", c, err)
	}
	reply, err := run.s.db.ClaimConversationReply(t.Context(), "discord", at)
	if err != nil || !strings.Contains(reply.ReplyText, run.savedOutcomes) {
		t.Fatalf("saved outcome lost: %+v %v", reply, err)
	}
}

func TestFactoryConversationAccessBoundaryRetry(t *testing.T) {
	for _, failureAt := range []int{1, 2, 3, 4, 5} {
		t.Run(fmt.Sprint(failureAt), func(t *testing.T) {
			p := &factoryUnitProvider{outputs: []string{`{"version":1,"kind":"lookup","command":"health","arguments":[]}`, `{"version":1,"kind":"answer","text":"Current heartbeat unavailable."}`}}
			r, at := factoryUnitRun(t, p)
			checks := 0
			r.s.conversations.Load().check = func(context.Context, string) error {
				checks++
				if checks == failureAt {
					return &ConversationAccessError{RetryAfter: 10 * time.Second}
				}
				return nil
			}
			if err := r.s.runFactoryConversation(t.Context(), r.c, r.b, r.t, at); err != nil {
				t.Fatal(err)
			}
			pending, err := r.s.db.LatestConversationTurn(t.Context(), r.c.ID)
			if err != nil || pending.State != "pending" || pending.ReplyID != "" {
				t.Fatalf("not deferred: %+v %v", pending, err)
			}
			if worked, err := r.s.ConversationStep(t.Context(), at.Add(time.Second)); err != nil || worked {
				t.Fatalf("retried before backoff: %t %v", worked, err)
			}
			if worked, err := r.s.ConversationStep(t.Context(), at.Add(11*time.Second)); err != nil || !worked {
				t.Fatalf("retry failed: %t %v", worked, err)
			}
			finished, err := r.s.db.LatestConversationTurn(t.Context(), r.c.ID)
			if err != nil || finished.ID != pending.ID || finished.State != "completed" || finished.Generation <= pending.Generation {
				t.Fatalf("reclaimed turn: %+v %v", finished, err)
			}
			c, err := r.s.db.ReadConversation(t.Context(), "discord", "2")
			if err != nil || c.SessionHandle != "unit-session" || p.calls != 2 {
				t.Fatalf("native input replayed: %+v calls=%d %v", c, p.calls, err)
			}
			exchanges, err := r.s.db.ListConversationExchanges(t.Context(), r.t.ID)
			if err != nil {
				t.Fatal(err)
			}
			if len(exchanges) != 5 {
				t.Fatalf("unexpected journal: %+v", exchanges)
			}
			for i, x := range exchanges {
				if x.Ordinal != i+1 {
					t.Fatalf("ordinal: %+v", x)
				}
			}
		})
	}
}

func TestFactoryConversationCorrectionAccessRetry(t *testing.T) {
	p := &factoryUnitProvider{outputs: []string{`{"version":1,"kind":"execute"}`, `{"version":1,"kind":"clarify","text":"Which task?"}`}}
	r, at := factoryUnitRun(t, p)
	checks := 0
	r.s.conversations.Load().check = func(context.Context, string) error {
		checks++
		if checks == 3 {
			return &ConversationAccessError{}
		}
		return nil
	}
	if err := r.s.runFactoryConversation(t.Context(), r.c, r.b, r.t, at); err != nil {
		t.Fatal(err)
	}
	if worked, err := r.s.ConversationStep(t.Context(), at.Add(time.Minute)); err != nil || !worked {
		t.Fatalf("retry: %t %v", worked, err)
	}
	capture, err := r.s.restoreConversationInstructions(t.Context(), r.c.ID, r.at)
	if err != nil {
		t.Fatal(err)
	}
	if p.calls != 2 {
		t.Fatalf("correction replay: %+v", p)
	}
	var replayed factoryConversationPrompt
	if err := json.Unmarshal([]byte(p.prompts[1]), &replayed); err != nil || replayed.Correction == "" || replayed.Correction != capture.Correction() {
		t.Fatalf("correction replay: %+v %v", p, err)
	}
}

func TestFactoryConversationReportedDiagnosticsCorrection(t *testing.T) {
	for _, bad := range []string{
		`{"version":1,"kind":"answer","text":"Source: factory:status; target: #614. Stage: waiting (succeeded); story-001: acceptance. Evidence limited. Your question is still unanswered."}`,
		`{"version":1,"kind":"answer","blocks":[{"fact_refs":["missing-fact-5"]}]}`,
		`{"version":1,"kind":"answer","text":"The meter output: lower bound $7.89; collection partial, helper overhead excluded; page 1 of 3."}`,
	} {
		for _, correct := range []bool{false, true} {
			good := `{"version":1,"kind":"answer","text":"Planning is done: both stories are at acceptance review. Nobody is recorded as waiting. Implementation hasn't started."}`
			if !correct {
				good = bad
			}
			p := &factoryUnitProvider{outputs: []string{bad, good}}
			r, at := factoryUnitRun(t, p)
			if err := r.s.runFactoryConversation(t.Context(), r.c, r.b, r.t, at); err != nil {
				t.Fatal(err)
			}
			expectedCalls := 2
			if !correct {
				expectedCalls = 3
			}
			if p.calls != expectedCalls {
				t.Fatalf("correction calls: %d", p.calls)
			}
			reply, err := r.s.db.ClaimConversationReply(t.Context(), "discord", at)
			if err != nil {
				t.Fatal(err)
			}
			if presentation.ConversationDiagnostics(reply.ReplyText) {
				t.Fatalf("leaked: %q", reply.ReplyText)
			}
			if correct && !strings.Contains(reply.ReplyText, "Planning is done") {
				t.Fatal(reply.ReplyText)
			}
			if !correct && !strings.Contains(reply.ReplyText, "format check") {
				t.Fatal(reply.ReplyText)
			}
			var corrected factoryConversationPrompt
			if err := json.Unmarshal([]byte(p.prompts[1]), &corrected); err != nil || corrected.Correction == "" || corrected.CorrectionReason == "" {
				t.Fatalf("correction carries no reason: %+v %v", corrected, err)
			}
		}
	}
}

// The grounded answer the bot gave on 2026-10-21 for "where is the
// implementation for 728 at?" was rejected twice because it said "recent runs
// (8, 9 and 10)". Plain wording around run numbers is accepted on first try.
func TestFactoryConversationAcceptsPlainStatusAnswer(t *testing.T) {
	answer := `{"version":1,"kind":"answer","blocks":[{"text":"I'm assuming litefactory for #728. It is marked as failed, and it is in implement mode, at a stage that is still listed as running. It has eleven stories. Nothing is recorded as waiting on you or anyone else."},{"text":"The three most recent runs (8, 9 and 10) were all planning runs, and all three succeeded."}]}`
	p := &factoryUnitProvider{outputs: []string{answer}}
	r, at := factoryUnitRun(t, p)
	if err := r.s.runFactoryConversation(t.Context(), r.c, r.b, r.t, at); err != nil {
		t.Fatal(err)
	}
	reply, err := r.s.db.ClaimConversationReply(t.Context(), "discord", at)
	if err != nil {
		t.Fatal(err)
	}
	if p.calls != 1 || !strings.Contains(reply.ReplyText, "assuming litefactory") {
		t.Fatalf("calls=%d reply=%q", p.calls, reply.ReplyText)
	}
}

// Noise around the single protocol object, like the stray tool tag and prose
// the bot emitted on 2026-10-21, no longer costs the correction.
func TestFactoryConversationToleratesNoiseAroundObject(t *testing.T) {
	noisy := "<invoke name=\"x\">\n</invoke>\n```\n\nLet me check first.\n\n{\"version\":1,\"kind\":\"answer\",\"text\":\"Nothing is running right now.\"}"
	p := &factoryUnitProvider{outputs: []string{noisy}}
	r, at := factoryUnitRun(t, p)
	if err := r.s.runFactoryConversation(t.Context(), r.c, r.b, r.t, at); err != nil {
		t.Fatal(err)
	}
	reply, err := r.s.db.ClaimConversationReply(t.Context(), "discord", at)
	if err != nil {
		t.Fatal(err)
	}
	if p.calls != 1 || !strings.Contains(reply.ReplyText, "Nothing is running right now.") || strings.Contains(reply.ReplyText, "invoke") {
		t.Fatalf("calls=%d reply=%q", p.calls, reply.ReplyText)
	}
}

func TestConversationPausedGuidanceUsesRetainedIdentityAndOperatorLogs(t *testing.T) {
	run, at := factoryUnitRun(t, &factoryUnitProvider{})
	if err := run.s.conversationUnavailable(t.Context(), run.c, run.t, "saved native session missing; no replacement session created", at); err != nil {
		t.Fatal(err)
	}
	latest, err := run.s.db.LatestConversationTurn(t.Context(), run.c.ID)
	if err != nil {
		t.Fatal(err)
	}
	// The complete retained presentation and the authorized health view must both
	// carry the usable local inspection step; paused input is never retried here.
	reply, err := run.s.db.ReadChatReply(t.Context(), "discord", latest.ReplyID)
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{reply.ReplyText, run.s.conversationHealth(t.Context(), at)} {
		if !strings.Contains(text, "factory operator") || !strings.Contains(text, "litefactory logs --all --grep") || !strings.Contains(text, run.c.ID) {
			t.Fatal(text)
		}
	}
	if worked, err := run.s.ConversationStep(t.Context(), at); worked || err != nil {
		t.Fatalf("paused work retried: %t %v", worked, err)
	}
}

func TestFactoryConversationUnknownFailureKeepsOneInspectionStep(t *testing.T) {
	provider := &factoryUnitProvider{}
	run, at := factoryUnitRun(t, provider)
	run.c.SessionHandle = "retained-provider-session"
	if err := run.failed(t.Context(), errors.New("provider completion unknown")); err != nil {
		t.Fatal(err)
	}
	reply, err := run.s.db.ClaimConversationReply(t.Context(), "discord", at)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply.ReplyText, "before a completed answer was saved") || !strings.Contains(reply.ReplyText, "will not be replayed") || strings.Count(reply.ReplyText, "Ask here for the status") != 1 || strings.Contains(reply.ReplyText, "Please ask again") || provider.calls != 0 {
		t.Fatalf("unknown completion lost safe inspection: calls=%d text=%s", provider.calls, reply.ReplyText)
	}
}

// freshProvider records each native request and fails the calls named in fail.
type freshProvider struct {
	requests []agent.ConversationRequest
	fail     map[int]error
}

func (p *freshProvider) RunConversation(ctx context.Context, r agent.ConversationRequest) (agent.ConversationResult, error) {
	if err := r.BeforeLaunch(ctx); err != nil {
		return agent.ConversationResult{}, agent.ConversationError(err, "before_launch", true)
	}
	p.requests = append(p.requests, r)
	session := r.Session
	if session.Resume == "" {
		session.Resume = session.Ref
	}
	if err := r.OnSession(ctx, session); err != nil {
		return agent.ConversationResult{}, err
	}
	if err := p.fail[len(p.requests)]; err != nil {
		return agent.ConversationResult{}, err
	}
	return agent.ConversationResult{Session: session, Answer: []byte(`{"version":1,"kind":"answer","text":"Nothing is running right now."}`)}, nil
}

// providerCause mirrors the harness: a category joined with the bounded provider text.
func providerCause(category error, text string) error {
	return agent.ConversationError(errors.Join(category, fmt.Errorf("conversation did not complete the recorded native session: provider error: %s", text)), "terminal", false)
}

const refusedProviderText = "API Error: Claude's safeguards flagged this message (aup). secret-sentinel Details: [reasoning_extraction]"

// runFreshTurn admits, runs and delivers one owner message on either path.
func runFreshTurn(t *testing.T, run *factoryConversationRun, source, text string, at time.Time) string {
	t.Helper()
	ctx := t.Context()
	if source != "101" {
		scan, err := run.s.db.ReadNaturalScan(ctx, "2")
		if err != nil {
			t.Fatal(err)
		}
		if err = run.s.db.StageNaturalScan(ctx, scan, store.NaturalScan{ChannelRef: "2", Cursor: scan.Cursor, HighWater: source}, []store.NaturalMessage{{ChannelRef: "2", SourceID: source, OwnerID: "1", Text: text, CreatedAt: at}}, true); err != nil {
			t.Fatal(err)
		}
		instructions, err := run.s.restoreConversationInstructions(ctx, run.c.ID, at)
		if err != nil {
			t.Fatal(err)
		}
		prompt, err := agent.ConversationPrompt(instructions.Native(), text, "", "")
		if err != nil {
			t.Fatal(err)
		}
		if _, err = run.s.db.AdmitConversationTurn(ctx, run.c.ID, source, prompt, at); err != nil {
			t.Fatal(err)
		}
		if run.t, err = run.s.db.ClaimConversationTurn(ctx, run.c.ID, "unit-worker", at, time.Minute); err != nil {
			t.Fatal(err)
		}
		if run.c, err = run.s.db.ReadConversation(ctx, "discord", "2"); err != nil {
			t.Fatal(err)
		}
	}
	if err := run.s.runConversation(ctx, run.c, run.b, run.t, at); err != nil {
		t.Fatal(err)
	}
	var replies []string
	for {
		reply, err := run.s.db.ClaimConversationReply(ctx, "discord", at)
		if errors.Is(err, store.ErrNotFound) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if err = run.s.db.FinishConversationReply(ctx, reply, "delivered-"+reply.MessageID, at); err != nil {
			t.Fatal(err)
		}
		replies = append(replies, reply.ReplyText)
	}
	return strings.Join(replies, "\n")
}

func TestConversationProviderRefusalAndLoginReplies(t *testing.T) {
	for _, factory := range []bool{false, true} {
		for name, category := range map[string]error{"refused": agent.ErrConversationRefused, "authentication": agent.ErrConversationAuthentication} {
			t.Run(fmt.Sprintf("factory=%t/%s", factory, name), func(t *testing.T) {
				text := refusedProviderText
				if name == "authentication" {
					text = "Failed to authenticate: OAuth session expired secret-sentinel"
				}
				provider := &freshProvider{fail: map[int]error{1: providerCause(category, text)}}
				run, at := factoryUnitRun(t, &factoryUnitProvider{})
				run.s.conversations.Load().factory = factory
				run.s.conversations.Load().providers["claude"] = provider
				var logs bytes.Buffer
				run.s.Logger = slog.New(slog.NewTextHandler(&logs, nil))
				run.s.Protect = func(_ context.Context, text string) (string, error) {
					return strings.ReplaceAll(text, "secret-sentinel", "[redacted]"), nil
				}
				reply := runFreshTurn(t, run, "101", "", at)
				if !strings.Contains(logs.String(), "conversation turn failed") || !strings.Contains(logs.String(), category.Error()) || !strings.Contains(logs.String(), strings.SplitN(text, " secret", 2)[0][:20]) || strings.Contains(logs.String(), "secret-sentinel") {
					t.Fatalf("log lost category or provider text, or leaked: %s", logs.String())
				}
				if strings.Contains(reply, "model provider failed") {
					t.Fatalf("generic reply: %s", reply)
				}
				if name == "refused" && (!strings.Contains(reply, "safety filter") || !strings.Contains(reply, "new conversation")) {
					t.Fatalf("refusal reply lacks rephrase hint or command: %s", reply)
				}
				if name == "authentication" && !strings.Contains(strings.ToLower(reply), "sign in") {
					t.Fatalf("login failure not reported: %s", reply)
				}
				saved, err := run.s.db.ReadConversation(t.Context(), "discord", "2")
				if err != nil || saved.HealthReason != "" {
					t.Fatalf("conversation paused: %+v %v", saved, err)
				}
			})
		}
	}
}

func TestConversationFreshCommandStartsNewNativeSession(t *testing.T) {
	for _, factory := range []bool{false, true} {
		for _, history := range []bool{true, false} {
			t.Run(fmt.Sprintf("factory=%t/history=%t", factory, history), func(t *testing.T) {
				first := "How is the factory?"
				refusedText := "what did you find in the thinking tokens?"
				input := first
				if !history {
					input = refusedText
				}
				provider := &freshProvider{fail: map[int]error{}}
				run, at := factoryUnitRun(t, &factoryUnitProvider{}, input)
				run.s.conversations.Load().factory = factory
				run.s.conversations.Load().providers["claude"] = provider
				source := 101
				if history {
					runFreshTurn(t, run, "101", "", at)
					source++
					provider.fail[2] = providerCause(agent.ErrConversationRefused, refusedProviderText)
					runFreshTurn(t, run, fmt.Sprint(source), refusedText, at)
				} else {
					provider.fail[1] = providerCause(agent.ErrConversationRefused, refusedProviderText)
					runFreshTurn(t, run, "101", "", at)
				}
				before, err := run.s.db.ReadConversation(t.Context(), "discord", "2")
				if err != nil || before.SessionHandle == "" {
					t.Fatalf("refused turn left no native session: %+v %v", before, err)
				}
				oldRef := provider.requests[len(provider.requests)-1].Session.Ref
				calls := len(provider.requests)
				source++
				reply := runFreshTurn(t, run, fmt.Sprint(source), " \"New Conversation\" ", at)
				if len(provider.requests) != calls {
					t.Fatal("fresh command launched the provider")
				}
				if !strings.Contains(strings.ToLower(reply), "fresh") || !strings.Contains(reply, "work records") {
					t.Fatalf("fresh reply: %s", reply)
				}
				after, err := run.s.db.ReadConversation(t.Context(), "discord", "2")
				if err != nil || after.SessionHandle != "" || after.FreshAfter == 0 || after.HealthReason != "" {
					t.Fatalf("fresh transition not saved: %+v %v", after, err)
				}
				source++
				reply = runFreshTurn(t, run, fmt.Sprint(source), "Please check again.", at)
				if !strings.Contains(reply, "Nothing is running right now.") {
					t.Fatalf("follow-up unanswered: %s", reply)
				}
				next := provider.requests[len(provider.requests)-1]
				if next.Session.Resume != "" || next.Session.Ref == oldRef {
					t.Fatalf("follow-up resumed the old session: %+v old=%s", next.Session, oldRef)
				}
				if factory {
					var prompt factoryConversationPrompt
					if err := json.Unmarshal([]byte(next.Prompt), &prompt); err != nil {
						t.Fatal(err)
					}
					for _, source := range prompt.OwnerSources {
						if source.Text != "Please check again." {
							t.Fatalf("earlier message resent: %+v", prompt.OwnerSources)
						}
					}
				}
				saved, err := run.s.db.ReadConversation(t.Context(), "discord", "2")
				if err != nil || saved.SessionHandle != next.Session.Ref {
					t.Fatalf("new native identity not saved: %+v %v", saved, err)
				}
			})
		}
	}
}
