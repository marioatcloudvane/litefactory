package messaging_test

import (
	"regexp"
	"slices"
	"testing"
	"unicode/utf8"

	"github.com/cloudvane/litefactory/internal/messaging"
)

// discordName is Discord's documented CHAT_INPUT name pattern restricted to
// lowercase, which Discord requires wherever a lowercase variant exists.
var discordName = regexp.MustCompile(`^[-_\p{Ll}\p{N}\p{Devanagari}\p{Thai}]{1,32}$`)

// TestDeclarationsMeetDiscordLimits checks every command a fully enabled
// Service registers against Discord's documented application command limits,
// because Discord rejects the whole bulk registration for one bad command.
func TestDeclarationsMeetDiscordLimits(t *testing.T) {
	s := messaging.New(database(t), "test", "owner", []string{"channel"}, &sender{}, nil)
	s.EnableAsk(nil)
	s.EnableFeatures(nil, nil)
	s.EnableResearch(nil, nil)
	defs := s.Declarations()
	if len(defs) == 0 || len(defs) > 100 {
		t.Fatalf("command count %d outside 1-100", len(defs))
	}
	for _, def := range defs {
		if !discordName.MatchString(def.Name) {
			t.Errorf("command %q: name breaks Discord's name rule", def.Name)
		}
		if n := utf8.RuneCountInString(def.Description); n < 1 || n > 100 {
			t.Errorf("command %q: description length %d outside 1-100", def.Name, n)
		}
		if len(def.Arguments) > 25 {
			t.Errorf("command %q: %d options exceed 25", def.Name, len(def.Arguments))
		}
		seen := map[string]bool{}
		optional := ""
		for _, arg := range def.Arguments {
			if !discordName.MatchString(arg.Name) {
				t.Errorf("command %q option %q: name breaks Discord's name rule", def.Name, arg.Name)
			}
			if n := utf8.RuneCountInString(arg.Description); n < 1 || n > 100 {
				t.Errorf("command %q option %q: description length %d outside 1-100", def.Name, arg.Name, n)
			}
			if seen[arg.Name] {
				t.Errorf("command %q: duplicate option %q", def.Name, arg.Name)
			}
			seen[arg.Name] = true
			if !arg.Required {
				if optional == "" {
					optional = arg.Name
				}
			} else if optional != "" {
				t.Errorf("command %q: required option %q follows optional %q", def.Name, arg.Name, optional)
			}
		}
	}
	want := map[string][]string{
		"question": {"issue", "project"},
		"diagnose": {"issue", "project", "note"},
		"answer":   {"issue", "revision", "text", "project"},
	}
	for _, def := range defs {
		order, ok := want[def.Name]
		if !ok {
			continue
		}
		var names []string
		for _, arg := range def.Arguments {
			names = append(names, arg.Name)
		}
		if !slices.Equal(names, order) {
			t.Errorf("command %q options %v, want %v", def.Name, names, order)
		}
		delete(want, def.Name)
	}
	if len(want) != 0 {
		t.Errorf("missing research commands: %v", want)
	}
}
