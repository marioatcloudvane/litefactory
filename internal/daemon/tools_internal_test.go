package daemon

import (
	"testing"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/report"
)

// testProfiles is the operator's configuration every resolveTools test in this
// file reads. It carries two profiles so that "two labels, both well-formed"
// is a real ambiguity between two things that could each have resolved on
// their own, and it copies to distinct addresses per test so a bug in the
// copy-on-read path can never leak between table cases.
func testProfiles() map[string]config.ToolProfile {
	return map[string]config.ToolProfile{
		"research": {Claude: &config.ToolProfileClaude{Tools: []string{"Read", "Grep", "Glob"}, AllowedTools: []string{"Read"}}},
		"build":    {Claude: &config.ToolProfileClaude{Tools: []string{"Read", "Edit", "Bash"}, AllowedTools: nil}},
	}
}

// TestResolveTools_DecisionTable pins the resolver's decision table (AC-2.2,
// AC-3.1 through AC-3.5, D-5, D-6, US-2): every shape of label a maintainer
// can put on an issue, and exactly one of the outcomes resolveTools may reach
// for it — including the old lf:tools= syntax, which is checked before the
// "Tools: " ladder runs at all and refuses unconditionally regardless of its
// casing, its value, or how many such labels are present.
//
// It calls resolveTools directly, on a *Daemon built with nothing but the
// tool configuration, because the decision is a pure function of the labels
// and the operator's profile map — nothing else about the daemon's wiring is
// relevant to it, and going through a whole tick per case would be the same
// test with the database and the forge server on the critical path instead
// of off it.
func TestResolveTools_DecisionTable(t *testing.T) {
	d := &Daemon{toolProfiles: testProfiles(), toolCeiling: []string{"Read", "Edit", "Bash", "Grep", "Glob"}}

	tests := []struct {
		name         string
		labels       []string
		wantRefusal  report.ReplyKind
		wantProfile  string
		wantMiscased bool
		wantResolved string // profile name the decision must carry Tools/AllowedTools for
	}{
		{
			name:         "one valid label/resolves the profile",
			labels:       []string{"Tools: Research"},
			wantResolved: "research",
		},
		{
			name:         "an unrelated label beside a valid one/resolves, ignoring the unrelated label",
			labels:       []string{"bug", "Tools: Research"},
			wantResolved: "research",
		},
		{
			name:        "mis-cased prefix, well-formed value/malformed, prefix flagged",
			labels:      []string{"tools: Research"},
			wantRefusal: report.ReplyMalformedToolLabel, wantMiscased: true,
		},
		{
			name:        "fully uppercase prefix/malformed, prefix flagged",
			labels:      []string{"TOOLS: Research"},
			wantRefusal: report.ReplyMalformedToolLabel, wantMiscased: true,
		},
		{
			name:        "mis-cased prefix over an unconfigured value/still the prefix failure, not unknown",
			labels:      []string{"tools: Nosuch"},
			wantRefusal: report.ReplyMalformedToolLabel, wantMiscased: true,
		},
		{
			name:        "byte-exact prefix, lower-case-first value/malformed on the value shape, prefix not flagged",
			labels:      []string{"Tools: research"},
			wantRefusal: report.ReplyMalformedToolLabel, wantMiscased: false,
		},
		{
			name:        "empty value/malformed, prefix not flagged",
			labels:      []string{"Tools: "},
			wantRefusal: report.ReplyMalformedToolLabel, wantMiscased: false,
		},
		{
			name:        "value with a space/malformed, prefix not flagged",
			labels:      []string{"Tools: Bad name"},
			wantRefusal: report.ReplyMalformedToolLabel, wantMiscased: false,
		},
		{
			name:        "well-formed value naming no configured profile/unknown, names the profile",
			labels:      []string{"Tools: Nosuch"},
			wantRefusal: report.ReplyUnknownToolProfile, wantProfile: "nosuch",
		},
		{
			name: "no labels at all/the zero value: no refusal, no profile",
		},
		{
			name:   "labels present, none of them a tools label/the zero value",
			labels: []string{"bug", "lf:reply", "priority:high"},
		},
		{
			name:   "the wrong key, Tool: /prefix mismatch, ignored entirely",
			labels: []string{"Tool: Research"},
		},
		{
			name:   "no colon at all/prefix mismatch, ignored entirely",
			labels: []string{"Tools"},
		},
		{
			name:        "two valid labels, same casing/ambiguous, neither is chosen",
			labels:      []string{"Tools: Research", "Tools: Build"},
			wantRefusal: report.ReplyAmbiguousToolLabels,
		},
		{
			name:        "two labels, mixed casing and reversed order/ambiguous regardless of order or casing",
			labels:      []string{"tools: Build", "Tools: Research"},
			wantRefusal: report.ReplyAmbiguousToolLabels,
		},
		{
			name:        "a single legacy label refuses loudly rather than resolving or no-oping (US-2)",
			labels:      []string{"lf:tools=research"},
			wantRefusal: report.ReplyLegacyToolLabel,
		},
		{
			name:        "a mis-cased legacy label still refuses/detected case-insensitively like the new prefix (US-2)",
			labels:      []string{"LF:Tools=research"},
			wantRefusal: report.ReplyLegacyToolLabel,
		},
		{
			name:        "an unconfigured or malformed value after the legacy prefix still refuses the same way (US-2)",
			labels:      []string{"lf:tools=bad name"},
			wantRefusal: report.ReplyLegacyToolLabel,
		},
		{
			name:        "two legacy labels are not counted for ambiguity, refused on the first (US-2)",
			labels:      []string{"lf:tools=research", "lf:tools=build"},
			wantRefusal: report.ReplyLegacyToolLabel,
		},
		{
			name:        "a legacy label beside a well-formed new-style label refuses for the legacy one (US-2)",
			labels:      []string{"lf:tools=research", "Tools: Build"},
			wantRefusal: report.ReplyLegacyToolLabel,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			decision := d.resolveTools(tc.labels)

			if decision.Refusal != tc.wantRefusal {
				t.Errorf("Refusal = %s, want %s", decision.Refusal, tc.wantRefusal)
			}
			if decision.refused() != (tc.wantRefusal != report.ReplyInvalid) {
				t.Errorf("refused() = %t, disagrees with Refusal = %s", decision.refused(), decision.Refusal)
			}
			if decision.MiscasedPrefix != tc.wantMiscased {
				t.Errorf("MiscasedPrefix = %t, want %t", decision.MiscasedPrefix, tc.wantMiscased)
			}
			// S-9: MiscasedPrefix is set on exactly the prefix-caused malformed
			// refusal, never on the value-caused one and never on ambiguous — the
			// two malformed causes have to stay distinguishable in the logs.
			if tc.wantMiscased && tc.wantRefusal != report.ReplyMalformedToolLabel {
				t.Fatalf("test table error: %s wants MiscasedPrefix without wanting the malformed refusal", tc.name)
			}

			switch {
			case tc.wantResolved != "":
				if decision.Profile != tc.wantResolved {
					t.Errorf("Profile = %q, want the resolved %q", decision.Profile, tc.wantResolved)
				}
				profile := testProfiles()[tc.wantResolved]
				if len(decision.Tools) == 0 {
					t.Fatalf("Tools is empty for a resolved profile")
				}
				if got, want := decision.Tools, profile.Claude.Tools; !equalStrings(got, want) {
					t.Errorf("Tools = %v, want the profile's %v", got, want)
				}
				if got, want := decision.AllowedTools, profile.Claude.AllowedTools; !equalStrings(got, want) {
					t.Errorf("AllowedTools = %v, want the profile's %v", got, want)
				}
				if decision.Resolved == nil {
					t.Fatalf("Resolved is nil for a resolved profile")
				}
				if decision.Resolved.Claude == nil {
					t.Errorf("Resolved.Claude is nil, want the profile's own claude stanza")
				}
			case tc.wantProfile != "":
				if decision.Profile != tc.wantProfile {
					t.Errorf("Profile = %q, want %q", decision.Profile, tc.wantProfile)
				}
				if len(decision.Tools) != 0 || len(decision.AllowedTools) != 0 {
					t.Errorf("an unresolved decision carries tools: %v / %v", decision.Tools, decision.AllowedTools)
				}
			default:
				// The zero value, the malformed refusal and the ambiguous refusal
				// all share this: Profile stays empty, because there is no field
				// for label text to travel in (S-9), and no tools resolved.
				if decision.Profile != "" {
					t.Errorf("Profile = %q, want empty: this decision must not name a label", decision.Profile)
				}
				if len(decision.Tools) != 0 || len(decision.AllowedTools) != 0 {
					t.Errorf("this decision carries tools: %v / %v, want none", decision.Tools, decision.AllowedTools)
				}
			}
		})
	}
}

// TestResolveTools_CopiesProfileSlices is AC-2.9. The map is the operator's
// own configuration and it lives for the whole process; a resolved decision's
// slices have to be independent copies, because the harness request they end
// up on is mutated in place on the cold-rerun path (harvest.go:126-129), and
// one append into shared spare capacity there would corrupt the profile for
// every later run this process makes.
func TestResolveTools_CopiesProfileSlices(t *testing.T) {
	profiles := testProfiles()
	original := append([]string(nil), profiles["research"].Claude.Tools...)
	d := &Daemon{toolProfiles: profiles, toolCeiling: []string{"Read", "Edit", "Bash", "Grep", "Glob"}}

	decision := d.resolveTools([]string{"Tools: Research"})

	// Index mutation: proves the two slices do not share a backing array at
	// all, regardless of what append happens to have done internally.
	decision.Tools[0] = "TAMPERED"
	decision.AllowedTools = append(decision.AllowedTools, "TAMPERED-TOO")

	// Growing past the copy's own length: if resolveTools had ever handed back
	// a slice with spare capacity into the profile's own array, this append
	// would silently overwrite the config's next element instead of
	// allocating a new one.
	for range 8 {
		decision.Tools = append(decision.Tools, "MORE")
	}

	if got := profiles["research"].Claude.Tools; !equalStrings(got, original) {
		t.Errorf("the operator's profile changed after the resolved decision was mutated: %v, want %v",
			got, original)
	}

	// A second resolution reads the map fresh and must be unaffected by
	// whatever the first decision's slices were put through.
	again := d.resolveTools([]string{"Tools: Research"})
	if !equalStrings(again.Tools, original) {
		t.Errorf("a later resolution of the same profile = %v, want the untouched %v", again.Tools, original)
	}
}

// TestResolveTools_ProfileWithNoClaudeStanzaResolvesWithEmptyToolsButKeepsResolved
// is T-7.2's own coverage of resolveTools: a profile that carries only a
// codex: stanza is not refused at this stage — resolveTools does not know
// which agent the run addresses, so it resolves what the config actually
// says and leaves the per-agent refusal to stage 3 (tick.go's run()). This
// pins the boundary: Tools/AllowedTools (the Claude-specific view) come back
// empty, but Resolved still carries the codex: stanza untouched, proving the
// two are never conflated.
func TestResolveTools_ProfileWithNoClaudeStanzaResolvesWithEmptyToolsButKeepsResolved(t *testing.T) {
	profiles := map[string]config.ToolProfile{
		"codex-only": {Codex: &config.ToolProfileCodex{Sandbox: "read-only"}},
	}
	d := &Daemon{toolProfiles: profiles, toolCeiling: []string{"Read"}}

	decision := d.resolveTools([]string{"Tools: Codex-only"})

	if decision.refused() {
		t.Fatalf("resolveTools refused a profile with only a codex: stanza; that decision belongs to stage 3, not here: %s", decision.Refusal)
	}
	if decision.Profile != "codex-only" {
		t.Errorf("Profile = %q, want %q", decision.Profile, "codex-only")
	}
	if len(decision.Tools) != 0 || len(decision.AllowedTools) != 0 {
		t.Errorf("Tools/AllowedTools = %v/%v, want both empty: this profile has no claude: stanza to derive them from",
			decision.Tools, decision.AllowedTools)
	}
	if decision.Resolved == nil {
		t.Fatal("Resolved is nil for a resolved profile")
	}
	if decision.Resolved.Claude != nil {
		t.Errorf("Resolved.Claude = %#v, want nil: this profile never wrote a claude: stanza", decision.Resolved.Claude)
	}
	if decision.Resolved.Codex == nil || decision.Resolved.Codex.Sandbox != "read-only" {
		t.Errorf("Resolved.Codex = %#v, want the codex: stanza as configured", decision.Resolved.Codex)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
