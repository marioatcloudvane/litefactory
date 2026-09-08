package team

import (
	"errors"
	"strings"
	"testing"
)

// TestValidateAgentEntry_ForbiddenFlag covers AC-1's index-level half: an
// agent entry whose Path or Desc smuggles in claude's permission-skipping
// flag is refused by the same check config.ContainsForbiddenFlag backs,
// wrapping ErrForbiddenFlag, before anything is ever rendered from it.
//
// The flag is assembled from fragments rather than written literally, so this
// file itself does not trip config's whole-tree
// TestForbiddenFlagIsAbsentFromTheTree scan.
func TestValidateAgentEntry_ForbiddenFlag(t *testing.T) {
	flag := "--dangerously-" + "skip-permissions"

	tests := []struct {
		name      string
		entry     AgentEntry
		wantField string
	}{
		{
			name:      "forbidden flag in source/reports source field",
			entry:     AgentEntry{Path: "agents/reviewer.md " + flag, Desc: "clean/dest.md"},
			wantField: "path",
		},
		{
			name:      "forbidden flag in dest/reports dest field",
			entry:     AgentEntry{Path: "clean/source.md", Desc: "agents/dest.md " + flag},
			wantField: "desc",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := validateAgentEntry("reviewer", tt.entry, "shared.reviewer")

			var found *ForbiddenFlagInIndexError
			for _, e := range errs {
				var ffie *ForbiddenFlagInIndexError
				if errors.As(e, &ffie) {
					found = ffie
					break
				}
			}
			if found == nil {
				t.Fatalf("errs = %v, want one *ForbiddenFlagInIndexError", errs)
			}
			if found.Field != tt.wantField {
				t.Errorf("ForbiddenFlagInIndexError.Field = %q, want %q", found.Field, tt.wantField)
			}
			if found.Path != "shared.reviewer" {
				t.Errorf("ForbiddenFlagInIndexError.Path = %q, want %q", found.Path, "shared.reviewer")
			}

			var joined error
			for _, e := range errs {
				joined = errors.Join(joined, e)
			}
			if !errors.Is(joined, ErrForbiddenFlag) {
				t.Errorf("errors.Is(errs, ErrForbiddenFlag) = false, want true")
			}
			if strings.Contains(found.Error(), flag) {
				t.Errorf("ForbiddenFlagInIndexError.Error() = %q, must not include the matched flag text", found.Error())
			}
		})
	}
}

// TestValidateAgentEntry_CleanEntry_ReturnsNoForbiddenFlagError is the
// negative case: an entry with an ordinary Path/Desc produces no
// forbidden-flag error at all, so the check is not simply always firing.
func TestValidateAgentEntry_CleanEntry_ReturnsNoForbiddenFlagError(t *testing.T) {
	entry := AgentEntry{Path: "agents/reviewer.md", Desc: ".claude/agents/reviewer.md"}

	errs := validateAgentEntry("reviewer", entry, "shared.reviewer")

	for _, e := range errs {
		if errors.Is(e, ErrForbiddenFlag) {
			t.Errorf("unexpected forbidden-flag error for a clean entry: %v", e)
		}
	}
}

// TestValidate_ForbiddenFlagInTeamOwnedAgent_ReportsDottedTeamPath proves the
// index-level check is wired all the way up through the public Validate
// entry point, not just reachable via the unexported helper directly, and
// that the dotted path it reports names the team an agent belongs to.
func TestValidate_ForbiddenFlagInTeamOwnedAgent_ReportsDottedTeamPath(t *testing.T) {
	flag := "--dangerously-" + "skip-permissions"
	idx := &Index{
		Teams: map[string]Entry{
			"go-team": {
				Plan: []AgentEntry{
					{ID: "go-architect", Path: "agents/go-architect.md " + flag, Desc: ".claude/agents/go-architect.md"},
				},
			},
		},
	}

	errs := Validate(idx)

	var found *ForbiddenFlagInIndexError
	for _, e := range errs {
		var ffie *ForbiddenFlagInIndexError
		if errors.As(e, &ffie) {
			found = ffie
			break
		}
	}
	if found == nil {
		t.Fatalf("Validate(idx) = %v, want one *ForbiddenFlagInIndexError", errs)
	}
	if found.Path != "teams.go-team.plan[0]" {
		t.Errorf("ForbiddenFlagInIndexError.Path = %q, want %q", found.Path, "teams.go-team.plan[0]")
	}
	if found.Field != "path" {
		t.Errorf("ForbiddenFlagInIndexError.Field = %q, want %q", found.Field, "path")
	}
}
