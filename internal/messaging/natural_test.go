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
	"github.com/cloudvane/litefactory/internal/messaging/feature"
	"github.com/cloudvane/litefactory/internal/messaging/response"
	"github.com/cloudvane/litefactory/internal/meterview"
	"github.com/cloudvane/litefactory/internal/redact"
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

func TestNaturalIssueProjectInference(t *testing.T) {
	for _, tc := range []struct {
		name          string
		first, second []int
		explicit      string
		want          string
	}{
		{"exact with empty", nil, []int{649}, "", "litefactory"},
		{"exact with other tasks", []int{723}, []int{649}, "", "litefactory"},
		{"single populated", nil, []int{726}, "", "litefactory"},
		{"duplicate", []int{649}, []int{649}, "", ""},
		{"no tasks", nil, nil, "", ""},
		{"explicit wins", []int{649}, []int{726}, "litefactory!", "litefactory"},
		{"unknown explicit", nil, []int{649}, "unknown", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := naturalExecutionDB(t)
			for i, name := range []string{"cockpit", "litefactory"} {
				repo, err := db.UpsertRepo(t.Context(), store.Repo{Path: t.TempDir(), Forge: "github", Owner: "acme", Name: name, BaseBranch: "main"})
				if err != nil {
					t.Fatal(err)
				}
				tasks := tc.first
				if i == 1 {
					tasks = tc.second
				}
				for _, n := range tasks {
					if _, err := db.GetOrCreateSession(t.Context(), repo, n); err != nil {
						t.Fatal(err)
					}
				}
			}
			s := New(db, "discord", "7", nil, nil, nil)
			got, err := s.resolveNaturalProject(t.Context(), map[string]string{"issue": "649", "project": tc.explicit})
			if err != nil || got.Repository.Name != tc.want || (tc.want == "" && got.Refusal == "") {
				t.Fatalf("%+v %v", got, err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			if got, err := s.resolveNaturalProject(ctx, map[string]string{"issue": "649"}); err == nil && got.Refusal == "" {
				t.Fatal("guessed after lookup failure")
			}
		})
	}
}

func TestNaturalProjectClarificationResumesSavedRequest(t *testing.T) {
	for _, answer := range []string{"litefactory!", "litefactory", "acme/litefactory", "unknown", "cancel", "expired"} {
		t.Run(answer, func(t *testing.T) {
			db := naturalExecutionDB(t)
			at := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
			for _, name := range []string{"cockpit", "litefactory"} {
				repo, err := db.UpsertRepo(t.Context(), store.Repo{Path: t.TempDir(), Forge: "github", Owner: "acme", Name: name, BaseBranch: "main"})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := db.GetOrCreateSession(t.Context(), repo, 649); err != nil {
					t.Fatal(err)
				}
			}
			question := "Is anything stuck? Why isn't issue 649 moving?"
			s := New(db, "discord", "7", []string{"3"}, &spanSender{}, FactoryStatus{DB: db})
			s.EnableResearch(nil, nil)
			s.EnableNatural(spanInterpreter{command: "diagnose", values: [][2]string{{"issue", "649"}, {"note", question}}})
			first := store.NaturalMessage{ChannelRef: "3", SourceID: "101", OwnerID: "7", Text: question, CreatedAt: at}
			if err := s.naturalTurn(t.Context(), first, at); err != nil {
				t.Fatal(err)
			}
			for range 3 {
				if _, err := s.Step(t.Context(), at); err != nil {
					t.Fatal(err)
				}
			}
			d, err := db.ReadNaturalDialogue(t.Context(), "discord", "7", "3")
			if err != nil || d.State != "clarifying" {
				t.Fatalf("%+v %v", d, err)
			}
			// Recreate the service; an interpreter that cannot propose proves the saved
			// payload, not model reconstruction, supplies the original request.
			s = New(db, "discord", "7", []string{"3"}, &spanSender{}, FactoryStatus{DB: db})
			s.EnableResearch(nil, nil)
			s.EnableNatural(spanInterpreter{command: "help"})
			text := answer
			if answer == "expired" {
				at = at.Add(16 * time.Minute)
				text = "litefactory!"
			}
			if err := s.naturalTurn(t.Context(), store.NaturalMessage{ChannelRef: "3", SourceID: "102", OwnerID: "7", Text: text, CreatedAt: at}, at); err != nil {
				t.Fatal(err)
			}
			d, err = db.ReadNaturalDialogue(t.Context(), "discord", "7", "3")
			if err != nil {
				t.Fatal(err)
			}
			valid := answer == "litefactory!" || answer == "litefactory" || answer == "acme/litefactory"
			if !valid {
				if d.State == "awaiting_confirmation" || d.State == "authorized" {
					t.Fatalf("stale/unknown executed: %+v", d)
				}
				return
			}
			var p naturalPayload
			if err := json.Unmarshal([]byte(d.Payload), &p); err != nil {
				t.Fatal(err)
			}
			if d.State != "awaiting_confirmation" || p.Invocation.Command != "diagnose" || p.Invocation.Arguments["issue"] != "649" || p.Invocation.Arguments["note"] != question || p.Project != "acme/litefactory" {
				t.Fatalf("lost request: %+v %+v", d, p)
			}
		})
	}
}

func TestStatusListsRunningAndOwnerDependentTasks(t *testing.T) {
	db := naturalExecutionDB(t)
	ctx := t.Context()
	at := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	repo, err := db.UpsertRepo(ctx, store.Repo{Path: t.TempDir(), Forge: "github", Owner: "acme", Name: "litefactory", BaseBranch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	send := &spanSender{}
	s := New(db, "discord", "7", []string{"3"}, send, FactoryStatus{DB: db})
	text, err := s.summary(ctx, Message{}, at)
	if err != nil || !strings.Contains(text, "no running tasks or owner waits") {
		t.Fatalf("empty: %s %v", text, err)
	}
	for _, n := range []int{649, 650, 651, 652} {
		session, err := db.GetOrCreateSession(ctx, repo, n)
		if err != nil {
			t.Fatal(err)
		}
		switch n {
		case 649:
			seg, err := db.GetOrCreateSegment(ctx, session.ID, 1, "session", "codex")
			if err != nil {
				t.Fatal(err)
			}
			_, err = db.AllocateRun(ctx, store.AllocateRunParams{SessionID: session.ID, SegmentID: seg.ID, Mode: "implement", Agent: "codex", TriggerCommentID: 1, StartedAt: at, LogPath: func(int) string { return "/tmp/test-run" }})
			if err != nil {
				t.Fatal(err)
			}
		case 650, 651:
			kind := "review"
			if n == 651 {
				kind = "question"
			}
			def, err := db.CreateGraphDefinition(ctx, store.GraphDefinition{Team: "chat", Mode: "implement", ProfileVersion: 1, Path: "implement.dot", Digest: strings.Repeat("a", 64), CreatedAt: at})
			if err != nil {
				t.Fatal(err)
			}
			instance, err := db.CreateGraphInstance(ctx, store.GraphInstance{SessionID: session.ID, DefinitionID: def.ID, CurrentNode: "build", StartedAt: at})
			if err != nil {
				t.Fatal(err)
			}
			attempt, err := db.StartGraphNodeAttempt(ctx, store.GraphAttemptStart{InstanceID: instance.ID, Node: "build", StartedAt: at})
			if err != nil {
				t.Fatal(err)
			}
			if err := db.FinishGraphNodeAttempt(ctx, attempt.ID, store.GraphSucceeded, kind, at); err != nil {
				t.Fatal(err)
			}
			key := "wait-" + kind
			if err := db.RetainGraphComment(ctx, store.GraphCommentIntent{Key: key, InstanceID: instance.ID, SourceAttemptID: attempt.ID, Node: "build", Kind: kind, Candidate: []byte(`{"questions":[{"text":"Choose release?"}]}`)}); err != nil {
				t.Fatal(err)
			}
			if _, err := db.EnsureReportDelivery(ctx, store.ReportDelivery{Key: key, SessionID: session.ID, Task: session.TaskRef, Destination: "acme/litefactory#" + strconv.Itoa(n), TriggerNamespace: "issue-comment", Purpose: "implement-" + kind, BotUserID: 99, Body: kind, Marker: "marker-" + key}); err != nil {
				t.Fatal(err)
			}
			if _, err := db.MarkReportDeliverySent(ctx, key); err != nil {
				t.Fatal(err)
			}
			if _, err := db.ConfirmReportDelivery(ctx, key, int64(n)); err != nil {
				t.Fatal(err)
			}
			if _, err := db.CompleteReportDelivery(ctx, key); err != nil {
				t.Fatal(err)
			}
		case 652:
			parent := "github:acme/litefactory#100"
			if err := db.EnsureImplementStories(ctx, parent, 1, []store.ImplementStorySeed{{Key: "a", Position: 1, Branch: "lf/a"}}, at); err != nil {
				t.Fatal(err)
			}
			row, err := db.TransitionImplementStory(ctx, store.ImplementTransition{Parent: parent, Generation: 1, Key: "a", From: store.ImplementWaiting, To: store.ImplementRunning, ExpectedRevision: 1, At: at})
			if err != nil {
				t.Fatal(err)
			}
			if err := db.SetImplementWorkerSession(ctx, parent, 1, "a", session.ID, at); err != nil {
				t.Fatal(err)
			}
			if _, err := db.TransitionImplementStory(ctx, store.ImplementTransition{Parent: parent, Generation: 1, Key: "a", From: store.ImplementRunning, To: store.ImplementParked, ExpectedRevision: row.Revision, Reason: "Choose deployment target", At: at}); err != nil {
				t.Fatal(err)
			}
		}
	}
	question := "Which tasks are running at the moment, and which ones are waiting on me?"
	request := InterpretationRequest{Turns: []DialogueTurn{{SourceID: "101", Text: question}}, Commands: s.NaturalCommands()}
	_, inv, err := DecodeInterpretation([]byte(`{"version":1,"kind":"propose","command":"health","arguments":[]}`), request)
	if err != nil || inv.Command != "status" {
		t.Fatalf("routing: %+v %v", inv, err)
	}
	s.EnablePresentation(nil, nil)
	if _, _, err := s.Admit(ctx, Message{Transport: "discord", ID: "101", Channel: "3", Sender: "7", Command: inv.Command, Arguments: inv.Arguments}, at); err != nil {
		t.Fatal(err)
	}
	for range 4 {
		if _, err := s.Step(ctx, at); err != nil {
			t.Fatal(err)
		}
	}
	text = strings.Join(send.texts, "\n")
	for _, want := range []string{"acme/litefactory #649: running", "#650: waiting on owner", "implement review", "#651: waiting on owner", "implement question", "#652: parked, waiting on owner", "Choose deployment target"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q: %s", want, text)
		}
	}
	s.EnablePresentation(statusTaskNarrator{}, nil)
	if _, _, err := s.Admit(ctx, Message{Transport: "discord", ID: "102", Channel: "3", Sender: "7", Command: inv.Command}, at); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Step(ctx, at); err != nil {
		t.Fatal(err)
	}
	narrated := send.texts[len(send.texts)-1]
	for _, want := range []string{"649", "650", "651", "652"} {
		if !strings.Contains(narrated, want) {
			t.Fatalf("narration omitted task %s: %s", want, narrated)
		}
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if text := s.taskSummary(cancelled); !strings.Contains(text, "could not be read") || strings.Contains(text, "no running tasks") {
		t.Fatalf("failed read: %s", text)
	}
}

// The provider is a fixture; the candidate/check validation and delivery are real.
type statusTaskNarrator struct{}

func (statusTaskNarrator) Present(_ context.Context, _ int64, _, phase, _ string) ([]byte, error) {
	if phase == "generation" {
		return []byte(`{"claims":[{"text":"Task #649 is running; #650 awaits your review, #651 awaits your answer, and #652 is parked for your deployment decision.","facts":["result"],"protected":[]}]}`), nil
	}
	return []byte(`{"findings":[{"claim":0,"support":"supported"}]}`), nil
}

type targetDiagnosis struct {
	calls   int
	request DiagnosisRequest
}

func (d *targetDiagnosis) Queue(_ context.Context, r DiagnosisRequest) error {
	d.calls++
	d.request = r
	return nil
}

func TestReportedDiagnosisQueuesOnlyAfterConfirmation(t *testing.T) {
	db := naturalExecutionDB(t)
	ctx := t.Context()
	at := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	for _, name := range []string{"cockpit", "litefactory"} {
		repo, err := db.UpsertRepo(ctx, store.Repo{Path: t.TempDir(), Forge: "github", Owner: "acme", Name: name, BaseBranch: "main"})
		if err != nil {
			t.Fatal(err)
		}
		if name == "litefactory" {
			if _, err := db.GetOrCreateSession(ctx, repo, 649); err != nil {
				t.Fatal(err)
			}
		}
	}
	question := "Is anything stuck? Why isn't issue 649 moving?"
	diagnosis := &targetDiagnosis{}
	s := New(db, "discord", "7", []string{"3"}, &spanSender{}, FactoryStatus{DB: db})
	s.EnableResearch(diagnosis, nil)
	s.EnableNatural(spanInterpreter{command: "diagnose", values: [][2]string{{"issue", "649"}, {"note", question}}})
	if err := s.naturalTurn(ctx, store.NaturalMessage{SourceID: "101", ChannelRef: "3", OwnerID: "7", Text: question, CreatedAt: at}, at); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Step(ctx, at); err != nil {
		t.Fatal(err)
	}
	d, err := db.ReadNaturalDialogue(ctx, "discord", "7", "3")
	if err != nil {
		t.Fatal(err)
	}
	if d.State != "awaiting_confirmation" || diagnosis.calls != 0 {
		t.Fatalf("premature queue: %+v %+v", d, diagnosis)
	}
	if err := s.naturalTurn(ctx, store.NaturalMessage{SourceID: "102", ChannelRef: "3", OwnerID: "7", Text: "yes", ReplyTo: d.PromptID, CreatedAt: at}, at); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Step(ctx, at); err != nil {
		t.Fatal(err)
	}
	if diagnosis.calls != 1 || diagnosis.request.Target != "github:acme/litefactory#649" || diagnosis.request.Note != question {
		t.Fatalf("wrong diagnosis: %+v", diagnosis)
	}
}

// The missing arguments in the reported proposal are fixture data, not observed
// owner content. Preserve the visible report text to exercise existing proposals.
const reportedProposalCode = "a03a88fba8cffa35"
const reportedProposal = "Proposed action: draft\nProject: marioatcloudvane/litefactory\n…\nConfirmation code: a03a88fba8cffa35\nReply directly to this message with yes, or send confirm a03a88fba8cffa35. Send cancel to cancel. Expires in 15 minutes."

func proposalConversation(t *testing.T, at time.Time, change func(*store.NaturalDialogue, *naturalPayload)) (*naturalConversation, store.NaturalDialogue) {
	t.Helper()
	c := newNaturalConversation(t, func(InterpretationRequest) ([]byte, error) {
		return []byte(`{"version":1,"kind":"ignore"}`), nil
	})
	c.s.EnableFeatures(nil, nil)
	repo, err := c.db.UpsertRepo(t.Context(), store.Repo{Path: t.TempDir(), Forge: "github", Owner: "marioatcloudvane", Name: "litefactory", BaseBranch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	p := naturalPayload{Invocation: Invocation{Command: "draft", Arguments: map[string]string{"project": repo.Slug(), "note": "Export report"}}, Project: repo.Slug(), RepoID: repo.ID, Channel: "3", Prompt: reportedProposal}
	d := store.NaturalDialogue{Transport: "discord", OwnerID: "7", ChannelRef: "3", State: "awaiting_confirmation", SourceID: "100", Revision: 1, Code: reportedProposalCode, PromptID: "900", UpdatedAt: at, ExpiresAt: at.Add(15 * time.Minute)}
	if change != nil {
		change(&d, &p)
	}
	d.Payload, d.Digest, err = naturalDigest(p)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.db.SaveNaturalDialogue(t.Context(), d, 0); err != nil {
		t.Fatal(err)
	}
	if d.PromptID != "" {
		if err = c.db.BindNaturalPrompt(t.Context(), d, d.PromptID); err != nil {
			t.Fatal(err)
		}
	}
	return c, d
}

type proposalDraftModel struct{ calls int }

func (m *proposalDraftModel) Answer(context.Context, int64, string) ([]byte, error) {
	m.calls++
	return json.Marshal(feature.Draft{Title: "Export report", Summary: "Export the report", Problem: "Manual copying", Wanted: "A downloadable file"})
}

func TestReportedProposalConfirmationForms(t *testing.T) {
	at := time.Date(2026, 10, 12, 13, 57, 0, 0, time.UTC)
	for _, text := range []string{reportedProposalCode, "confirm " + reportedProposalCode, "yes " + reportedProposalCode, "ok " + reportedProposalCode, "  A03A88FBA8CFFA35\n", " CONFIRM A03A88FBA8CFFA35 ", " Yes A03A88FBA8CFFA35 ", " OK A03A88FBA8CFFA35 "} {
		t.Run(text, func(t *testing.T) {
			c, d := proposalConversation(t, at, nil)
			model := &proposalDraftModel{}
			c.s.EnableFeatures(&feature.Drafter{Repositories: c.db, Model: model, Store: c.db, Redactor: redact.New(redact.Options{Environ: []string{}}), Now: func() time.Time { return at }}, nil)
			c.say("101", text, at)
			row, err := c.db.ReadChatReply(t.Context(), "discord", "nl-exec:"+d.Code)
			if err != nil || row.Command != "draft" || row.Arguments["note"] != "Export report" || row.Arguments["project"] != "marioatcloudvane/litefactory" {
				t.Fatalf("saved arguments lost: %+v %v", row, err)
			}
			for range 10 {
				did, err := c.s.Step(t.Context(), at)
				if err != nil {
					t.Fatal(err)
				}
				if !did {
					break
				}
			}
			c.say("102", text, at)
			preview, err := c.db.ChatIssuePreviewForMessage(t.Context(), "discord", "nl-exec:"+d.Code)
			if err != nil || preview.State != "previewed" || preview.RepoID == 0 || preview.IssueNumber != 0 || preview.IssueLink != "" {
				t.Fatalf("preview missing: %+v %v", preview, err)
			}
			execution, err := c.db.ReadNaturalExecution(t.Context(), "nl-exec:"+d.Code)
			if err != nil || execution.State != "finished" || model.calls != 1 || len(c.requests) != 0 {
				t.Fatalf("execution=%+v err=%v draft calls=%d interpreter calls=%d", execution, err, model.calls, len(c.requests))
			}
			// No issue-creation handler is installed; the saved command and
			// resulting preview must remain draft-only.
		})
	}
}

func TestUnavailableProposalCodes(t *testing.T) {
	at := time.Date(2026, 10, 12, 13, 57, 0, 0, time.UTC)
	for _, state := range []string{"wrong", "replaced", "cancelled", "finished", "expired", "unknown"} {
		for _, prefix := range []string{"", "confirm ", "yes ", "ok "} {
			t.Run(state+"/"+prefix, func(t *testing.T) {
				c, d := proposalConversation(t, at, nil)
				code := d.Code
				switch state {
				case "wrong":
					code = "0000000000000000"
				case "replaced":
					d.Revision++
					d.Code = "0000000000000000"
					if err := c.db.SaveNaturalDialogue(t.Context(), d, 1); err != nil {
						t.Fatal(err)
					}
					if err := c.db.BindNaturalPrompt(t.Context(), d, "901"); err != nil {
						t.Fatal(err)
					}
				case "cancelled", "finished":
					if err := c.db.EndNaturalDialogue(t.Context(), d, state, at); err != nil {
						t.Fatal(err)
					}
				case "unknown":
					c = newNaturalConversation(t, func(InterpretationRequest) ([]byte, error) { return []byte(`{"version":1,"kind":"ignore"}`), nil })
				}
				now := at
				if state == "expired" {
					now = at.Add(16 * time.Minute)
				}
				before, _ := c.db.ReadNaturalDialogue(t.Context(), "discord", "7", "3")
				c.say("101", prefix+code, now)
				reply, err := c.db.ReadChatReply(t.Context(), "discord", "nl:101")
				if err != nil || reply.ReplyText != "This confirmation code is unknown or expired; request a fresh proposal." || len(c.requests) != 0 {
					t.Fatalf("refusal=%q err=%v interpreter calls=%d", reply.ReplyText, err, len(c.requests))
				}
				if _, err = c.db.ReadNaturalExecution(t.Context(), "nl-exec:"+code); !errors.Is(err, store.ErrNotFound) {
					t.Fatalf("unexpected execution: %v", err)
				}
				if state == "wrong" || state == "replaced" {
					after, err := c.db.ReadNaturalDialogue(t.Context(), "discord", "7", "3")
					if err != nil || before != after {
						t.Fatalf("wrong code changed proposal: %+v -> %+v (%v)", before, after, err)
					}
					c.say("102", d.Code, now)
					if _, err := c.db.ReadNaturalExecution(t.Context(), "nl-exec:"+d.Code); err != nil {
						t.Fatalf("correct code no longer works: %v", err)
					}
				}
				if state == "expired" {
					after, err := c.db.ReadNaturalDialogue(t.Context(), "discord", "7", "3")
					if err != nil || after.Code != "" || after.State != "expired" {
						t.Fatalf("expiry purge not exercised: %+v %v", after, err)
					}
				}
			})
		}
	}
}

func TestProposalContextAndInstructions(t *testing.T) {
	at := time.Date(2026, 10, 12, 13, 57, 0, 0, time.UTC)
	for _, state := range []string{"live", "long", "undelivered", "expired"} {
		t.Run(state, func(t *testing.T) {
			c, _ := proposalConversation(t, at, func(d *store.NaturalDialogue, p *naturalPayload) {
				if state == "undelivered" {
					d.PromptID = ""
				}
				if state == "long" {
					p.Prompt = strings.Repeat("ä", 4000) + reportedProposal
				}
			})
			now := at
			if state == "expired" {
				now = at.Add(16 * time.Minute)
			}
			c.say("101", "change the draft", now)
			if len(c.requests) != 1 {
				t.Fatalf("requests=%v", c.requests)
			}
			previous := c.requests[0].PreviousReply
			if state == "expired" || state == "undelivered" {
				if previous != "" {
					t.Fatalf("unavailable proposal represented as confirmable: %q", previous)
				}
				return
			}
			for _, want := range []string{"context only", "Proposed action: draft", "Project: marioatcloudvane/litefactory", "Confirmation code: " + reportedProposalCode} {
				if !strings.Contains(previous, want) {
					t.Errorf("context missing %q: %s", want, previous)
				}
			}
			if len(previous) > 4096 || !utf8.ValidString(previous) {
				t.Fatal("invalid context bound")
			}
			d, err := c.db.ReadNaturalDialogue(t.Context(), "discord", "7", "3")
			if err != nil || d.Code != "" || d.PromptID != "" {
				t.Fatalf("correction did not invalidate proposal: %+v %v", d, err)
			}
		})
	}
	s := New(nil, "discord", "7", nil, nil, nil)
	text := s.naturalProposal(naturalPayload{Invocation: Invocation{Command: "draft"}, Project: "marioatcloudvane/litefactory"}, reportedProposalCode)
	for _, want := range []string{"Reply directly to this message with yes", "send " + reportedProposalCode + " alone or prefixed by confirm, yes or ok", "Send cancel to cancel", "Expires in 15 minutes", "feature preview only", "Issue creation needs a separate confirmation"} {
		if !strings.Contains(text, want) {
			t.Errorf("instructions missing %q: %s", want, text)
		}
	}
}

func TestProposalConfirmationAuthority(t *testing.T) {
	at := time.Date(2026, 10, 12, 13, 57, 0, 0, time.UTC)
	for _, boundary := range []string{"wrong owner", "other channel", "disallowed channel", "undelivered", "stale source", "changed digest", "failed recheck", "quoted", "embedded", "unbound yes", "unbound ok", "reply yes"} {
		t.Run(boundary, func(t *testing.T) {
			c, d := proposalConversation(t, at, func(d *store.NaturalDialogue, p *naturalPayload) {
				if boundary == "undelivered" {
					d.PromptID = ""
				}
				if boundary == "stale source" {
					d.SourceID = "200"
				}
				if boundary == "failed recheck" {
					p.Project = "missing/project"
				}
			})
			if boundary == "changed digest" {
				d.Revision++
				d.Digest = strings.Repeat("0", 64)
				if err := c.db.SaveNaturalDialogue(t.Context(), d, 1); err != nil {
					t.Fatal(err)
				}
				if err := c.db.BindNaturalPrompt(t.Context(), d, "900"); err != nil {
					t.Fatal(err)
				}
			}
			m := store.NaturalMessage{ChannelRef: "3", SourceID: "101", OwnerID: "7", Text: d.Code, CreatedAt: at}
			switch boundary {
			case "wrong owner":
				m.OwnerID = "8"
			case "other channel", "disallowed channel":
				m.ChannelRef = "4"
				if boundary == "other channel" {
					c.s.channels["4"] = true
				}
				if err := c.db.ActivateNaturalScan(t.Context(), "4", "100"); err != nil {
					t.Fatal(err)
				}
			case "quoted":
				m.Text = "> " + d.Code
			case "embedded":
				m.Text = "please use " + d.Code + " now"
			case "unbound yes":
				m.Text = "yes"
			case "unbound ok":
				m.Text = "ok"
			case "reply yes":
				m.Text, m.ReplyTo = "yes", d.PromptID
			}
			// Stage directly to exercise NaturalStep's owner guard even if
			// intake has not already filtered an unauthorized sender.
			scan, err := c.db.ReadNaturalScan(t.Context(), m.ChannelRef)
			if err != nil {
				t.Fatal(err)
			}
			next := scan
			next.HighWater = m.SourceID
			if err = c.db.StageNaturalScan(t.Context(), scan, next, []store.NaturalMessage{m}, true); err != nil {
				t.Fatal(err)
			}
			if _, err = c.s.NaturalStep(t.Context(), at); err != nil {
				t.Fatal(err)
			}
			_, err = c.db.ReadNaturalExecution(t.Context(), "nl-exec:"+d.Code)
			if boundary == "reply yes" {
				if err != nil {
					t.Fatalf("direct reply lost authorization: %v", err)
				}
			} else if !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("%s authorized: %v", boundary, err)
			}
			if boundary != "quoted" && boundary != "embedded" && boundary != "unbound ok" && len(c.requests) != 0 {
				t.Fatalf("confirmation reached interpreter: %v", c.requests)
			}
		})
	}
}
