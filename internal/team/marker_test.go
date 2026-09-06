package team

import (
	"errors"
	"testing"
)

func TestUpsertMarkerSectionAppendsToEmptyContent(t *testing.T) {
	out := upsertMarkerSection(nil, []string{"go-team"}, "go-architect", []byte("hello\n"))
	want := "<!-- litefactory:team BEGIN team=go-team agent=go-architect -->\nhello\n<!-- litefactory:team END -->\n"
	if string(out) != want {
		t.Errorf("upsertMarkerSection(nil, ...) = %q, want %q", out, want)
	}
}

func TestUpsertMarkerSectionAppendsAfterExistingContent(t *testing.T) {
	existing := []byte("# Some Root File\n\nSome human-written content.\n")
	out := upsertMarkerSection(existing, []string{"go-team"}, "go-architect", []byte("hello\n"))

	want := "# Some Root File\n\nSome human-written content.\n\n" +
		"<!-- litefactory:team BEGIN team=go-team agent=go-architect -->\nhello\n<!-- litefactory:team END -->\n"
	if string(out) != want {
		t.Errorf("upsertMarkerSection = %q, want %q", out, want)
	}
}

func TestUpsertMarkerSectionReplacesInPlaceLeavingSurroundingContentUntouched(t *testing.T) {
	before := "# Root\n\nHuman content above.\n\n" +
		"<!-- litefactory:team BEGIN team=go-team agent=go-architect -->\nold body\n<!-- litefactory:team END -->\n\n" +
		"Human content below.\n"

	out := upsertMarkerSection([]byte(before), []string{"go-team"}, "go-architect", []byte("new body\n"))

	want := "# Root\n\nHuman content above.\n\n" +
		"<!-- litefactory:team BEGIN team=go-team agent=go-architect -->\nnew body\n<!-- litefactory:team END -->\n\n" +
		"Human content below.\n"
	if string(out) != want {
		t.Errorf("upsertMarkerSection replace = %q, want %q", out, want)
	}
}

func TestUpsertMarkerSectionSortsAndDedupsTeamList(t *testing.T) {
	out := upsertMarkerSection(nil, []string{"other-team", "go-team", "go-team"}, "shared-agent", []byte("body\n"))
	want := "<!-- litefactory:team BEGIN team=go-team,other-team agent=shared-agent -->\nbody\n<!-- litefactory:team END -->\n"
	if string(out) != want {
		t.Errorf("upsertMarkerSection team list = %q, want %q", out, want)
	}
}

func TestLocateMarkerSectionByAgentKeyIgnoresTeamListChanges(t *testing.T) {
	content := []byte("<!-- litefactory:team BEGIN team=old-team agent=go-architect -->\nbody\n<!-- litefactory:team END -->\n")
	start, end, found := locateMarkerSection(content, "go-architect")
	if !found {
		t.Fatal("locateMarkerSection did not find a section by agent key alone")
	}
	if start != 0 || end != len(content) {
		t.Errorf("locateMarkerSection span = [%d, %d), want [0, %d)", start, end, len(content))
	}
}

func TestRemoveMarkerSectionRemovesSpanAndCollapsesBlankLines(t *testing.T) {
	before := "# Root\n\nAbove.\n\n" +
		"<!-- litefactory:team BEGIN team=go-team agent=go-architect -->\nbody\n<!-- litefactory:team END -->\n\n" +
		"Below.\n"

	out, err := removeMarkerSection([]byte(before), "AGENTS.md", "go-architect")
	if err != nil {
		t.Fatalf("removeMarkerSection: %v", err)
	}
	want := "# Root\n\nAbove.\n\nBelow.\n"
	if string(out) != want {
		t.Errorf("removeMarkerSection = %q, want %q", out, want)
	}
}

func TestRemoveMarkerSectionNotFound(t *testing.T) {
	content := []byte("# Root\n\nNo sections here.\n")
	_, err := removeMarkerSection(content, "AGENTS.md", "go-architect")
	if !errors.Is(err, ErrMarkerNotFound) {
		t.Fatalf("error = %v, want ErrMarkerNotFound", err)
	}
	var mnf *MarkerNotFoundError
	if !errors.As(err, &mnf) {
		t.Fatalf("error = %v, want *MarkerNotFoundError", err)
	}
	if mnf.RootFile != "AGENTS.md" || mnf.Agent != "go-architect" {
		t.Errorf("MarkerNotFoundError = %+v, want RootFile=AGENTS.md Agent=go-architect", mnf)
	}
}

func TestMarkerSpanHashChangesWithTeamListEvenIfBodyUnchanged(t *testing.T) {
	h1 := markerSpanHash([]string{"go-team"}, "go-architect", []byte("body\n"))
	h2 := markerSpanHash([]string{"go-team", "other-team"}, "go-architect", []byte("body\n"))
	if h1 == h2 {
		t.Error("markerSpanHash did not change when the contributing team list changed with the same body")
	}
}

func TestMarkerSpanHashStableUnderTeamOrderAndDuplicates(t *testing.T) {
	h1 := markerSpanHash([]string{"go-team", "other-team"}, "go-architect", []byte("body\n"))
	h2 := markerSpanHash([]string{"other-team", "go-team", "go-team"}, "go-architect", []byte("body\n"))
	if h1 != h2 {
		t.Error("markerSpanHash should be stable regardless of input team order/duplicates")
	}
}

func TestMarkerSpanHashUnaffectedByContentOutsideMarkerSpan(t *testing.T) {
	teams := []string{"go-team"}
	agentKey := "go-architect"
	body := []byte("body\n")
	direct := markerSpanHash(teams, agentKey, body)

	// Two full root files differ only in content surrounding the shared
	// agent's marker span: one has nothing else in it, the other has
	// unrelated human-written content both before and after the span.
	bare := upsertMarkerSection(nil, teams, agentKey, body)
	surrounded := upsertMarkerSection([]byte("# Root\n\nSome preamble.\n"), teams, agentKey, body)
	surrounded = append(surrounded, []byte("\nSome unrelated trailer content.\n")...)

	for name, content := range map[string][]byte{"bare": bare, "surrounded": surrounded} {
		start, end, found := locateMarkerSection(content, agentKey)
		if !found {
			t.Fatalf("%s: locateMarkerSection did not find the section", name)
		}
		span := content[start:end]
		got := HashBytes(span)
		if got != direct {
			t.Errorf("%s: hash of extracted span = %q, want markerSpanHash result %q (content outside the marker span must not affect the hash)", name, got, direct)
		}
	}
}
