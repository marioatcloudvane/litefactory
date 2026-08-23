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

		// AC-2.19: free text. Recorded, ignored, no reply.
		{name: "empty body", body: "", want: Command{Kind: KindNone}},
		{name: "whitespace only", body: " \t\n ", want: Command{Kind: KindNone}},
		{name: "prose", body: "This looks wrong to me.", want: Command{Kind: KindNone}},
		{
			name: "a spike that is not the first token does not trigger",
			body: "Please /spike this when you get a chance",
			want: Command{Kind: KindNone},
		},
		{
			name: "a spike on a later line does not trigger",
			body: "Some context first.\n/spike fix the login bug",
			want: Command{Kind: KindNone},
		},
		{
			name: "a quoted spike does not trigger",
			body: "> /spike fix the login bug\n\nI would not do that.",
			want: Command{Kind: KindNone},
		},
		{
			name: "a fenced spike does not trigger",
			body: "```\n/spike fix the login bug\n```",
			want: Command{Kind: KindNone},
		},
		{
			name: "an emoji before the command does not trigger",
			body: "🚀 /spike fix the login bug",
			want: Command{Kind: KindNone},
		},
		{
			name: "a leading path is not a command",
			body: "/usr/bin/security is on the PATH",
			want: Command{Kind: KindNone},
		},
		{name: "a bare slash", body: "/", want: Command{Kind: KindNone}},
		{name: "a double slash", body: "// nope", want: Command{Kind: KindNone}},
		{name: "a slash then punctuation", body: "/!!! what", want: Command{Kind: KindNone}},
		{name: "a slash then a hyphen", body: "/-force it", want: Command{Kind: KindNone}},
		{name: "a non-Latin command word", body: "/спайк текст", want: Command{Kind: KindNone}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := ParseCommand(tt.body)
			if got != tt.want {
				t.Errorf("ParseCommand(%q) = %+v, want %+v", tt.body, got, tt.want)
			}
			// Only an actionable spike carries text, and only an actionable
			// spike may start a run.
			if (got.Text != "") != (got.Kind == KindSpike) {
				t.Errorf("ParseCommand(%q) = %+v: text is set exactly when the kind is %v", tt.body, got, KindSpike)
			}
			if got.Spawns() != (got.Kind == KindSpike) {
				t.Errorf("ParseCommand(%q) = %+v: Spawns() disagrees with the kind", tt.body, got)
			}
			if (got.Name == "") != (got.Kind == KindNone) {
				t.Errorf("ParseCommand(%q) = %+v: name is empty exactly when the kind is %v", tt.body, got, KindNone)
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
