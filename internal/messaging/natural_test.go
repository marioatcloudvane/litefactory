package messaging

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/cloudvane/litefactory/internal/messaging/ask"
	"github.com/cloudvane/litefactory/internal/messaging/response"
	"github.com/cloudvane/litefactory/internal/meterview"
	"github.com/cloudvane/litefactory/internal/store"
)

func TestRelativeBound(t *testing.T) {
	zone := time.FixedZone("CEST", 2*60*60)
	now := time.Date(2026, 10, 14, 15, 30, 0, 0, zone) // Wednesday
	day := func(month time.Month, d int) string {
		return time.Date(2026, month, d, 0, 0, 0, 0, zone).Format(time.RFC3339)
	}
	cases := []struct {
		phrase, from, to string
		ok               bool
	}{
		{"today", day(10, 14), "", true},
		{"yesterday", day(10, 13), day(10, 14), true},
		{"since yesterday", day(10, 13), "", true},
		{"Since Yesterday", day(10, 13), "", true},
		{"this week", day(10, 12), "", true},
		{"last week", day(10, 5), day(10, 12), true},
		{"since last week", day(10, 5), "", true},
		{"this month", day(10, 1), "", true},
		{"last month", day(9, 1), day(10, 1), true},
		{"since monday", day(10, 12), "", true},
		{"since wednesday", day(10, 14), "", true},
		{"since thursday", day(10, 8), "", true},
		{"sunday", day(10, 11), "", true},
		{"three days ago", "", "", false},
		{"2026-10-01", "", "", false},
		{"", "", "", false},
	}
	for _, tc := range cases {
		from, to, ok := relativeBound(tc.phrase, now)
		if from != tc.from || to != tc.to || ok != tc.ok {
			t.Errorf("%q: got %q %q %t want %q %q %t", tc.phrase, from, to, ok, tc.from, tc.to, tc.ok)
		}
	}
}

// spanInterpreter proposes one command whose arguments are exact owner-text spans.
type spanInterpreter struct {
	command string
	values  [][2]string
}

func (p spanInterpreter) Interpret(_ context.Context, r InterpretationRequest) ([]byte, error) {
	turn := r.Turns[len(r.Turns)-1]
	var args []SourceValue
	for _, v := range p.values {
		start := strings.Index(turn.Text, v[1])
		args = append(args, SourceValue{Name: v[0], SourceID: turn.SourceID, Start: start, End: start + len(v[1])})
	}
	return json.Marshal(Interpretation{Version: 1, Kind: "propose", Command: p.command, Arguments: args})
}

type spanSender struct{ texts []string }

func (p *spanSender) Deliver(_ context.Context, _ Target, text string) Delivery {
	p.texts = append(p.texts, text)
	return Delivery{Confirmed: true, PromptID: "900"}
}

type spanReader struct{ messages []store.NaturalMessage }

func (*spanReader) CheckIntakeCapability(context.Context, string) error { return nil }
func (r *spanReader) ReadIntakePage(_ context.Context, _, _, before string) (IntakePage, error) {
	if before != "" || len(r.messages) == 0 {
		return IntakePage{}, nil
	}
	return IntakePage{Messages: r.messages, HighID: r.messages[0].SourceID, LowID: r.messages[len(r.messages)-1].SourceID, Count: len(r.messages)}, nil
}

// naturalRead runs one owner message through intake and interpretation and
// returns the admitted read arguments (nil if none) and all replies sent.
func naturalRead(t *testing.T, repos []string, text string, interpreter spanInterpreter, at time.Time) (map[string]string, []string) {
	t.Helper()
	db := naturalExecutionDB(t)
	ctx := t.Context()
	for _, name := range repos {
		if _, err := db.UpsertRepo(ctx, store.Repo{Path: t.TempDir(), Forge: "github", Owner: "acme", Name: name, BaseBranch: "main"}); err != nil {
			t.Fatal(err)
		}
	}
	send := &spanSender{}
	s := New(db, "discord", "7", []string{"3"}, send, FactoryStatus{DB: db})
	s.EnableResearch(nil, nil)
	s.EnableNatural(interpreter)
	if err := db.ActivateNaturalScan(ctx, "3", "100"); err != nil {
		t.Fatal(err)
	}
	intake := &Intake{DB: db, Reader: &spanReader{messages: []store.NaturalMessage{{ChannelRef: "3", SourceID: "101", OwnerID: "7", Text: text, CreatedAt: at}}}, Transport: "discord", Owner: "7", Channels: []string{"3"}, Protect: func(_ context.Context, text string) (string, error) { return text, nil }}
	if err := intake.Poll(ctx, at); err != nil {
		t.Fatal(err)
	}
	var args map[string]string
	for range 10 {
		natural, err := s.NaturalStep(ctx, at)
		if err != nil {
			t.Fatal(err)
		}
		if row, e := db.ReadChatReply(ctx, "discord", "nl-read:101"); e == nil && args == nil && len(row.Arguments) > 0 {
			args = row.Arguments
		}
		shared, err := s.Step(ctx, at)
		if err != nil {
			t.Fatal(err)
		}
		if !natural && !shared {
			return args, send.texts
		}
	}
	t.Fatal("natural request did not settle")
	return nil, nil
}

func TestNaturalMeterResolvesYesterdayAndStatesRange(t *testing.T) {
	at := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	local := at.In(time.Local)
	today := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.Local)
	want := today.AddDate(0, 0, -1).Format(time.RFC3339)
	args, replies := naturalRead(t, []string{"app"}, "How much have we spent since yesterday", spanInterpreter{command: "meter", values: [][2]string{{"from", "since yesterday"}}}, at)
	if args["from"] != want || args["to"] != "" {
		t.Fatalf("from not resolved: %v replies=%v", args, replies)
	}
	if _, err := meterview.Validate(meterview.Request{From: args["from"]}, true); err != nil {
		t.Fatalf("resolved bound rejected: %v", err)
	}
	if len(replies) != 1 || !strings.Contains(replies[0], "Time range: "+want+" – now") {
		t.Fatalf("reply lacks resolved range or asked: %v", replies)
	}
}

func TestNaturalSingleProjectIsDefault(t *testing.T) {
	at := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	args, replies := naturalRead(t, []string{"app"}, "how much did we spend on feature 631?", spanInterpreter{command: "meter", values: [][2]string{{"task", "631"}}}, at)
	if args["project"] != "acme/app" || args["task"] != "631" {
		t.Fatalf("meter task project not defaulted: %v replies=%v", args, replies)
	}
	args, replies = naturalRead(t, []string{"app"}, "Is there an open research question on issue 12?", spanInterpreter{command: "question", values: [][2]string{{"issue", "12"}}}, at)
	if args["project"] != "acme/app" || strings.Contains(strings.Join(replies, "\n"), "name a project") {
		t.Fatalf("question project not defaulted: %v replies=%v", args, replies)
	}
}

func TestNaturalSeveralProjectsAsk(t *testing.T) {
	at := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	args, replies := naturalRead(t, []string{"app", "web"}, "Is there an open research question on issue 12?", spanInterpreter{command: "question", values: [][2]string{{"issue", "12"}}}, at)
	if args != nil || len(replies) != 1 || !strings.Contains(replies[0], "Please name a project. Registered projects: acme/app, acme/web") {
		t.Fatalf("several projects did not ask: %v replies=%v", args, replies)
	}
	args, _ = naturalRead(t, []string{"app", "web"}, "Is there an open research question on web issue 12?", spanInterpreter{command: "question", values: [][2]string{{"project", "web"}, {"issue", "12"}}}, at)
	if args["project"] != "acme/web" {
		t.Fatalf("named project not resolved: %v", args)
	}
}

// funcInterpreter adapts a function to the Interpreter seam for fallback tests.
type funcInterpreter func(context.Context, InterpretationRequest) ([]byte, error)

func (f funcInterpreter) Interpret(ctx context.Context, r InterpretationRequest) ([]byte, error) {
	return f(ctx, r)
}

// naturalFallback runs one owner message through interpretation with a
// captured log and returns the replies, the log text and the dialogue state.
func naturalFallback(t *testing.T, interpreter Interpreter) ([]string, string, string) {
	t.Helper()
	db := naturalExecutionDB(t)
	ctx := t.Context()
	at := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	if _, err := db.UpsertRepo(ctx, store.Repo{Path: t.TempDir(), Forge: "github", Owner: "acme", Name: "app", BaseBranch: "main"}); err != nil {
		t.Fatal(err)
	}
	send := &spanSender{}
	var logs bytes.Buffer
	s := New(db, "discord", "7", []string{"3"}, send, FactoryStatus{DB: db})
	s.Logger = slog.New(slog.NewTextHandler(&logs, nil))
	s.EnableNatural(interpreter)
	if err := db.ActivateNaturalScan(ctx, "3", "100"); err != nil {
		t.Fatal(err)
	}
	intake := &Intake{DB: db, Reader: &spanReader{messages: []store.NaturalMessage{{ChannelRef: "3", SourceID: "101", OwnerID: "7", Text: "How is the factory doing?", CreatedAt: at}}}, Transport: "discord", Owner: "7", Channels: []string{"3"}, Protect: func(_ context.Context, text string) (string, error) { return text, nil }}
	if err := intake.Poll(ctx, at); err != nil {
		t.Fatal(err)
	}
	for range 10 {
		natural, err := s.NaturalStep(ctx, at)
		if err != nil {
			t.Fatal(err)
		}
		shared, err := s.Step(ctx, at)
		if err != nil {
			t.Fatal(err)
		}
		if !natural && !shared {
			d, err := db.ReadNaturalDialogue(ctx, "discord", "7", "3")
			if err != nil {
				t.Fatal(err)
			}
			return send.texts, logs.String(), d.State
		}
	}
	t.Fatal("natural request did not settle")
	return nil, "", ""
}

func TestNaturalSlowInterpreterWithinBudgetSucceeds(t *testing.T) {
	var remaining time.Duration
	slow := funcInterpreter(func(ctx context.Context, _ InterpretationRequest) ([]byte, error) {
		deadline, _ := ctx.Deadline()
		remaining = time.Until(deadline)
		return []byte(`{"version":1,"kind":"clarify","question":"Which project?"}`), nil
	})
	replies, logs, _ := naturalFallback(t, slow)
	if remaining <= 30*time.Second || remaining > response.Budget {
		t.Fatalf("interpreter budget = %s, want more than 30s and at most %s", remaining, response.Budget)
	}
	if strings.Contains(logs, "natural interpretation fallback") || len(replies) != 1 || !strings.Contains(replies[0], "Which project?") {
		t.Fatalf("slow interpretation within budget fell back: replies=%v logs=%s", replies, logs)
	}
}

func TestNaturalInterpretationFallbackNamesReason(t *testing.T) {
	saved := response.Budget
	t.Cleanup(func() { response.Budget = saved })
	response.Budget = 50 * time.Millisecond
	cases := []struct {
		reason      string
		interpreter funcInterpreter
		reply       string
	}{
		{"timeout", func(ctx context.Context, _ InterpretationRequest) ([]byte, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		}, "The factory is busy, please try again in a minute. Nothing was applied."},
		{"model_error", func(context.Context, InterpretationRequest) ([]byte, error) {
			return nil, errors.New("provider failed")
		}, "could not safely understand"},
		{"decode", func(context.Context, InterpretationRequest) ([]byte, error) {
			return []byte("not json"), nil
		}, "could not safely understand"},
	}
	for _, tc := range cases {
		t.Run(tc.reason, func(t *testing.T) {
			replies, logs, state := naturalFallback(t, tc.interpreter)
			if len(replies) != 1 || !strings.Contains(replies[0], tc.reply) {
				t.Fatalf("replies = %v, want %q", replies, tc.reply)
			}
			if tc.reason == "timeout" && strings.Contains(replies[0], "could not safely understand") {
				t.Fatalf("timeout blamed the wording: %q", replies[0])
			}
			if state != "finished" {
				t.Fatalf("dialogue state = %q, want finished", state)
			}
			if strings.Count(logs, "natural interpretation fallback") != 1 || !strings.Contains(logs, "reason="+tc.reason) || !strings.Contains(logs, "elapsed=") || !strings.Contains(logs, "source_id=101") {
				t.Fatalf("fallback log = %q", logs)
			}
		})
	}
}

// naturalConversation runs owner messages one by one through intake and the
// natural step only, recording every interpretation request.
type naturalConversation struct {
	t        *testing.T
	db       *store.DB
	s        *Service
	send     *spanSender
	requests []InterpretationRequest
}

func newNaturalConversation(t *testing.T, answer func(InterpretationRequest) ([]byte, error)) *naturalConversation {
	t.Helper()
	c := &naturalConversation{t: t, db: naturalExecutionDB(t), send: &spanSender{}}
	if _, err := c.db.UpsertRepo(t.Context(), store.Repo{Path: t.TempDir(), Forge: "github", Owner: "acme", Name: "litefactory", BaseBranch: "main"}); err != nil {
		t.Fatal(err)
	}
	c.s = New(c.db, "discord", "7", []string{"3"}, c.send, FactoryStatus{DB: c.db})
	c.s.EnableNatural(funcInterpreter(func(_ context.Context, r InterpretationRequest) ([]byte, error) {
		c.requests = append(c.requests, r)
		return answer(r)
	}))
	if err := c.db.ActivateNaturalScan(t.Context(), "3", "100"); err != nil {
		t.Fatal(err)
	}
	return c
}

func (c *naturalConversation) say(id, text string, at time.Time) {
	c.t.Helper()
	intake := &Intake{DB: c.db, Reader: &spanReader{messages: []store.NaturalMessage{{ChannelRef: "3", SourceID: id, OwnerID: "7", Text: text, CreatedAt: at}}}, Transport: "discord", Owner: "7", Channels: []string{"3"}, Protect: func(_ context.Context, text string) (string, error) { return text, nil }}
	if err := intake.Poll(c.t.Context(), at); err != nil {
		c.t.Fatal(err)
	}
	for range 10 {
		natural, err := c.s.NaturalStep(c.t.Context(), at)
		if err != nil {
			c.t.Fatal(err)
		}
		if !natural {
			return
		}
	}
	c.t.Fatal("natural request did not settle")
}

// askReply records a delivered /ask reply in the channel.
func (c *naturalConversation) askReply(text string, at time.Time) {
	c.t.Helper()
	ctx := c.t.Context()
	if _, err := c.db.AdmitChatReply(ctx, store.ChatReply{Transport: "discord", MessageID: "ask-1", ChannelRef: "3", Command: "ask", ReplyText: text, CreatedAt: at}); err != nil {
		c.t.Fatal(err)
	}
	r, err := c.db.ClaimChatReply(ctx, "discord", at)
	if err != nil {
		c.t.Fatal(err)
	}
	if err = c.db.FinishChatReply(ctx, r, false, at); err != nil {
		c.t.Fatal(err)
	}
}

func proposeSpans(r InterpretationRequest, command string, values ...[2]string) ([]byte, error) {
	if len(values) == 0 {
		return []byte(`{"version":1,"kind":"propose","command":"` + command + `","arguments":[]}`), nil
	}
	turn := r.Turns[len(r.Turns)-1]
	args := []SourceValue{}
	for _, v := range values {
		start := strings.Index(turn.Text, v[1])
		args = append(args, SourceValue{Name: v[0], SourceID: turn.SourceID, Start: start, End: start + len(v[1])})
	}
	return json.Marshal(Interpretation{Version: 1, Kind: "propose", Command: command, Arguments: args})
}

func TestNaturalTypoStatusRunsStatusWithoutClarifying(t *testing.T) {
	at := time.Date(2026, 10, 12, 10, 25, 0, 0, time.UTC)
	for i, text := range []string{"what is the overall staud", "what is the status of the livrary?"} {
		c := newNaturalConversation(t, func(r InterpretationRequest) ([]byte, error) { return proposeSpans(r, "status") })
		id := strconv.Itoa(101 + i)
		c.say(id, text, at)
		row, err := c.db.ReadChatReply(t.Context(), "discord", "nl-read:"+id)
		if err != nil || row.Command != "status" {
			t.Fatalf("%q: status not admitted: %+v %v", text, row, err)
		}
		if len(c.send.texts) != 0 {
			t.Fatalf("%q: asked back: %v", text, c.send.texts)
		}
	}
}

func TestNaturalYesFollowUpAnswersPreviousOffer(t *testing.T) {
	at := time.Date(2026, 10, 12, 11, 0, 0, 0, time.UTC)
	offer := "I can't find that. If you mean the whole project, I can summarize the project status of litefactory."
	c := newNaturalConversation(t, func(r InterpretationRequest) ([]byte, error) {
		if r.PreviousReply == "" {
			return []byte(`{"version":1,"kind":"clarify","question":"What do you mean?"}`), nil
		}
		return proposeSpans(r, "ask", [2]string{"question", "project status of litefactory"})
	})
	c.s.EnableAsk(&ask.Service{})
	c.askReply(offer, at)
	c.say("101", "Yes project status of litefactory", at.Add(time.Minute))
	if len(c.requests) != 1 || c.requests[0].PreviousReply != offer {
		t.Fatalf("offer not supplied as previous_reply: %+v", c.requests)
	}
	row, err := c.db.ReadChatReply(t.Context(), "discord", "nl-read:101")
	if err != nil || row.Command != "ask" || row.Question != "project status of litefactory" {
		t.Fatalf("offered read not admitted: %s %q %v replies=%v", row.Command, row.Question, err, c.send.texts)
	}
	raw, err := json.Marshal(c.requests[0])
	if err != nil || !strings.Contains(string(raw), `"previous_reply":`) {
		t.Fatalf("previous_reply not serialized: %s %v", raw, err)
	}

	// A bare yes without a pending change proposal reaches the interpreter.
	c.say("102", "yes", at.Add(2*time.Minute))
	if len(c.requests) != 2 || c.requests[1].PreviousReply != offer {
		t.Fatalf("bare yes did not reach interpreter with context: %+v", c.requests)
	}
	if strings.Contains(strings.Join(c.send.texts, "\n"), "Nothing was authorized") {
		t.Fatalf("bare yes refused: %v", c.send.texts)
	}

	// An /ask reply older than the dialogue window is not context.
	stale := newNaturalConversation(t, func(InterpretationRequest) ([]byte, error) { return []byte(`{"version":1,"kind":"ignore"}`), nil })
	stale.askReply(offer, at)
	stale.say("101", "yes", at.Add(16*time.Minute))
	if len(stale.requests) != 1 || stale.requests[0].PreviousReply != "" {
		t.Fatalf("stale reply used as context: %+v", stale.requests)
	}
}

func TestNaturalAmbiguousAsksOnceAndKeepsQuestion(t *testing.T) {
	at := time.Date(2026, 10, 12, 11, 0, 0, 0, time.UTC)
	c := newNaturalConversation(t, func(r InterpretationRequest) ([]byte, error) {
		if r.PreviousReply == "" {
			return []byte(`{"version":1,"kind":"clarify","question":"Do you mean the factory status or the project litefactory?"}`), nil
		}
		return proposeSpans(r, "status")
	})
	c.say("101", "how is it going", at)
	d, err := c.db.ReadNaturalDialogue(t.Context(), "discord", "7", "3")
	if err != nil || d.State != "clarifying" || len(c.send.texts) != 0 {
		t.Fatalf("dialogue=%+v err=%v", d, err)
	}
	reply, err := c.db.ReadChatReply(t.Context(), "discord", "nl:101")
	if err != nil || !strings.Contains(reply.ReplyText, "factory status or the project") {
		t.Fatalf("clarifying reply = %+v %v", reply, err)
	}
	c.say("102", "the factory", at.Add(time.Minute))
	if len(c.requests) != 2 || c.requests[1].PreviousReply != "Do you mean the factory status or the project litefactory?" {
		t.Fatalf("clarifying question not carried: %+v", c.requests)
	}
	if row, err := c.db.ReadChatReply(t.Context(), "discord", "nl-read:102"); err != nil || row.Command != "status" {
		t.Fatalf("answer not admitted: %+v %v", row, err)
	}
}

func TestCapPreviousReplyKeepsValidUTF8(t *testing.T) {
	got := capPreviousReply(strings.Repeat("ä", 3000))
	if len(got) > 4096 || !utf8.ValidString(got) || got == "" {
		t.Fatalf("capped reply len=%d valid=%t", len(got), utf8.ValidString(got))
	}
}

func TestNaturalBareYesRefusedWhileProposalAwaits(t *testing.T) {
	at := time.Date(2026, 10, 12, 11, 0, 0, 0, time.UTC)
	c := newNaturalConversation(t, func(InterpretationRequest) ([]byte, error) { return []byte(`{"version":1,"kind":"ignore"}`), nil })
	payload, digest, err := naturalDigest(naturalPayload{Invocation: Invocation{Command: "diagnose", Arguments: map[string]string{"issue": "3", "note": "exact"}}})
	if err != nil {
		t.Fatal(err)
	}
	d := store.NaturalDialogue{Transport: "discord", OwnerID: "7", ChannelRef: "3", Revision: 1, State: "awaiting_confirmation", SourceID: "100", Payload: payload, Digest: digest, Code: "code", PromptID: "900", ExpiresAt: at.Add(time.Minute), UpdatedAt: at}
	if err = c.db.SaveNaturalDialogue(t.Context(), d, 0); err != nil {
		t.Fatal(err)
	}
	c.say("101", "yes", at)
	reply, err := c.db.ReadChatReply(t.Context(), "discord", "nl:101")
	if err != nil || !strings.Contains(reply.ReplyText, "Nothing was authorized") || len(c.requests) != 0 {
		t.Fatalf("bare yes not refused: %q %v requests=%d", reply.ReplyText, err, len(c.requests))
	}
}
