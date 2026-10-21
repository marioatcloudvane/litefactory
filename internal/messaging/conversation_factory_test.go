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
	if !strings.HasSuffix(reply.ReplyText, "Saved action outcome:\nAnswer saved for acme/app\\#42.") || strings.Contains(reply.ReplyText, "Unverified") {
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
	if !strings.HasSuffix(reply.ReplyText, "Cancelled the pending request.") {
		t.Fatalf("cancel response %q", reply.ReplyText)
	}
}

func TestFactoryConversationAsyncDiagnosisSavedOutcome(t *testing.T) {
	const code = "1234567890abcdef"
	provider := &factoryUnitProvider{outputs: []string{`{"version":1,"kind":"answer","text":"The saved diagnosis is ready."}`}}
	run, at := factoryUnitRun(t, provider, code)
	diagnosis := &factoryUnitDiagnosis{}
	run.s.EnableResearch(diagnosis, nil)
	repo, err := run.s.db.UpsertRepo(t.Context(), store.Repo{Path: t.TempDir(), Forge: "github", Owner: "acme", Name: "app"})
	if err != nil {
		t.Fatal(err)
	}
	payload := naturalPayload{Invocation: Invocation{Command: "diagnose", Arguments: map[string]string{"project": "acme/app", "issue": "42", "note": "check current state"}}, Channel: "2", Project: "acme/app", RepoID: repo.ID}
	encoded, digest, err := naturalDigest(payload)
	if err != nil {
		t.Fatal(err)
	}
	dialogue := store.NaturalDialogue{Transport: "discord", OwnerID: "1", ChannelRef: "2", Revision: 1, State: "awaiting_confirmation", SourceID: "100", Payload: encoded, Digest: digest, Code: code, ExpiresAt: at.Add(time.Minute), UpdatedAt: at}
	if err = run.s.db.SaveNaturalDialogue(t.Context(), dialogue, 0); err != nil {
		t.Fatal(err)
	}
	if err = run.s.db.BindNaturalPrompt(t.Context(), dialogue, "delivered-proposal"); err != nil {
		t.Fatal(err)
	}
	// The proposed action was generated in this already established native session.
	if err = run.s.db.SaveConversationSession(t.Context(), run.t, "unit-session"); err != nil {
		t.Fatal(err)
	}
	run.c.SessionHandle = "unit-session"
	handled, err := run.confirm(t.Context())
	if err != nil || !handled {
		t.Fatalf("confirmation handled=%v err=%v", handled, err)
	}
	if diagnosis.calls != 1 || diagnosis.request.Target != "github:acme/app#42" {
		t.Fatalf("queue calls=%d request=%+v", diagnosis.calls, diagnosis.request)
	}
	if provider.calls != 0 {
		t.Fatal("queued action made an unnecessary model call")
	}
	action, err := run.s.db.ClaimChatDiagnosis(t.Context(), "discord", at)
	if err != nil {
		t.Fatal(err)
	}
	if action.State != "working" || action.ConversationTurnID != run.t.ID {
		t.Fatalf("async row %+v", action)
	}
	note, err := run.s.ConversationDiagnosisInput(t.Context(), action, at)
	if err != nil || note != "check current state" {
		t.Fatalf("saved exact diagnosis input %q %v", note, err)
	}

	queued, err := run.s.db.ClaimConversationReply(t.Context(), "discord", at)
	if err != nil {
		t.Fatal(err)
	}
	if err = run.s.db.FinishConversationReply(t.Context(), queued, "queue-notice", at); err != nil {
		t.Fatal(err)
	}
	const saved = "Findings: the current task is waiting on the owner."
	if err = run.s.db.CompleteChatDiagnosisResult(t.Context(), "discord", action.MessageID, action.AnswerAttempts, saved, at, "success"); err != nil {
		t.Fatal(err)
	}
	if _, err = run.s.db.ClaimChatReply(t.Context(), "discord", at); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("ordinary delivery claimed diagnosis: %v", err)
	}
	scan, err := run.s.db.ReadNaturalScan(t.Context(), "2")
	if err != nil {
		t.Fatal(err)
	}
	if err = run.s.db.StageNaturalScan(t.Context(), scan, store.NaturalScan{ChannelRef: "2", Cursor: scan.Cursor, HighWater: "102"}, []store.NaturalMessage{{ChannelRef: "2", SourceID: "102", OwnerID: "1", Text: "What did it find?", CreatedAt: at}}, true); err != nil {
		t.Fatal(err)
	}
	if _, err = run.s.db.AdmitConversationTurn(t.Context(), run.c.ID, "102", "protected followup", at); err != nil {
		t.Fatal(err)
	}
	next, err := run.s.db.ClaimConversationTurn(t.Context(), run.c.ID, "unit-worker", at, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err = run.s.runFactoryConversation(t.Context(), run.c, run.b, next, at); err != nil {
		t.Fatal(err)
	}
	if provider.calls != 1 || !strings.Contains(provider.prompts[0], saved) || diagnosis.calls != 1 {
		t.Fatalf("same-session handoff calls=%d queued=%d", provider.calls, diagnosis.calls)
	}
	execution, err := run.s.db.ReadNaturalExecution(t.Context(), action.MessageID)
	if err != nil {
		t.Fatal(err)
	}
	if execution.State != "finished" || execution.Outcome != saved {
		t.Fatalf("execution %+v", execution)
	}
	link, err := run.s.db.GetConversationActionLink(t.Context(), run.t.ID)
	if err != nil {
		t.Fatal(err)
	}
	if link.Outcome != saved {
		t.Fatalf("saved link %+v", link)
	}
	pending, err := run.s.db.ListPendingConversationActionOutcomes(t.Context(), run.c.ID)
	if err != nil || len(pending) != 0 {
		t.Fatalf("outcome eligible again: %+v %v", pending, err)
	}
	result, err := run.s.db.ClaimConversationReply(t.Context(), "discord", at)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.ReplyText, saved) {
		t.Fatalf("saved outcome absent %q", result.ReplyText)
	}
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

type uncertainFactoryConfirmer struct{ calls int }

func (f *uncertainFactoryConfirmer) Confirm(ctx context.Context, _ string, _ string, _ string, _ string, _ time.Time) (string, error) {
	f.calls++
	presentation.Assess(ctx, presentation.Uncertain)
	return "Issue creation outcome is uncertain.", nil
}
func TestFactoryConversationPreservesUncertainDomainOutcome(t *testing.T) {
	const code = "1234567890abcdef"
	run, at := factoryUnitRun(t, &factoryUnitProvider{fail: true}, code)
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
	raw, digest, err := naturalDigest(payload)
	if err != nil {
		t.Fatal(err)
	}
	d := store.NaturalDialogue{Transport: "discord", OwnerID: "1", ChannelRef: "2", Revision: 1, State: "awaiting_confirmation", SourceID: "100", Payload: raw, Digest: digest, Code: code, ExpiresAt: at.Add(time.Minute), UpdatedAt: at}
	if err = run.s.db.SaveNaturalDialogue(t.Context(), d, 0); err != nil {
		t.Fatal(err)
	}
	if err = run.s.db.BindNaturalPrompt(t.Context(), d, "delivered"); err != nil {
		t.Fatal(err)
	}
	if err = run.s.db.SaveConversationSession(t.Context(), run.t, "unit-session"); err != nil {
		t.Fatal(err)
	}
	run.c.SessionHandle = "unit-session"
	handled, err := run.confirm(t.Context())
	if err != nil || !handled {
		t.Fatalf("confirm %v %v", handled, err)
	}
	execution, err := run.s.db.ReadNaturalExecution(t.Context(), "nl-exec:"+code)
	if err != nil {
		t.Fatal(err)
	}
	if confirmer.calls != 1 || execution.State != "uncertain" || execution.Outcome != "Issue creation outcome is uncertain." {
		t.Fatalf("uncertainty lost: %+v calls=%d", execution, confirmer.calls)
	}
	reply, err := run.s.db.ReadChatReply(t.Context(), "discord", execution.Identity)
	if err != nil {
		t.Fatal(err)
	}
	if reply.OutcomeStatus != string(presentation.Uncertain) {
		t.Fatalf("saved status %q", reply.OutcomeStatus)
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
			if p.calls != 2 {
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
