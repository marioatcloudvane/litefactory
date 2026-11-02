package report

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/redact"
)

var linearPartMatrix = []string{
	"MAR-60-001: Show what each uploaded invoice became",
	`a | b || c\|d`,
	"see https://evil.example/x, then [docs](https://docs.example/a) now",
	"www.evil.example evil.example a@b.co mailto:a@b.co <https://x.y>",
	"![i](http://evil.example/i.png) [click](javascript:alert)",
	"**b** _i_ ~~s~~ `c` <b>x</b> <!-- lf:marker --> @user #12",
	"# h",
	"> q",
	"- a",
	"1. d",
	"one\n---\n    code",
}

// legacyStoryText is a verbatim copy of storyText before the Linear mode.
func legacyStoryText(value string) string {
	var b strings.Builder
	for len(value) > 0 {
		link := storyLinkPattern.FindStringSubmatchIndex(value)
		bare := storyURLPattern.FindStringIndex(value)
		switch {
		case link != nil && (bare == nil || link[0] <= bare[0]):
			b.WriteString(legacyStoryEntities(value[:link[0]]))
			url := value[link[4]:link[5]]
			fmt.Fprintf(&b, "[%s](<%s>)", legacyStoryEntities(value[link[2]:link[3]]), url)
			value = value[link[1]:]
		case bare != nil:
			end := bare[1]
			for end > bare[0] && strings.ContainsRune(".,;:!?'", rune(value[end-1])) {
				end--
			}
			url := value[bare[0]:end]
			b.WriteString(legacyStoryEntities(value[:bare[0]]))
			fmt.Fprintf(&b, "[%s](<%s>)", url, url)
			value = value[end:]
		default:
			b.WriteString(legacyStoryEntities(value))
			value = ""
		}
	}
	return b.String()
}

func legacyStoryEntities(value string) string {
	var b strings.Builder
	for _, r := range value {
		switch {
		case r == '\n':
			b.WriteString("<br>")
		case r == '\r':
			b.WriteString("&#13;")
		case r < 32 || r == 127 || strings.ContainsRune("&<>|`*_[]{}()#+-!.\\", r):
			fmt.Fprintf(&b, "&#%d;", r)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

var (
	factoryStoryLink = regexp.MustCompile(`\[[^\]]*\]\(<https?://[^>\s]*>\)`)
	storyCodeSpan    = regexp.MustCompile("(`+)[^`]+?(`+)")
	storyActive      = regexp.MustCompile("(^|[^\\\\])[\\[\\]*_~<`|]")
	storyLinkLike    = regexp.MustCompile(`(?i)://|(^|\s)www\.|mailto:|@\S*\.|[a-z0-9-]+\.[a-z]{2,}`)
	storyBlockStart  = regexp.MustCompile(`^(#|>|-|\+|=|\d+[.)]| {4})`)
)

// AC-007 (field level) and AC-002: Linear story fields are inert apart from
// the factory's http(s) links; GitHub keeps the legacy entity bytes.
func TestLinearStoryTextIsInert(t *testing.T) {
	for _, in := range linearPartMatrix {
		if got, want := storyTextFor(in, false), legacyStoryText(in); got != want || storyText(in) != want {
			t.Errorf("GitHub storyText(%q) = %q, want %q", in, got, want)
		}
		got := storyTextFor(in, true)
		if strings.Contains(got, "&#") || strings.Contains(got, "<br>") || strings.Contains(got, "\n") {
			t.Errorf("Linear storyText(%q) = %q writes entities or breaks", in, got)
		}
		if storyBlockStart.MatchString(got) {
			t.Errorf("Linear storyText(%q) = %q can start a block", in, got)
		}
		rest := factoryStoryLink.ReplaceAllString(got, " ")
		rest = storyCodeSpan.ReplaceAllString(rest, " ")
		if m := storyActive.FindString(rest); m != "" {
			t.Errorf("Linear storyText(%q) = %q leaves active %q", in, got, m)
		}
		if m := storyLinkLike.FindString(rest); m != "" {
			t.Errorf("Linear storyText(%q) = %q leaves link-like %q outside code spans", in, got, m)
		}
	}
}

func linearPartStory() planbrief.TypedStory {
	s := planbrief.TypedStory{Key: "US-001", Intent: "create", Title: "Safe story",
		UserStory:    planbrief.UserStoryContent{Actor: "owner", Want: linearPartMatrix[0], Benefit: linearPartMatrix[2], Notes: linearPartMatrix},
		Tasks:        []planbrief.TaskContent{{ID: "T-001", Description: "Build it", OwnerRole: "engineer", CriterionIDs: []string{"AC-001"}, DependsOn: []string{}}},
		Architecture: planbrief.ArchitectureContent{Summary: linearPartMatrix[6], Constraints: linearPartMatrix},
		Dependencies: planbrief.DependencyContent{StoryKeys: []string{}, IssueRefs: []string{}},
	}
	for _, in := range linearPartMatrix {
		s.Architecture.Components = append(s.Architecture.Components, planbrief.ComponentContent{Name: in, Responsibility: in})
		s.Architecture.Decisions = append(s.Architecture.Decisions, planbrief.DecisionContent{Decision: in, Reason: in})
	}
	s.Architecture.Interfaces = []planbrief.InterfaceContent{{Name: "render", Input: linearPartMatrix[1], Output: linearPartMatrix[5]}}
	for i := 1; i <= 64; i++ {
		s.AcceptanceCriteria = append(s.AcceptanceCriteria, planbrief.CriterionContent{ID: fmt.Sprintf("AC-%03d", i), Text: strings.Repeat("x", 3900) + " " + linearPartMatrix[i%len(linearPartMatrix)]})
	}
	return s
}

// checkLinearParts asserts the joined part comments carry no entities, every
// table row keeps its header's cell count, and every field is inert text.
func checkLinearParts(t *testing.T, where string, parts []string, fields []string) {
	t.Helper()
	joined := strings.Join(parts, "")
	if strings.Contains(joined, "&#") {
		t.Errorf("%s: parts contain numeric entities", where)
	}
	cells := -1
	for _, line := range strings.Split(joined, "\n") {
		if !strings.HasPrefix(line, "|") {
			cells = -1
			continue
		}
		n := strings.Count(storyCodeSpan.ReplaceAllString(strings.ReplaceAll(line, `\|`, ""), " "), "|")
		if cells < 0 {
			cells = n
		} else if n != cells {
			t.Errorf("%s: row has %d pipes, header %d: %q", where, n, cells, line)
		}
	}
	for _, in := range fields {
		if !strings.Contains(joined, storyTextFor(in, true)) {
			t.Errorf("%s: parts lack the inert field %q", where, storyTextFor(in, true))
		}
	}
}

// AC-007: oversized typed story part comments on Linear read cleanly while the
// shortened issue body stays byte-identical.
func TestLinearTypedStoryParts(t *testing.T) {
	s := linearPartStory()
	scope := planbrief.ContentScope{Roles: []string{"engineer"}}
	body, parts, err := RenderTypedStoryPartsFor(s, scope, true)
	if err != nil || len(parts) == 0 {
		t.Fatalf("parts=%d err=%v", len(parts), err)
	}
	githubBody, githubParts, err := RenderTypedStoryParts(s, scope)
	if err != nil || body != githubBody {
		t.Fatalf("Linear changed the story body: %v", err)
	}
	legacy, legacyParts, _ := RenderTypedStoryPartsFor(s, scope, false)
	if legacy != githubBody || strings.Join(legacyParts, "\x00") != strings.Join(githubParts, "\x00") {
		t.Fatal("GitHub typed parts changed")
	}
	checkLinearParts(t, "typed parts", parts, linearPartMatrix)
}

// AC-007 for the legacy PlanDraft parts posted by the Plan story activity.
func TestLinearPlanDraftParts(t *testing.T) {
	d := planbrief.PlanDraft{Key: "story-a", Name: "Story Name", UserStory: planbrief.UserStoryContent{Actor: "owner", Want: "clear intent", Benefit: "shared understanding"},
		Problem: strings.Repeat("y", 70000) + " " + linearPartMatrix[2], InScope: linearPartMatrix, NotInScope: linearPartMatrix}
	red := redact.New(redact.Options{Environ: []string{}})
	header := func(n, total int) string { return fmt.Sprintf("<!-- part %d/%d -->\n", n, total) }
	in := PlanStoryInput{Parent: 362, Ordinal: 1, Draft: d, Stage: "story", Prefix: "<!-- markers -->\n", PartHeader: header, CommentLimit: 65536}
	github, err := PreparePlanStory(t.Context(), red, in)
	if err != nil || len(github.Parts) == 0 {
		t.Fatalf("parts=%d err=%v", len(github.Parts), err)
	}
	in.Linear = true
	linear, err := PreparePlanStory(t.Context(), red, in)
	if err != nil || linear.Body.String() != github.Body.String() || linear.Title.String() != github.Title.String() {
		t.Fatalf("Linear changed the story body or title: %v", err)
	}
	parts := make([]string, len(linear.Parts))
	for i, p := range linear.Parts {
		parts[i] = p.String()
	}
	checkLinearParts(t, "plan draft parts", parts, linearPartMatrix)
	if strings.Contains(strings.Join(parts, ""), legacyStoryText(linearPartMatrix[0])) {
		t.Error("Linear parts still carry GitHub entities")
	}
}
