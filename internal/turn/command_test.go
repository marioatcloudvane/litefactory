package turn

import "testing"

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
			name: "an autocapitalised unsupported command",
			body: "/Plan the work",
			want: Command{Kind: KindUnsupported, Name: "/plan"},
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

		// AC-2.18: known but not yet supported. Each of these is a real
		// command in M1 or later.
		{name: "research", body: "/research the failure mode", want: Command{Kind: KindUnsupported, Name: "/research"}},
		{name: "plan", body: "/plan", want: Command{Kind: KindUnsupported, Name: "/plan"}},
		{name: "implement", body: "/implement T-001.2", want: Command{Kind: KindUnsupported, Name: "/implement"}},
		{name: "approve", body: "/approve", want: Command{Kind: KindUnsupported, Name: "/approve"}},
		{name: "include", body: "/include docs/spec.md", want: Command{Kind: KindUnsupported, Name: "/include"}},
		{name: "retry", body: "/retry", want: Command{Kind: KindUnsupported, Name: "/retry"}},
		{name: "reset", body: "/reset", want: Command{Kind: KindUnsupported, Name: "/reset"}},
		{name: "status", body: "/status", want: Command{Kind: KindUnsupported, Name: "/status"}},

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
			// A kind that spawns carries text and nothing else does: an
			// instruction with no instruction in it is the one shape the turn
			// cannot execute.
			if (got.Text != "") != got.Spawns() {
				t.Errorf("ParseCommand(%q) = %+v: text is set exactly when the command spawns", tt.body, got)
			}
			if got.Spawns() != (got.Kind == KindSpike || got.Kind == KindFreeText) {
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
		{kind: KindUnsupported, needsReply: true, kindAsString: "unsupported"},
		{kind: KindUnknown, needsReply: true, kindAsString: "unknown"},
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

// Every command in the known set must classify as unsupported rather than
// unknown, so adding one to the vocabulary without implementing it cannot
// silently change the reply an operator gets.
func TestKnownCommandsAreUnsupported(t *testing.T) {
	t.Parallel()

	for _, name := range knownCommands {
		got := ParseCommand(name + " some argument")
		if got.Kind != KindUnsupported {
			t.Errorf("ParseCommand(%q) = %v, want %v", name, got.Kind, KindUnsupported)
		}
		if got.Name != name {
			t.Errorf("ParseCommand(%q) reported the name %q", name, got.Name)
		}
	}
	if got := ParseCommand(SpikeCommand + " x"); got.Kind != KindSpike {
		t.Errorf("%q classified as %v, want %v", SpikeCommand, got.Kind, KindSpike)
	}
}
