package turn

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/linear/linearmd"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
)

// lmMatrix is the shared adversarial input matrix for every Linear caller.
var lmMatrix = []string{
	"MAR-60-001: Show what each uploaded invoice became",
	"Fix story-001: done.",
	`a | b || c\|d`,
	"https://evil.example/x",
	"www.evil.example",
	"evil.example",
	"a@b.co",
	"mailto:a@b.co",
	"<https://x.y>",
	"[click](https://evil.example)",
	"![i](https://evil.example/i.png)",
	"**b** _i_ ~~s~~ `c`",
	"<b>x</b> <!-- lf:implement-handoff -->",
	"@user #12",
	"see https://evil.example|x and `a|b`",
	"one\n# h\n> q\n- a\n+ b\n* c\n1. d\n2) e\n```\n~~~\n---\n***\n===\n    code\n| a | b |\n|---|---|",
}

var linearRef = naming.Ref{Forge: "linear", Owner: "cloudvane", Repo: "MAR", Issue: 60}
var githubRef = naming.Ref{Forge: "github", Owner: "cloudvane", Repo: "litefactory", Issue: 60}

const lmFactoryLink = "[MAR-62](https://linear.app/cloudvane/issue/MAR-62)"

// lmScan reads Markdown text: it resolves backslash escapes and code spans
// and reports unescaped pipes (cell boundaries), other active punctuation and
// the text outside code spans.
type lmScan struct {
	plain, outside string
	cells          []string // raw text split at unescaped pipes outside code spans
	active         []string
}

func lmRead(s string) lmScan {
	var out lmScan
	var plain, outside, cell strings.Builder
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == '\\' && i+1 < len(s) && strings.IndexByte("\\`*_[]<>~|#+-=.)!", s[i+1]) >= 0:
			plain.WriteByte(s[i+1])
			outside.WriteByte(s[i+1])
			cell.WriteString(s[i : i+2])
			i += 2
		case c == '`':
			n := 0
			for i+n < len(s) && s[i+n] == '`' {
				n++
			}
			end := lmClosing(s, i+n, n)
			if end < 0 {
				out.active = append(out.active, "`")
				cell.WriteString(s[i : i+n])
				i += n
				continue
			}
			content := s[i+n : end]
			if len(content) >= 2 && content[0] == ' ' && content[len(content)-1] == ' ' && strings.Trim(content, " ") != "" {
				content = content[1 : len(content)-1]
			}
			plain.WriteString(content)
			outside.WriteString(" ")
			cell.WriteString(s[i : end+n])
			i = end + n
		case c == '|':
			out.cells = append(out.cells, cell.String())
			cell.Reset()
			plain.WriteByte(c)
			outside.WriteByte(c)
			i++
		default:
			if strings.IndexByte("[]*_~<", c) >= 0 {
				out.active = append(out.active, string(c))
			}
			plain.WriteByte(c)
			outside.WriteByte(c)
			cell.WriteByte(c)
			i++
		}
	}
	out.cells = append(out.cells, cell.String())
	out.plain, out.outside = plain.String(), outside.String()
	return out
}

func lmClosing(s string, from, n int) int {
	for j := from; j+n <= len(s); j++ {
		if strings.Repeat("`", n) == s[j:j+n] && (j+n == len(s) || s[j+n] != '`') && s[j-1] != '`' {
			return j
		}
	}
	return -1
}

var lmLinkShape = regexp.MustCompile(`(?i)://|^www\.|^mailto:|@[^ ]*\.|[a-z0-9-]+\.[a-z]{2,}`)

// lmAssertInert checks one untrusted field as rendered: no entities, no active
// Markdown or link-like token outside code spans, and its literal text back.
func lmAssertInert(t *testing.T, where, rendered, input string) {
	t.Helper()
	if strings.Contains(rendered, "&#") {
		t.Errorf("%s: numeric entity in %q", where, rendered)
	}
	s := lmRead(rendered)
	if len(s.active) > 0 || len(s.cells) > 1 {
		t.Errorf("%s: active Markdown %q or cell break in %q", where, s.active, rendered)
	}
	for _, token := range strings.Fields(s.outside) {
		if lmLinkShape.MatchString(token) {
			t.Errorf("%s: link-like %q outside a code span in %q", where, token, rendered)
		}
	}
	if want := strings.Join(strings.Fields(input), " "); want != "" && !strings.Contains(s.plain, want) {
		t.Errorf("%s: reads %q, want literal %q", where, s.plain, want)
	}
}

// lmTable returns the table rows (lines starting with '|') after the header
// and rule, and checks every row has the expected cell count.
func lmTable(t *testing.T, where, body string, cells int) []lmScan {
	t.Helper()
	var rows []lmScan
	for _, line := range strings.Split(body, "\n") {
		if !strings.HasPrefix(line, "|") {
			continue
		}
		s := lmRead(line)
		// A row "| a | b |" splits into an empty lead, the cells and an empty tail.
		if got := len(s.cells) - 2; got != cells {
			t.Errorf("%s: row has %d cells, want %d: %q", where, got, cells, line)
		}
		rows = append(rows, s)
	}
	if len(rows) < 2 {
		t.Fatalf("%s: no table in\n%s", where, body)
	}
	return rows[2:]
}

// Verbatim copies of the GitHub escapers before the Linear branch was added.
func legacyEscapeImplementText(text string) string {
	var out strings.Builder
	for _, r := range text {
		switch {
		case r == '\n':
			out.WriteString("<br>\n")
		case strings.ContainsRune("&<>@#[]()!*_`~\\:./=-+|", r):
			fmt.Fprintf(&out, "&#%d;", r)
		default:
			out.WriteRune(r)
		}
	}
	return out.String()
}

func legacySafeImplementProgress(text string) string {
	text = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			return ' '
		}
		return r
	}, text)
	text = strings.Join(strings.Fields(text), " ")
	runes := []rune(text)
	if len(runes) > 300 {
		text = string(runes[:300]) + "..."
	}
	var out strings.Builder
	for _, r := range text {
		if strings.ContainsRune("&<>@#[]()!*_`~\\:./=-+|", r) {
			fmt.Fprintf(&out, "&#%d;", r)
		} else {
			out.WriteRune(r)
		}
	}
	return out.String()
}

func legacyBudgetImplementText(text string, budget int) string {
	text = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			return ' '
		}
		return r
	}, text)
	text = strings.Join(strings.Fields(text), " ")
	var out strings.Builder
	for _, r := range text {
		next := legacyEscapeImplementText(string(r))
		if out.Len()+len(next) > budget-3 {
			if budget >= 3 {
				out.WriteString("...")
			}
			break
		}
		out.WriteString(next)
	}
	return out.String()
}

func legacyPlanTableCell(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	if text == "" {
		return "—"
	}
	return strings.ReplaceAll(text, "|", `\|`)
}

// AC-002: every GitHub branch returns the legacy bytes over the matrix.
func TestLinearMatrixGitHubHelpersKeepLegacyBytes(t *testing.T) {
	for _, in := range append(lmMatrix, "", "  \n\t", strings.Repeat("long text. ", 60)) {
		if got, want := implementInline(false, in), legacyEscapeImplementText(in); got != want || escapeImplementText(in) != want {
			t.Errorf("implementInline(false, %q) = %q, want %q", in, got, want)
		}
		if got, want := implementBlock(false, in), legacyEscapeImplementText(in); got != want {
			t.Errorf("implementBlock(false, %q) = %q, want %q", in, got, want)
		}
		if got, want := safeImplementProgressFor(false, in), legacySafeImplementProgress(in); got != want || safeImplementProgress(in) != want {
			t.Errorf("safeImplementProgressFor(false, %q) = %q, want %q", in, got, want)
		}
		if got, want := planTableCellFor(false, in), legacyPlanTableCell(in); got != want || planTableCell(in) != want {
			t.Errorf("planTableCellFor(false, %q) = %q, want %q", in, got, want)
		}
		full := len(legacyBudgetImplementText(in, 1<<20))
		for budget := 0; budget <= full+3; budget++ {
			if got, want := budgetImplementTextFor(false, in, budget), legacyBudgetImplementText(in, budget); got != want || budgetImplementText(in, budget) != want {
				t.Fatalf("budgetImplementTextFor(false, %q, %d) = %q, want %q", in, budget, got, want)
			}
		}
	}
}

func lmAuthority(parent naming.Ref) (ImplementAuthority, []store.ImplementStory, []store.ImplementProgressReport) {
	at := time.Date(2026, 11, 1, 13, 0, 0, 0, time.UTC)
	a := ImplementAuthority{Parent: parent, Generation: 1}
	a.Stories = append(a.Stories, ImplementStory{Key: "story-001", Name: "MAR-60-001: Show what each uploaded invoice became", IssueNumber: 62})
	rows := []store.ImplementStory{{Key: "story-001", State: store.ImplementRunning, StateAt: at}}
	var progress []store.ImplementProgressReport
	for i, in := range lmMatrix {
		running := fmt.Sprintf("story-%03d", 100+i)
		parked := fmt.Sprintf("story-%03d", 200+i)
		a.Stories = append(a.Stories,
			ImplementStory{Key: running, Name: in, IssueNumber: 62},
			ImplementStory{Key: parked, Name: in, IssueNumber: 62, Dependencies: []string{running}})
		rows = append(rows,
			store.ImplementStory{Key: running, State: store.ImplementRunning, StateAt: at},
			store.ImplementStory{Key: parked, State: store.ImplementParked, Reason: in, StateAt: at})
		progress = append(progress, store.ImplementProgressReport{ID: int64(i + 1), Generation: 1, Parent: parent.String(), StoryKey: running, Kind: "implementation", Message: in, RecordedAt: at})
	}
	return a, rows, progress
}

// AC-001, AC-005: the Linear hand-off reads plain text, keeps three cells per
// row and its factory links, and lets no untrusted text become Markdown.
func TestLinearMatrixImplementHandoff(t *testing.T) {
	a, rows, progress := lmAuthority(linearRef)
	displays := map[string]implementDisplay{}
	notes := map[string]store.ReportDelivery{}
	for _, s := range a.Stories {
		displays[s.Key] = implementDisplay{Reference: lmFactoryLink, ReplyURL: "https://linear.app/cloudvane/issue/MAR-62#comment-" + s.Key}
		notes[s.Key] = store.ReportDelivery{CommentID: 7}
	}
	body, _ := renderImplementHandoffNotes(a, rows, progress, notes, displays)
	for _, want := range []string{"story-001 MAR-60-001: Show what each uploaded invoice became", "being built now."} {
		if !strings.Contains(body, want) {
			t.Errorf("hand-off lacks %q", want)
		}
	}
	if strings.Contains(body, "&#") {
		t.Errorf("hand-off contains numeric entities:\n%s", body)
	}
	table := lmTable(t, "hand-off", body, 3)
	if len(table) != len(a.Stories) {
		t.Fatalf("hand-off has %d rows, want %d", len(table), len(a.Stories))
	}
	for i, row := range table {
		story := a.Stories[i]
		label, info := row.cells[1], row.cells[3]
		reference := " (" + lmFactoryLink + ") "
		if !strings.HasSuffix(label, reference) {
			t.Errorf("row %s loses its ticket reference: %q", story.Key, label)
		}
		lmAssertInert(t, "hand-off story cell "+story.Key, strings.TrimSuffix(label, reference), story.Key+" "+story.Name)
		// Remove the factory-built links, then the rest of the cell is inert.
		for _, link := range []string{
			"[Full answer](" + displays[story.Key].ReplyURL + ")",
			"[blocking decision for " + story.Key + "](" + displays[story.Key].ReplyURL + ")",
		} {
			if rows[i].State == store.ImplementParked && !strings.Contains(info, link) {
				t.Errorf("parked row %s loses factory link %q: %q", story.Key, link, info)
			}
			info = strings.ReplaceAll(info, link, "")
		}
		want := rows[i].Reason
		if rows[i].State == store.ImplementRunning && i > 0 {
			want = "being built now. " + progress[(i-1)/2].Message
		}
		lmAssertInert(t, "hand-off information cell "+story.Key, info, want)
	}
	ga, grows, gprogress := lmAuthority(githubRef)
	github, _ := renderImplementHandoffNotes(ga, grows, gprogress, nil)
	for _, s := range a.Stories {
		if want := "| " + legacyEscapeImplementText(s.Key) + " " + legacySafeImplementProgress(s.Name) + " (#62) |"; !strings.Contains(github, want) {
			t.Errorf("GitHub hand-off lacks legacy cell %q", want)
		}
	}
}

// AC-008: Linear budget clipping cuts between whole units only.
func TestLinearMatrixBudgetClipping(t *testing.T) {
	for _, in := range lmMatrix {
		full := budgetImplementTextFor(true, in, 1<<20)
		if full != linearmd.InlineText(in) {
			t.Fatalf("unclipped Linear budget text %q, want %q", full, linearmd.InlineText(in))
		}
		for budget := 0; budget <= len(full); budget++ {
			got := budgetImplementTextFor(true, in, budget)
			kept := strings.TrimSuffix(got, "...")
			if !strings.HasPrefix(full, kept) || got != full && !strings.HasSuffix(got, "...") && budget >= 3 {
				t.Fatalf("budget %d of %q: %q is not a unit prefix of %q", budget, in, got, full)
			}
			units := strings.Join(linearmd.InlineUnits(in), "\x00")
			if kept != "" && !strings.HasPrefix(units, strings.Join(lmPrefixUnits(in, kept), "\x00")) {
				t.Fatalf("budget %d of %q cuts inside a unit: %q", budget, in, got)
			}
			s := lmRead("| " + got + " | x | y |")
			if len(s.active) > 0 || len(s.cells)-2 != 3 || strings.HasSuffix(kept, `\`) && !strings.HasSuffix(kept, `\\`) {
				t.Fatalf("budget %d of %q breaks the cell: %q (%q)", budget, in, got, s.active)
			}
		}
	}
}

// lmPrefixUnits returns the leading units of in that make up kept exactly.
func lmPrefixUnits(in, kept string) []string {
	var units []string
	n := 0
	for _, u := range linearmd.InlineUnits(in) {
		if n+len(u) > len(kept) {
			break
		}
		units = append(units, u)
		n += len(u)
	}
	if n != len(kept) {
		return []string{"\x01unit boundary missed"}
	}
	return units
}

// AC-004, AC-005: the other Implement and Validate texts on Linear.
func TestLinearMatrixParkAndValidateText(t *testing.T) {
	f, _, _ := parkNoteFixture(t)
	for _, in := range append(lmMatrix, "Fix story-001: done.") {
		body, err := renderImplementParkAnswerFor(t.Context(), f.opts.ResearchReporter, agentresult.ResearchNode{Response: "blocked", Result: agentresult.Result{Reason: in}}, true)
		mustProgress(t, err)
		_, text, ok := strings.Cut(body.String(), "Worker response: blocked\n\n")
		if !ok {
			t.Fatalf("park note lost its header: %q", body.String())
		}
		for _, line := range strings.Split(text, "\n") {
			if line == "" {
				continue
			}
			if regexp.MustCompile(`^(#|>|-|\+|=|\*|_|~|\||\d+[.)]| {4}|\t)`).MatchString(line) {
				t.Errorf("park note line can start a block: %q", line)
			}
			lmAssertInert(t, "park note line", line, "")
		}
		for _, line := range strings.Split(in, "\n") {
			if want := strings.Join(strings.Fields(line), " "); !strings.Contains(lmRead(text).plain, want) {
				t.Errorf("park note lost %q", want)
			}
		}
		if body, _ := renderImplementParkAnswerFor(t.Context(), f.opts.ResearchReporter, agentresult.ResearchNode{Response: "blocked", Result: agentresult.Result{Reason: in}}, false); !strings.Contains(body.String(), legacyEscapeImplementText(in)) {
			t.Errorf("GitHub park note lost legacy bytes for %q", in)
		}

		// The parked line of Validate's Implement guidance.
		lmAssertInert(t, "validate parked line", implementInline(true, in), in)

		st := ValidateState{Revision: "r1", Commit: strings.Repeat("c", 40)}
		st.Findings = []ValidateFinding{{ID: "story-002:F-001", Story: "story-002", Name: "F-001", Role: "code", Status: "fail", Summary: in, State: findingOpen, Blocking: true}}
		items := validateDecisionItemsFor(true, st)
		if len(items) != 1 {
			t.Fatalf("decision items %q", items)
		}
		head, _, _ := strings.Cut(items[0], "\n")
		_, summary, _ := strings.Cut(head, "`**: ")
		lmAssertInert(t, "validate decision item", summary, in)
		if github := validateDecisionItemsFor(false, st); strings.Join(github, "") != strings.Join(validateDecisionItems(st), "") || !strings.Contains(github[0], legacyEscapeImplementText(in)) {
			t.Errorf("GitHub decision item changed for %q", in)
		}

		comment := renderValidateStoryCommentFor(true, st, ValidateIssueRef{Key: "story-002"}, 60, lmFactoryLink)
		if strings.Contains(comment, "&#") {
			t.Errorf("validate story comment contains entities:\n%s", comment)
		}
		for _, line := range strings.Split(comment, "\n") {
			if _, reason, ok := strings.Cut(line, "`F-001` failed: "); ok {
				lmAssertInert(t, "validate story blocker", strings.TrimSuffix(reason, " It clears only on a new tested snapshot."), in)
			}
		}
		if !strings.Contains(lmRead(comment).plain, strings.Join(strings.Fields(in), " ")) {
			t.Errorf("validate story comment lost %q", in)
		}
		if renderValidateStoryCommentFor(false, st, ValidateIssueRef{Key: "story-002"}, 60) != renderValidateStoryComment(st, ValidateIssueRef{Key: "story-002"}, 60) {
			t.Errorf("GitHub story comment changed for %q", in)
		}
	}
	if !strings.Contains(implementInline(true, "Fix story-001: done."), "story-001: done.") {
		t.Error("Linear inline text writes entities for ordinary punctuation")
	}
}

// AC-006: Plan and Validate tables keep their shape, literal text and
// factory links on Linear.
func TestLinearMatrixPlanAndValidateTables(t *testing.T) {
	var rows []planStoryRow
	var summary [][4]string
	for i, in := range lmMatrix {
		rows = append(rows, planStoryRow{Key: fmt.Sprintf("story-%03d", i+1), Name: in, Description: in, Issue: 62, Reference: lmFactoryLink, Guidance: in, Command: "/approve-plan story x"})
		summary = append(summary, [4]string{in, issueLink(linearRef, 62, lmFactoryLink), "story", in})
	}
	check := func(where, body string, cells int, untrusted ...int) {
		table := lmTable(t, where, body, cells)
		if len(table) != len(lmMatrix) {
			t.Fatalf("%s: %d rows, want %d", where, len(table), len(lmMatrix))
		}
		for i, row := range table {
			for _, c := range untrusted {
				lmAssertInert(t, fmt.Sprintf("%s row %d cell %d", where, i, c), strings.TrimSpace(row.cells[c]), lmMatrix[i])
			}
		}
	}
	for _, stages := range []bool{false, true} {
		cells := 5
		if stages {
			cells = 6
		}
		body := renderPlanStoryTable(linearRef, rows, stages)
		check("plan story table", body, cells, 2, 3)
		if strings.Count(body, "| "+lmFactoryLink+" |") != len(rows) {
			t.Errorf("plan story table lost its issue links")
		}
		if renderPlanStoryTable(githubRef, rows, stages) != lmLegacyPlanStoryTable(githubRef, rows, stages) {
			t.Errorf("GitHub plan story table changed")
		}
	}
	check("plan waiting", planWaitingText(linearRef, rows), 5, 2, 5)
	check("plan summary", planSummaryTextFor(true, summary), 4, 1, 4)
	if planSummaryTextFor(false, summary) != planSummaryText(summary) || !strings.Contains(planSummaryText(summary), "| "+legacyPlanTableCell(lmMatrix[3])+" |") {
		t.Errorf("GitHub plan summary changed")
	}

	target := &ValidateDeliveryTarget{Task: linearRef, Parent: ValidateIssueRef{Number: 60}}
	st := ValidateState{Revision: "r1", Commit: strings.Repeat("c", 40), Assessed: true}
	st.Suite.Outcome = store.SuitePassed
	for i, in := range lmMatrix {
		key := fmt.Sprintf("story-%03d", i+1)
		st.Stories = append(st.Stories, ValidateJoinedStory{Story: key})
		st.Findings = append(st.Findings, ValidateFinding{ID: key + ":F-001", Story: key, Name: "F-001", Role: "code", Status: "fail", Summary: in, State: findingAwaitingCheck, Criterion: true, Blocking: true})
		target.Stories = append(target.Stories, ValidateIssueRef{Key: key, Title: in, Number: 62, Display: ticketDisplay{Label: "MAR-62", URL: "https://linear.app/cloudvane/issue/MAR-62"}})
	}
	body := renderValidateReport(st, target, "")
	if strings.Contains(body, "&#") {
		t.Errorf("validate report contains entities")
	}
	var table []lmScan
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "| ") && strings.Contains(line, "story-") {
			s := lmRead(line)
			if len(s.cells)-2 != 5 {
				t.Errorf("validate row has %d cells: %q", len(s.cells)-2, line)
			}
			table = append(table, s)
		}
	}
	if len(table) != len(lmMatrix) {
		t.Fatalf("validate report has %d story rows, want %d:\n%s", len(table), len(lmMatrix), body)
	}
	for i, row := range table {
		cell := strings.TrimSpace(row.cells[1])
		suffix := " (" + lmFactoryLink + ")"
		if !strings.HasSuffix(cell, suffix) || !strings.HasSuffix(strings.TrimSpace(row.cells[5]), " on "+lmFactoryLink) {
			t.Errorf("validate row %d loses its ticket links: %q / %q", i, cell, row.cells[5])
		}
		_, untrusted, _ := strings.Cut(strings.TrimSuffix(cell, suffix), " ")
		lmAssertInert(t, fmt.Sprintf("validate story cell %d", i), untrusted, fmt.Sprintf("story-%03d %s", i+1, lmMatrix[i]))
	}
	githubTarget := *target
	githubTarget.Task = githubRef
	for i := range githubTarget.Stories {
		githubTarget.Stories[i].Display = ticketDisplay{Label: "#62"}
	}
	github := renderValidateReport(st, &githubTarget, "")
	for i, in := range lmMatrix {
		if want := legacyPlanTableCell(fmt.Sprintf("story-%03d %s (#62)", i+1, in)); !strings.Contains(github, want) {
			t.Errorf("GitHub validate report lost legacy story cell %q", want)
		}
	}
}

// lmLegacyPlanStoryTable is the GitHub story table as written before the
// Linear branch, using the legacy cell escaper.
func lmLegacyPlanStoryTable(ref naming.Ref, rows []planStoryRow, stages bool) string {
	var out strings.Builder
	if stages {
		out.WriteString("| ID | Name | Description | Dependencies | Sub-issue | Approved stages |\n|---|---|---|---|---|---|\n")
	} else {
		out.WriteString("| ID | Name | Description | Dependencies | Sub-issue |\n|---|---|---|---|---|\n")
	}
	for _, row := range rows {
		fmt.Fprintf(&out, "| `%s` | %s | %s | %s | %s |", row.Key, legacyPlanTableCell(row.Name), legacyPlanTableCell(row.Description), dependencyCell(row.Dependencies), issueLink(ref, row.Issue, row.Reference))
		if stages {
			approved := "none"
			if len(row.Approved) > 0 {
				approved = strings.Join(row.Approved, ", ")
			}
			fmt.Fprintf(&out, " %s |", approved)
		}
		out.WriteString("\n")
	}
	return out.String()
}
