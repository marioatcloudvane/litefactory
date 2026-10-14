package turn

import (
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
)

func TestParseCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want Command
	}{
		{
			name: "a plain spike",
			body: "/spike fix the login bug",
			want: Command{Kind: KindSpike, Name: "/spike", Text: "fix the login bug"},
		},
		{
			name: "leading whitespace is skipped",
			body: "   /spike fix the login bug",
			want: Command{Kind: KindSpike, Name: "/spike", Text: "fix the login bug"},
		},
		{
			name: "leading blank lines are skipped",
			body: "\n\n  /spike fix the login bug\n",
			want: Command{Kind: KindSpike, Name: "/spike", Text: "fix the login bug"},
		},
		{
			name: "a tab separates the argument",
			body: "/spike\tfix the login bug",
			want: Command{Kind: KindSpike, Name: "/spike", Text: "fix the login bug"},
		},
		{
			name: "the argument keeps its interior shape",
			body: "/spike fix the bug\n\n- check the retry path\n- add a test\n",
			want: Command{Kind: KindSpike, Name: "/spike", Text: "fix the bug\n\n- check the retry path\n- add a test"},
		},
		{
			name: "the argument may start on the next line",
			body: "/spike\nfix the login bug\nand the logout one",
			want: Command{Kind: KindSpike, Name: "/spike", Text: "fix the login bug\nand the logout one"},
		},
		{
			name: "an argument that looks like flags is still text",
			body: "/spike --force the issue",
			want: Command{Kind: KindSpike, Name: "/spike", Text: "--force the issue"},
		},
		{
			name: "a following slash command is part of the argument",
			body: "/spike then /plan the rest",
			want: Command{Kind: KindSpike, Name: "/spike", Text: "then /plan the rest"},
		},

		// Case folding. A phone keyboard capitalises the first word of a
		// message, so /Spike has to work; the reported name is canonical.
		{
			name: "an autocapitalised spike",
			body: "/Spike fix the login bug",
			want: Command{Kind: KindSpike, Name: "/spike", Text: "fix the login bug"},
		},
		{
			name: "a shouted spike",
			body: "/SPIKE fix the login bug",
			want: Command{Kind: KindSpike, Name: "/spike", Text: "fix the login bug"},
		},
		{
			name: "an autocapitalised mode command",
			body: "/Plan the work",
			want: Command{Kind: KindSpike, Name: "/plan", Text: "the work"},
		},

		// AC-2.20: the argument is missing.
		{
			name: "the body is exactly the command",
			body: "/spike",
			want: Command{Kind: KindSpikeEmpty, Name: "/spike"},
		},
		{
			name: "trailing whitespace is not an argument",
			body: "/spike   ",
			want: Command{Kind: KindSpikeEmpty, Name: "/spike"},
		},
		{
			name: "trailing blank lines are not an argument",
			body: "  /spike \n\n\t\n",
			want: Command{Kind: KindSpikeEmpty, Name: "/spike"},
		},

		// The active work commands retain their established mode behavior.
		{name: "research", body: "/research the failure mode", want: Command{Kind: KindSpike, Name: "/research", Text: "the failure mode"}},
		{name: "plan", body: "/plan", want: Command{Kind: KindSpikeEmpty, Name: "/plan"}},
		{name: "implement", body: "/implement T-001.2", want: Command{Kind: KindSpike, Name: "/implement", Text: "T-001.2"}},
		{name: "schedule is kept for factory grammar", body: "/schedule -by 02:00:00 /implement T-001.2", want: Command{Kind: KindSchedule, Name: "/schedule", Text: "-by 02:00:00 /implement T-001.2"}},

		// Graph Plan controls carry the whole body, which the Plan runtime matches exactly.
		{name: "promote a story set", body: "  /approve-plan set abc123 \n", want: Command{Kind: KindPlanControl, Name: "/approve-plan", Text: "/approve-plan set abc123"}},
		{name: "approve the assembled plan", body: "/approve-plan-set ref", want: Command{Kind: KindPlanControl, Name: "/approve-plan-set", Text: "/approve-plan-set ref"}},
		{name: "request a new story", body: "/plan-new-story", want: Command{Kind: KindPlanControl, Name: "/plan-new-story", Text: "/plan-new-story"}},

		// Retired commands have no special status: all are unknown commands.
		{name: "retired approve", body: "/approve", want: Command{Kind: KindUnknown, Name: "/approve"}},
		{name: "retired include", body: "/include docs/spec.md", want: Command{Kind: KindUnknown, Name: "/include"}},
		{name: "retired retry", body: "/retry", want: Command{Kind: KindUnknown, Name: "/retry"}},
		{name: "retired reset", body: "/reset", want: Command{Kind: KindUnknown, Name: "/reset"}},
		{name: "retired status", body: "/status", want: Command{Kind: KindUnknown, Name: "/status"}},

		// AC-2.18: unknown slash commands get the same treatment.
		{name: "an invented command", body: "/frobnicate the widget", want: Command{Kind: KindUnknown, Name: "/frobnicate"}},
		{name: "a near miss", body: "/spikey fix it", want: Command{Kind: KindUnknown, Name: "/spikey"}},
		{name: "a hyphenated command", body: "/dry-run", want: Command{Kind: KindUnknown, Name: "/dry-run"}},
		{name: "an underscored command", body: "/dry_run", want: Command{Kind: KindUnknown, Name: "/dry_run"}},
		{name: "a numbered command", body: "/v2", want: Command{Kind: KindUnknown, Name: "/v2"}},

		// Only a body with nothing in it is ignored.
		{name: "empty body", body: "", want: Command{Kind: KindNone}},
		{name: "whitespace only", body: " \t\n ", want: Command{Kind: KindNone}},

		// Free text. It names no command, so it spawns a run whose mode the
		// agent infers, and it carries the whole body as its instruction.
		{
			name: "prose",
			body: "This looks wrong to me.",
			want: Command{Kind: KindFreeText, Text: "This looks wrong to me."},
		},
		{
			name: "the body is trimmed but keeps its interior shape",
			body: "\n  draft some mockups\n\n- one per screen\n  ",
			want: Command{Kind: KindFreeText, Text: "draft some mockups\n\n- one per screen"},
		},

		// A /spike that is not the first token is still not a spike (AC-2.18).
		// It is now free text rather than nothing, which is the point of the
		// change: the comment is a request, it just is not that command.
		{
			name: "a spike that is not the first token does not trigger a spike",
			body: "Please /spike this when you get a chance",
			want: Command{Kind: KindFreeText, Text: "Please /spike this when you get a chance"},
		},
		{
			name: "a spike on a later line does not trigger a spike",
			body: "Some context first.\n/spike fix the login bug",
			want: Command{Kind: KindFreeText, Text: "Some context first.\n/spike fix the login bug"},
		},
		{
			name: "a quoted spike does not trigger a spike",
			body: "> /spike fix the login bug\n\nI would not do that.",
			want: Command{Kind: KindFreeText, Text: "> /spike fix the login bug\n\nI would not do that."},
		},
		{
			name: "a fenced spike does not trigger a spike",
			body: "```\n/spike fix the login bug\n```",
			want: Command{Kind: KindFreeText, Text: "```\n/spike fix the login bug\n```"},
		},
		{
			name: "an emoji before the command does not trigger a spike",
			body: "🚀 /spike fix the login bug",
			want: Command{Kind: KindFreeText, Text: "🚀 /spike fix the login bug"},
		},

		// A leading slash that is not a command token. None of these may be
		// reported as a command, so none of them may set Name.
		{
			name: "a leading path is not a command",
			body: "/usr/bin/security is on the PATH",
			want: Command{Kind: KindFreeText, Text: "/usr/bin/security is on the PATH"},
		},
		{name: "a bare slash", body: "/", want: Command{Kind: KindFreeText, Text: "/"}},
		{name: "a double slash", body: "// nope", want: Command{Kind: KindFreeText, Text: "// nope"}},
		{name: "a slash then punctuation", body: "/!!! what", want: Command{Kind: KindFreeText, Text: "/!!! what"}},
		{name: "a slash then a hyphen", body: "/-force it", want: Command{Kind: KindFreeText, Text: "/-force it"}},
		{
			name: "a non-Latin command word",
			body: "/спайк текст",
			want: Command{Kind: KindFreeText, Text: "/спайк текст"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := ParseCommand(tt.body)
			if got != tt.want {
				t.Errorf("ParseCommand(%q) = %+v, want %+v", tt.body, got, tt.want)
			}
			// Work commands carry a run instruction; schedules retain their
			// control text for the factory-only grammar and never spawn an agent.
			carriesText := got.Spawns() || got.Kind == KindSchedule
			if (got.Text != "") != carriesText {
				t.Errorf("ParseCommand(%q) = %+v: text is set exactly for work or scheduling controls", tt.body, got)
			}
			if got.Spawns() != (got.Kind == KindSpike || got.Kind == KindFreeText || got.Kind == KindPlanControl) {
				t.Errorf("ParseCommand(%q) = %+v: Spawns() disagrees with the kind", tt.body, got)
			}
			// Name is what a reply quotes back, so only a kind that really
			// named a command may set it.
			namedACommand := got.Kind != KindNone && got.Kind != KindFreeText
			if (got.Name != "") != namedACommand {
				t.Errorf("ParseCommand(%q) = %+v: name is set exactly when a command was named", tt.body, got)
			}
			if got.Spawns() && got.NeedsReply() {
				t.Errorf("ParseCommand(%q) = %+v: a command cannot both spawn and draw a reply", tt.body, got)
			}
		})
	}
}

func TestCommandDisposition(t *testing.T) {
	t.Parallel()

	tests := []struct {
		kind         CommandKind
		spawns       bool
		needsReply   bool
		kindAsString string
	}{
		{kind: KindNone, kindAsString: "none"},
		{kind: KindSpike, spawns: true, kindAsString: "spike"},
		{kind: KindFreeText, spawns: true, kindAsString: "free_text"},
		{kind: KindSpikeEmpty, needsReply: true, kindAsString: "spike_empty"},
		{kind: KindSchedule, kindAsString: "schedule"},
		{kind: KindUnknown, needsReply: true, kindAsString: "unknown"},
		{kind: KindPlanControl, spawns: true, kindAsString: "plan_control"},
	}

	for _, tt := range tests {
		t.Run(tt.kindAsString, func(t *testing.T) {
			t.Parallel()

			cmd := Command{Kind: tt.kind}
			if got := cmd.Spawns(); got != tt.spawns {
				t.Errorf("Command{%v}.Spawns() = %t, want %t", tt.kind, got, tt.spawns)
			}
			if got := cmd.NeedsReply(); got != tt.needsReply {
				t.Errorf("Command{%v}.NeedsReply() = %t, want %t", tt.kind, got, tt.needsReply)
			}
			if got := tt.kind.String(); got != tt.kindAsString {
				t.Errorf("CommandKind.String() = %q, want %q", got, tt.kindAsString)
			}
			// A command either starts a run or answers, never both: a spike
			// replies through its run report, not through the reply path.
			if cmd.Spawns() && cmd.NeedsReply() {
				t.Errorf("Command{%v} both spawns a run and asks for a reply", tt.kind)
			}
		})
	}
}

func TestActiveCommandsAreNotUnknown(t *testing.T) {
	t.Parallel()

	for _, name := range []string{SpikeCommand, "/research", "/plan", "/implement", "/validate", ScheduleCommand} {
		got := ParseCommand(name + " some argument")
		if got.Kind == KindUnknown {
			t.Errorf("ParseCommand(%q) = %v, want an active command", name, got.Kind)
		}
		if got.Name != name {
			t.Errorf("ParseCommand(%q) reported the name %q", name, got.Name)
		}
	}
	if got := ParseCommand(SpikeCommand + " x"); got.Kind != KindSpike {
		t.Errorf("%q classified as %v, want %v", SpikeCommand, got.Kind, KindSpike)
	}
}

// A Plan control always names Plan as its mode, whatever mode label the issue
// carries, so the promotion reaches the graph Plan runtime.
func TestPlanControlCommandsSelectPlan(t *testing.T) {
	t.Parallel()

	for _, name := range PlanControlCommands {
		got := ParseCommand(name + " argument")
		if got.Kind != KindPlanControl || got.Name != name {
			t.Errorf("ParseCommand(%q) = %+v, want a Plan control", name, got)
		}
		if mode, ok := got.Mode(); !ok || mode != agent.ModePlan {
			t.Errorf("%q selects mode %q, %t; want plan", name, mode, ok)
		}
	}
}

func TestParseCommandInlineCode(t *testing.T) {
	for _, body := range []string{
		"/approve-plan set ab853aa4bf79081a5330b2499672434e34f380bd18013b75a5df1392c2bef64f",
		"/plan investigate", "/implement this", "/validate this", "/validate-push abcdef012345", "/approve-bug analysis 1", "/doctor stop",
	} {
		for _, suffix := range []string{"", "\r\nfollowing text"} {
			want := ParseCommand(body + suffix)
			got := ParseCommand(" \n\t`" + body + "`" + suffix)
			if got != want {
				t.Errorf("%q: got %+v want %+v", body+suffix, got, want)
			}
		}
	}
	for _, body := range []string{"``/plan work``", "```/plan work```", "```\n/plan work\n```", "> `/plan work`", "`/plan work", "`/plan `work`", "Please `/plan work`", "`/plan work` please", "Please\n`/plan work`"} {
		if got := ParseCommand(body); got.Kind != KindFreeText {
			t.Errorf("%q gained command authority: %+v", body, got)
		}
	}
}
