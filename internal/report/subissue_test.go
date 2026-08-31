package report_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/secrets"
)

// testStory is the story every fixture here renders: the shape a plan produces
// once per user story.
func testStory() report.Story {
	return report.Story{
		Ref:   testRef,
		ID:    "C1",
		Title: "The plan ships as one GitHub sub-issue per user story",
		Summary: "Each user story arrives as its own issue, so it can be tracked, commented on and " +
			"closed on its own.\n\nToday a plan is one long document: there is nowhere to say " +
			"\"this part is done\" and nowhere to ask a question about one story without burying " +
			"it in a thread about all of them.",
		Detail:    testPlanPath,
		DetailSHA: "9c1e0a4b7d2f8e6a5c3b1d09f8e7a6b5c4d3e2f1",
		Excerpt: "| ID | Title | Type |\n" +
			"|----|-------|------|\n" +
			"| T-C1.3 | Create-issue capability, via the SDK | Go |\n",
	}
}

// testPlanPath is where the technical detail for the fixture story lives. It is
// written out rather than derived so that a change to the derivation has to be
// agreed with here too.
const testPlanPath = ".aiteam/lf-123-add-dry-run-to-task-clean/2026-05-20/" +
	"add-dry-run-to-task-clean/IMPLEMENTATION_PLAN.md"

// renderStory renders a sub-issue and fails on error.
func renderStory(t *testing.T, rep *report.Reporter, story report.Story) report.SubIssue {
	t.Helper()
	sub, err := rep.RenderSubIssue(t.Context(), story)
	if err != nil {
		t.Fatalf("RenderSubIssue: %v", err)
	}
	if !sub.Body.Valid() || !sub.Title.Valid() {
		t.Fatal("RenderSubIssue returned a Body that reports Valid() == false")
	}
	return sub
}

// TestSubIssueBeginsWithItsStoryMarker is the S-8 property for the third body
// shape. A sub-issue is a marker-bearing artefact the owner will reply to, so
// it has to be recognisable as litefactory's own — at byte zero, where the
// self-filter can see it and where a quote-reply cannot fake it.
func TestSubIssueBeginsWithItsStoryMarker(t *testing.T) {
	t.Parallel()

	sub := renderStory(t, newReporter(t), testStory())
	marker := report.StoryMarker(testRef, "C1")

	if sub.Marker != marker {
		t.Errorf("Marker = %q, want %q", sub.Marker, marker)
	}
	if !strings.HasPrefix(sub.Body.String(), marker) {
		t.Errorf("the sub-issue body does not begin with its story marker:\n%.160s", sub.Body.String())
	}
	if want := "<!-- lf:story task=github:acme/acme-api#123 story=C1 -->"; marker != want {
		t.Errorf("StoryMarker = %q, want %q — the marker carries the PARENT's ref and the story's id",
			marker, want)
	}
	if !strings.HasPrefix(marker, report.MarkerPrefix) {
		t.Errorf("StoryMarker %q does not begin with MarkerPrefix; the self-filter tests the prefix "+
			"and would not recognise this body as litefactory's own", marker)
	}
	// Exactly one marker in the body, and it is the first line: a second one
	// would put a marker somewhere other than byte zero.
	if got := strings.Count(sub.Body.String(), report.MarkerPrefix); got != 1 {
		t.Errorf("the body carries %d markers, want 1", got)
	}
}

// TestSubIssueTellsTheReaderHowToReply. A sub-issue is a second stream of
// marker-bearing comments the owner will answer, so it ends the way every other
// body litefactory posts does (AC-S0.6).
func TestSubIssueTellsTheReaderHowToReply(t *testing.T) {
	t.Parallel()

	body := renderStory(t, newReporter(t), testStory()).Body.String()
	if !strings.Contains(body, report.ReplyPlainlyNotice) {
		t.Errorf("the sub-issue does not say how to reply to it:\n%s", body)
	}
}

// TestSubIssueBody is the golden: the whole rendered issue, read as a human
// would read it. A diff here is the review.
func TestSubIssueBody(t *testing.T) {
	t.Parallel()

	sub := renderStory(t, newReporter(t), testStory())
	checkGolden(t, "subissue", sub.Body.String())

	containsAll(t, "the sub-issue body", sub.Body.String(),
		// The business register: the story, in the words a reader who will not
		// open the plan needs.
		"tracked, commented on and closed on its own",
		// And a link to where the technical detail is.
		"https://github.com/acme/acme-api/blob/9c1e0a4b7d2f8e6a5c3b1d09f8e7a6b5c4d3e2f1/"+testPlanPath,
	)
}

// TestSubIssueTitleLeadsWithTheStoryID. The title is read in a list of a dozen
// siblings, where "which story is this" is the question the list has to answer.
func TestSubIssueTitleLeadsWithTheStoryID(t *testing.T) {
	t.Parallel()
	rep := newReporter(t)

	tests := []struct {
		name  string
		story func(report.Story) report.Story
		want  string
	}{
		{
			name:  "id then title",
			story: func(s report.Story) report.Story { return s },
			want:  "C1 — The plan ships as one GitHub sub-issue per user story",
		},
		{
			name:  "no title still names the story",
			story: func(s report.Story) report.Story { s.Title = ""; return s },
			want:  "Story C1",
		},
		{
			name: "a title with a line break is one line",
			story: func(s report.Story) report.Story {
				s.Title = "first line\nsecond line"
				return s
			},
			want: "C1 — first line second line",
		},
		{
			name: "control characters are stripped",
			story: func(s report.Story) report.Story {
				s.Title = "\x1b[31mred\x00 title"
				return s
			},
			want: "C1 — red title",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			sub := renderStory(t, rep, tc.story(testStory()))
			if got := sub.Title.String(); got != tc.want {
				t.Errorf("title = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestALongTitleIsCutToSomethingSendable. GitHub does not publish a maximum
// title length, so this package cuts to a length that still reads as a title
// and github.CreateIssue refuses anything beyond its own ceiling — the two
// together mean a verbose plan cannot turn into a 422 nobody can explain.
func TestALongTitleIsCutToSomethingSendable(t *testing.T) {
	t.Parallel()

	story := testStory()
	story.Title = strings.Repeat("an unreasonably wordy story title ", 40)

	sub := renderStory(t, newReporter(t), story)
	title := sub.Title.String()

	if len(title) > github.IssueTitleSizeLimit {
		t.Errorf("title is %d bytes, over github.IssueTitleSizeLimit (%d)",
			len(title), github.IssueTitleSizeLimit)
	}
	if len(title) > 300 {
		t.Errorf("title is %d bytes; it is supposed to be cut to a readable length", len(title))
	}
	if !strings.HasSuffix(title, "…") {
		t.Errorf("a cut title does not say it was cut: %q", title)
	}
	if !strings.HasPrefix(title, "C1 — an unreasonably wordy") {
		t.Errorf("title = %q, want the story id and the beginning of the title", title)
	}
}

// TestRenderSubIssueRejectsUnrenderableInput. Both halves of the marker are
// interpolated verbatim, so both are refused when they could close the HTML
// comment early — a body whose marker is broken is worse than no body at all,
// because the marker is what tells litefactory its own output from an
// instruction.
func TestRenderSubIssueRejectsUnrenderableInput(t *testing.T) {
	t.Parallel()
	rep := newReporter(t)

	tests := []struct {
		name  string
		story report.Story
	}{
		{"empty ref", report.Story{ID: "C1"}},
		{"ref that could close the comment", report.Story{
			Ref: naming.Ref{Forge: "github", Owner: "acme --> x", Repo: "api", Issue: 1},
			ID:  "C1",
		}},
		{"no story id", report.Story{Ref: testRef}},
		{"story id that closes the comment", report.Story{Ref: testRef, ID: "C1 -->"}},
		{"story id with a line break", report.Story{Ref: testRef, ID: "C1\nC2"}},
		{"story id with markup", report.Story{Ref: testRef, ID: "<b>C1</b>"}},
		{"story id that is only punctuation", report.Story{Ref: testRef, ID: "-"}},
		{"story id far too long", report.Story{Ref: testRef, ID: strings.Repeat("C", 41)}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			sub, err := rep.RenderSubIssue(t.Context(), tc.story)
			if !errors.Is(err, report.ErrInvalidArgument) {
				t.Fatalf("RenderSubIssue error = %v, want ErrInvalidArgument", err)
			}
			if sub.Body.Valid() || sub.Title.Valid() {
				t.Error("a failed render returned a postable Body")
			}
		})
	}
}

// TestAThinStoryStillRendersAnIssue. A story index row pointing at nothing is
// worse than a thin issue, so everything except the two halves of the marker is
// allowed to be missing — the same reasoning AC-4.1 applies to a run report.
func TestAThinStoryStillRendersAnIssue(t *testing.T) {
	t.Parallel()

	sub := renderStory(t, newReporter(t), report.Story{Ref: testRef, ID: "C1"})
	body := sub.Body.String()

	if !strings.HasPrefix(body, report.StoryMarker(testRef, "C1")) {
		t.Errorf("the body does not begin with its marker:\n%s", body)
	}
	if !strings.Contains(body, "no summary") {
		t.Errorf("the body does not say the plan recorded no summary:\n%s", body)
	}
	if sub.Title.String() != "Story C1" {
		t.Errorf("title = %q, want %q", sub.Title.String(), "Story C1")
	}
}

// TestStoryTextCannotBreakOutOfItsBlock is AC-C1.9's structural half. The
// summary is a model's markdown and the excerpt is a copy of a document full of
// code fences; neither may take the rest of the issue with it.
func TestStoryTextCannotBreakOutOfItsBlock(t *testing.T) {
	t.Parallel()

	story := testStory()
	// A fence the summary opens and never closes. CommonMark runs it to the end
	// of the document, which would swallow the detail line and the reply notice.
	story.Summary = "The story, in prose.\n\n```go\nfunc unfinished() {\n"
	// And an excerpt that contains a fence of its own plus the closing tag of
	// the block it sits in.
	story.Excerpt = "```\nnot the end\n```\n</details>\n### not a heading\n"

	body := renderStory(t, newReporter(t), story).Body.String()

	// The excerpt's fence is longer than any run of backticks inside it.
	open := strings.Index(body, "````text\n")
	closing := strings.LastIndex(body, "````")
	if open < 0 || closing <= open {
		t.Fatalf("the excerpt does not use a widened fence:\n%s", body)
	}
	if quoted := strings.Index(body, "</details>"); quoted < open || quoted > closing {
		t.Errorf("the quoted </details> at %d is not inside the fence (%d..%d)", quoted, open, closing)
	}

	// The summary's unclosed fence was closed, so everything below it is still
	// itself rather than code.
	if !strings.Contains(body, report.ReplyPlainlyNotice) {
		t.Errorf("the reply notice was swallowed by the summary's open fence:\n%s", body)
	}
	if !strings.Contains(body, testPlanPath) {
		t.Errorf("the detail line was swallowed by the summary's open fence:\n%s", body)
	}
	if !strings.HasSuffix(strings.TrimRight(body, "\n"), report.ReplyPlainlyNotice) {
		t.Errorf("the body does not end where it should:\n%s", body)
	}
}

// TestControlCharactersAreStrippedFromASubIssue. GitHub answers a body that is
// not valid UTF-8 with a 422 that says nothing about what was wrong, and a
// half-stripped ANSI escape renders as visible junk.
func TestControlCharactersAreStrippedFromASubIssue(t *testing.T) {
	t.Parallel()

	story := testStory()
	story.Summary = "\x1b[31mred\x1b[0m story\x00 and \xff invalid\r\n"
	story.Excerpt = "\x1b[1mbold\x1b[0m excerpt\x00\r\n"

	body := renderStory(t, newReporter(t), story).Body.String()
	for _, bad := range []string{"\x1b", "\x00", "\r", "[31m", "[1m"} {
		if strings.Contains(body, bad) {
			t.Errorf("body still contains %q", bad)
		}
	}
	containsAll(t, "the body", body, "red story and", "bold excerpt")
}

// TestTokenShapedStoryTextNeverReachesTheForge is AC-C1.9's redaction half.
//
// A sub-issue body is agent-authored text going to a public forge, so it goes
// through the same S-7 pass as every other outbound body — and so does the
// title, which is quoted in every notification GitHub sends about the issue and
// is exactly the place a leak would be least expected.
func TestTokenShapedStoryTextNeverReachesTheForge(t *testing.T) {
	t.Parallel()

	const botToken = "ghp_liveBotTokenValue0000000000000000"
	const envSecret = "s3cret-deploy-key-value"

	rep := newReporter(t, func(o *redact.Options) {
		o.Token = secrets.NewToken(botToken)
		o.Environ = []string{"DEPLOY_TOKEN=" + envSecret}
	})

	story := testStory()
	story.Title = "Rotate " + envSecret + " everywhere"
	story.Summary = "The deploy key " + envSecret + " is in three places.\n\nAKIAIOSFODNN7EXAMPLE\n"
	story.Excerpt = "curl -H 'Authorization: Bearer " + botToken + "'\n"

	sub := renderStory(t, rep, story)
	for _, where := range []struct{ what, text string }{
		{"the body", sub.Body.String()},
		{"the title", sub.Title.String()},
	} {
		for _, secret := range []string{botToken, envSecret, "AKIAIOSFODNN7EXAMPLE"} {
			if strings.Contains(where.text, secret) {
				t.Errorf("%s contains the credential %q", where.what, secret)
			}
		}
	}
	if !strings.Contains(sub.Body.String(), "[redacted]") {
		t.Error("nothing in the body was replaced, so the fixture never reached the pass")
	}
	if sub.Body.Counts().Total() == 0 {
		t.Error("no redactions were counted")
	}
}

// TestOversizeSubIssueDropsTheExcerptFirst. What goes first is what exists
// somewhere else: the excerpt is a copy of the plan's own text, and the summary
// is the part written for this reader.
func TestOversizeSubIssueDropsTheExcerptFirst(t *testing.T) {
	t.Parallel()

	story := testStory()
	story.Summary = "The story, stated once.\n\n" + strings.Repeat("summary line\n", 200)
	story.Excerpt = strings.Repeat("| T-C1.3 | a task | Go |\n", 6_000)

	sub := renderStory(t, newReporter(t), story)
	body := sub.Body.String()

	if sub.Body.Len() > github.CommentSizeLimit {
		t.Errorf("body is %d bytes, over the %d-byte limit", sub.Body.Len(), github.CommentSizeLimit)
	}
	if strings.Contains(body, "| T-C1.3 | a task | Go |") {
		t.Error("the excerpt survived; it is the first thing that should go")
	}
	if !strings.Contains(body, "The story, stated once.") {
		t.Error("the summary was cut before the excerpt was")
	}
	containsAll(t, "the trimmed body", body,
		"omitted to fit the comment size limit",
		// The notice names where the original is, so a shortened issue never
		// reads as a complete one.
		testPlanPath,
		report.ReplyPlainlyNotice,
	)
}

// TestAnOversizeSummaryIsCutAndSaysSo. Below the excerpt there is only the
// summary left to cut, and it is cut from the end because its opening is what
// says what the story is.
func TestAnOversizeSummaryIsCutAndSaysSo(t *testing.T) {
	t.Parallel()

	story := testStory()
	story.Excerpt = ""
	story.Summary = "The story, stated once.\n\n" + strings.Repeat("summary line\n", 8_000)

	sub := renderStory(t, newReporter(t), story)
	body := sub.Body.String()

	if sub.Body.Len() > github.CommentSizeLimit {
		t.Errorf("body is %d bytes, over the %d-byte limit", sub.Body.Len(), github.CommentSizeLimit)
	}
	containsAll(t, "the cut body", body,
		"The story, stated once.",
		"cut to fit the comment size limit",
		testPlanPath,
		report.ReplyPlainlyNotice,
	)
	if !strings.HasPrefix(body, report.StoryMarker(testRef, "C1")) {
		t.Error("the marker did not survive the cut")
	}
}

// TestTheDetailLinkIsHonest. The same three rules the brief link follows: it is
// pinned to a commit, it is refused when it cannot be built honestly, and a
// path that is not a plain relative path is not named at all.
func TestTheDetailLinkIsHonest(t *testing.T) {
	t.Parallel()
	rep := newReporter(t)

	tests := []struct {
		name       string
		story      func(report.Story) report.Story
		wantLink   bool
		wantPath   bool
		wantAbsent string
	}{
		{
			name:     "pinned to the sha it was given",
			story:    func(s report.Story) report.Story { return s },
			wantLink: true,
			wantPath: true,
		},
		{
			name:     "no sha names the file but links nothing",
			story:    func(s report.Story) report.Story { s.DetailSHA = ""; return s },
			wantPath: true,
		},
		{
			name: "a sha that is not an object id links nothing",
			story: func(s report.Story) report.Story {
				s.DetailSHA = "HEAD"
				return s
			},
			wantPath: true,
		},
		{
			name: "a path that could escape the repository is not named",
			story: func(s report.Story) report.Story {
				s.Detail = "../../../etc/passwd"
				return s
			},
			wantAbsent: "etc/passwd",
		},
		{
			name: "a path that could close the link is not named",
			story: func(s report.Story) report.Story {
				s.Detail = "plan.md)](https://evil.example.com"
				return s
			},
			wantAbsent: "evil.example.com",
		},
		{
			name:  "no path at all",
			story: func(s report.Story) report.Story { s.Detail = ""; return s },
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			body := renderStory(t, rep, tc.story(testStory())).Body.String()
			hasLink := strings.Contains(body, "https://github.com/acme/acme-api/blob/")
			if hasLink != tc.wantLink {
				t.Errorf("body links the detail = %v, want %v:\n%s", hasLink, tc.wantLink, body)
			}
			if got := strings.Contains(body, testPlanPath); got != tc.wantPath {
				t.Errorf("body names the plan path = %v, want %v", got, tc.wantPath)
			}
			if tc.wantAbsent != "" && strings.Contains(body, tc.wantAbsent) {
				t.Errorf("body names %q, which is not a path this package will render:\n%s",
					tc.wantAbsent, body)
			}
		})
	}
}

// No rendered sub-issue escapes this package as a string, for the reason the
// other two renderers do not: a title is as public as a body, and the type is
// what makes "send something unredacted" inexpressible rather than forbidden.
//
// Changing either field of report.SubIssue to a string breaks this file.
var (
	_ func(*report.Reporter, context.Context, report.Story) (report.SubIssue, error) = (*report.Reporter).RenderSubIssue
	_ redact.Body                                                                    = report.SubIssue{}.Title
	_ redact.Body                                                                    = report.SubIssue{}.Body
)
