package messaging_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/messaging"
	"github.com/cloudvane/litefactory/internal/messaging/feature"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
)

// External interpretation is fake; admission, dialogue, existing diagnose handler,
// outcome ownership and delivery all use production services and a real SQLite DB.
type journeyInterpreter struct{ calls int }

func (p *journeyInterpreter) Interpret(_ context.Context, r messaging.InterpretationRequest) ([]byte, error) {
	p.calls++
	if len(r.Turns) == 1 {
		return json.Marshal(messaging.Interpretation{Version: 1, Kind: "clarify", Question: "Which project and issue should I diagnose?"})
	}
	var args []messaging.SourceValue
	for _, field := range []struct{ name, value string }{{"project", "acme/app"}, {"issue", "42"}, {"note", "keep the exact note"}} {
		found := false
		for _, turn := range r.Turns {
			if start := strings.Index(turn.Text, field.value); start >= 0 {
				args = append(args, messaging.SourceValue{Name: field.name, SourceID: turn.SourceID, Start: start, End: start + len(field.value)})
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("missing owner source for %s", field.name)
		}
	}
	return json.Marshal(messaging.Interpretation{Version: 1, Kind: "propose", Command: "diagnose", Arguments: args})
}

type journeyDiagnosis struct {
	db      *store.DB
	calls   int
	request messaging.DiagnosisRequest
	at      time.Time
}

func (p *journeyDiagnosis) Queue(ctx context.Context, r messaging.DiagnosisRequest) error {
	p.calls++
	p.request = r
	id := strings.TrimPrefix(r.SourceID, "chat:discord:")
	return p.db.CompleteChatDiagnosisResult(ctx, "discord", id, 0, "Diagnosis finished for acme/app issue 42.", p.at, "success")
}

type journeySender struct{ texts []string }

func (p *journeySender) Deliver(_ context.Context, _ messaging.Target, text string) messaging.Delivery {
	p.texts = append(p.texts, text)
	return messaging.Delivery{Confirmed: true, PromptID: fmt.Sprintf("%d", 900+len(p.texts))}
}

func TestNaturalOwnerJourney(t *testing.T) {
	db := database(t)
	ctx := t.Context()
	at := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	if _, err := db.UpsertRepo(ctx, store.Repo{Path: t.TempDir(), Forge: "github", Owner: "acme", Name: "app", BaseBranch: "main"}); err != nil {
		t.Fatal(err)
	}
	interpreter := &journeyInterpreter{}
	diagnosis := &journeyDiagnosis{db: db, at: at}
	send := &journeySender{}
	newService := func() *messaging.Service {
		s := messaging.New(db, "discord", "7", []string{"3"}, send, messaging.FactoryStatus{DB: db})
		s.EnableResearch(diagnosis, nil)
		s.EnableNatural(interpreter)
		return s
	}
	service := newService()
	if err := db.ActivateNaturalScan(ctx, "3", "100"); err != nil {
		t.Fatal(err)
	}
	reader := &journeyReader{}
	intake := &messaging.Intake{DB: db, Reader: reader, Transport: "discord", Owner: "7", Channels: []string{"3"}, Protect: func(_ context.Context, text string) (string, error) { return text, nil }}
	admit := func(id, text, reply string) {
		t.Helper()
		reader.messages = []store.NaturalMessage{{ChannelRef: "3", SourceID: id, OwnerID: "7", ReplyTo: reply, Text: text, CreatedAt: at}}
		if err := intake.Poll(ctx, at); err != nil {
			t.Fatal(err)
		}
	}

	drain := func() {
		t.Helper()
		for range 30 {
			natural, err := service.NaturalStep(ctx, at)
			if err != nil {
				t.Fatal(err)
			}
			shared, err := service.Step(ctx, at)
			if err != nil {
				t.Fatal(err)
			}
			if !natural && !shared {
				return
			}
		}
		t.Fatal("journey failed to settle")
	}
	admit("101", "Please diagnose; keep the exact note", "")
	drain()
	if diagnosis.calls != 0 || len(send.texts) != 1 || !strings.Contains(send.texts[0], "Which project") {
		t.Fatalf("clarification ran an action or lost question: calls=%d replies=%v", diagnosis.calls, send.texts)
	}
	admit("102", "acme/app issue 42", "")
	drain()
	proposal, err := db.ReadNaturalDialogue(ctx, "discord", "7", "3")
	if err != nil {
		t.Fatal(err)
	}
	if proposal.State != "awaiting_confirmation" || proposal.PromptID == "" || diagnosis.calls != 0 {
		t.Fatalf("undelivered or prematurely executed proposal: %+v calls=%d", proposal, diagnosis.calls)
	}
	displayed := strings.Join(send.texts, "\n")
	for _, exact := range []string{"acme/app", "42", "keep the exact note", proposal.Code} {
		if !strings.Contains(displayed, exact) {
			t.Fatalf("proposal omitted %q: %s", exact, displayed)
		}
	}
	// Recreate the service before confirming: only durable delivered bytes grant authority.
	service = newService()
	admit("103", "yes", proposal.PromptID)
	drain()
	if diagnosis.calls != 1 || diagnosis.request.Target != "github:acme/app#42" || diagnosis.request.Note != "keep the exact note" {
		t.Fatalf("executed different proposal: calls=%d request=%+v", diagnosis.calls, diagnosis.request)
	}
	executionID := strings.TrimPrefix(diagnosis.request.SourceID, "chat:discord:")
	saved, err := db.ReadChatReply(ctx, "discord", executionID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.State != "confirmed" {
		t.Fatalf("outcome not persisted: %+v", saved)
	}
	execution, err := db.ReadNaturalExecution(ctx, executionID)
	if err != nil || execution.State != "finished" || !strings.Contains(execution.Outcome, "Diagnosis finished") {
		t.Fatalf("saved execution outcome: %+v, %v", execution, err)
	}
	if !strings.Contains(strings.Join(send.texts, "\n"), "Diagnosis finished for acme/app issue 42.") {
		t.Fatalf("outcome missing from shared replies: %v", send.texts)
	}
	service = newService()
	admit("104", "confirm "+proposal.Code, "")
	drain()
	if diagnosis.calls != 1 {
		t.Fatalf("duplicate confirmation repeated action %d times", diagnosis.calls)
	}
	done, err := db.ReadNaturalDialogue(ctx, "discord", "7", "3")
	if err != nil {
		t.Fatal(err)
	}
	if done.Payload != "" && done.Payload != "{}" {
		t.Fatalf("terminal dialogue retained executable text: %+v", done)
	}
	// Existing slash reads remain available without natural-language confirmation.
	allowed, inserted, err := service.Admit(ctx, messaging.Message{Transport: "discord", ID: "slash-help", Channel: "3", Sender: "7", Command: "help"}, at)
	if err != nil || !allowed || !inserted {
		t.Fatalf("slash compatibility: %t %t %v", allowed, inserted, err)
	}
	drain()
	if !strings.Contains(send.texts[len(send.texts)-1], "/diagnose") {
		t.Fatalf("slash help unavailable: %v", send.texts)
	}
}

// A newest-first external page exercises the production ordered collector.
type journeyReader struct{ messages []store.NaturalMessage }

func (*journeyReader) CheckIntakeCapability(context.Context, string) error { return nil }
func (r *journeyReader) ReadIntakePage(_ context.Context, channel, owner, before string) (messaging.IntakePage, error) {
	if before != "" || len(r.messages) == 0 {
		return messaging.IntakePage{}, nil
	}
	return messaging.IntakePage{Messages: r.messages, HighID: r.messages[0].SourceID, LowID: r.messages[len(r.messages)-1].SourceID, Count: len(r.messages)}, nil
}

type mutationInterpreter struct {
	command string
	values  map[string]string
}

func (p mutationInterpreter) Interpret(_ context.Context, r messaging.InterpretationRequest) ([]byte, error) {
	var spans []messaging.SourceValue
	for name, value := range p.values {
		found := false
		for _, turn := range r.Turns {
			if start := strings.Index(turn.Text, value); start >= 0 {
				spans = append(spans, messaging.SourceValue{Name: name, SourceID: turn.SourceID, Start: start, End: start + len(value)})
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("source missing for %s", name)
		}
	}
	return json.Marshal(messaging.Interpretation{Version: 1, Kind: "propose", Command: p.command, Arguments: spans})
}

type mutationEffects struct{ calls int }

func (p *mutationEffects) Answer(_ context.Context, _ string, _ string, _ string, _ time.Time) (messaging.Question, error) {
	p.calls++
	return messaging.Question{Reason: "answer recorded"}, nil
}
func (*mutationEffects) Current(context.Context, string) (messaging.Question, error) {
	return messaging.Question{Text: "Which output?", Token: "123456789abc"}, nil
}
func (p *mutationEffects) Confirm(_ context.Context, _ string, _ string, _ string, _ string, _ time.Time) (string, error) {
	p.calls++
	return "Created https://github.com/acme/app/issues/43", nil
}

type mutationDraftModel struct{ calls int }

func (p *mutationDraftModel) Answer(context.Context, int64, string) ([]byte, error) {
	p.calls++
	return json.Marshal(feature.Draft{Title: "Export report", Summary: "Export the report", Problem: "Manual copying", Wanted: "A downloadable file"})
}

func TestNaturalMutationProposalsAndCorrection(t *testing.T) {
	for _, command := range []string{"draft", "answer", "confirm"} {
		t.Run(command, func(t *testing.T) {
			db := database(t)
			ctx := t.Context()
			at := time.Date(2026, 10, 11, 13, 0, 0, 0, time.UTC)
			repo, err := db.UpsertRepo(ctx, store.Repo{Path: t.TempDir(), Forge: "github", Owner: "acme", Name: "app", BaseBranch: "main"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = db.GetOrCreateSession(ctx, repo, 42); err != nil {
				t.Fatal(err)
			}
			effects := &mutationEffects{}
			model := &mutationDraftModel{}
			send := &journeySender{}
			service := messaging.New(db, "discord", "7", []string{"3"}, send, messaging.FactoryStatus{DB: db})
			service.EnableResearch(nil, effects)
			service.EnableFeatures(&feature.Drafter{Repositories: db, Model: model, Store: db, Redactor: redact.New(redact.Options{Environ: []string{}}), Now: func() time.Time { return at }}, effects)
			values := map[string]string{"project": "acme/app", "note": "Export report"}
			if command == "answer" {
				values = map[string]string{"project": "acme/app", "issue": "42", "revision": "123456789abc", "text": "CSV output"}
			}
			if command == "confirm" {
				values = map[string]string{"code": "1234abcd"}
				preview := store.ChatIssuePreview{ID: "natural-preview", Transport: "discord", ChannelRef: "3", SourceMessageID: "earlier", Code: "1234abcd", Title: "Export report", Body: "# Feature request\nDownload CSV output.", State: "previewed", RepoID: repo.ID, CreatedAt: at, ExpiresAt: at.Add(24 * time.Hour)}
				preview.ContentHash = feature.Hash(preview.Title, preview.Body)
				if err = db.SaveChatIssuePreview(ctx, preview); err != nil {
					t.Fatal(err)
				}
			}
			service.EnableNatural(mutationInterpreter{command: command, values: values})
			if err = db.ActivateNaturalScan(ctx, "3", "100"); err != nil {
				t.Fatal(err)
			}
			reader := &journeyReader{}
			intake := &messaging.Intake{DB: db, Reader: reader, Transport: "discord", Owner: "7", Channels: []string{"3"}, Protect: func(_ context.Context, text string) (string, error) { return text, nil }}
			drain := func() {
				t.Helper()
				for range 30 {
					a, e := service.NaturalStep(ctx, at)
					if e != nil {
						t.Fatal(e)
					}
					b, e := service.Step(ctx, at)
					if e != nil {
						t.Fatal(e)
					}
					if !a && !b {
						return
					}
				}
				t.Fatal("did not settle")
			}
			admit := func(id, text, reply string) {
				t.Helper()
				reader.messages = []store.NaturalMessage{{ChannelRef: "3", SourceID: id, OwnerID: "7", Text: text, ReplyTo: reply, CreatedAt: at}}
				if e := intake.Poll(ctx, at); e != nil {
					t.Fatal(e)
				}
				drain()
			}
			var content []string
			for _, value := range values {
				content = append(content, value)
			}
			request := "Please perform " + command + " with " + strings.Join(content, " ")
			admit("101", request, "")
			first, err := db.ReadNaturalDialogue(ctx, "discord", "7", "3")
			if err != nil {
				t.Fatal(err)
			}
			if first.State != "awaiting_confirmation" || first.PromptID == "" || effects.calls != 0 || model.calls != 0 {
				t.Fatalf("proposal authorization: %+v effects=%d model=%d", first, effects.calls, model.calls)
			}
			for _, value := range values {
				if !strings.Contains(strings.Join(send.texts, "\n"), value) {
					t.Fatalf("proposal omitted %q: %v", value, send.texts)
				}
			}
			if command == "confirm" {
				for _, value := range []string{"Export report", "Download CSV output.", "acme/app"} {
					if !strings.Contains(strings.Join(send.texts, "\n"), value) {
						t.Fatalf("saved preview omitted %q: %v", value, send.texts)
					}
				}
			}
			// A correction creates a different delivered revision even when the same
			// canonical content is selected; the old prompt must lose authority.
			admit("102", request+" please use this request", "")
			corrected, err := db.ReadNaturalDialogue(ctx, "discord", "7", "3")
			if err != nil {
				t.Fatal(err)
			}
			if corrected.Revision <= first.Revision || corrected.Code == first.Code || corrected.PromptID == first.PromptID {
				t.Fatalf("correction reused old authorization: first=%+v next=%+v", first, corrected)
			}
			admit("103", "yes", first.PromptID)
			if effects.calls != 0 || model.calls != 0 {
				t.Fatal("stale prompt executed")
			}
			admit("104", "confirm "+corrected.Code, "")
			if effects.calls+model.calls != 1 {
				t.Fatalf("confirmed mutation count effects=%d model=%d replies=%v", effects.calls, model.calls, send.texts)
			}
			admit("105", "confirm "+corrected.Code, "")
			if effects.calls+model.calls != 1 {
				t.Fatal("duplicate executed")
			}
		})
	}
}
