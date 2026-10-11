package messaging

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

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
