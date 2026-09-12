package report

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/redact"
)

// A plan ships one GitHub sub-issue per user story (C1), and this file renders
// what one of those issues is made of: a title, a body, and the marker that
// identifies it again on a later run.
//
// It is here, in internal/report, for a reason that is not organisational.
// [sanitize] and [fenceFor] are unexported, so the only code that can put
// agent-authored text through them is code in this package — and a sub-issue
// body is agent-authored text going to the forge, which makes it exactly the
// kind of body this package exists to render safely (T-C1.11). Rendering it
// anywhere else would mean either exporting the two helpers or reimplementing
// them, and a second copy of the sanitiser is how the two drift.
//
// Everything else about a sub-issue — creating it, attaching it to its parent,
// and not creating it twice — is internal/github's, and that package never sees
// a body this one has not redacted.

// maxStoryTitleBytes is how much of a story title reaches the issue title.
//
// It is a readability budget, not a protocol limit: a title is one line of a
// GitHub issue list, and a story whose title needs more than this has a summary
// in the body for the rest. github.IssueTitleSizeLimit is the ceiling that
// actually refuses one, and it sits well above this — deliberately, because
// redaction runs after this cut and "[redacted]" is longer than most of what it
// replaces.
const maxStoryTitleBytes = 256

// titleEllipsis ends a title that was cut. It is a single rune so that a cut
// title cannot be mistaken for a short one.
const titleEllipsis = "…"

// storyIDPattern is the shape of a story id — "C1", "US-3", "E1a".
//
// The id is interpolated into the marker verbatim, so this is what keeps a
// marker from being closed early by its own contents: the accepted set has no
// "-->", no "<" and no newline. It is checked here rather than assumed of the
// caller for the same reason [profileNamePattern] is re-declared in reply.go —
// this package is the boundary where text litefactory did not write becomes a
// comment litefactory signs its name to.
var storyIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,39}$`)

// Story is everything [Reporter.RenderSubIssue] needs to render one user
// story's sub-issue.
//
// Like [Run] it is plain data assembled by the caller: this package holds no
// forge client, so it neither knows nor cares which issue number the story ends
// up as. Only [Story.Ref] and [Story.ID] are required — a story with no summary
// still gets an issue, because a story index row pointing at nothing is worse
// than a thin one.
type Story struct {
	// Ref is the task the plan was produced for: the parent issue the
	// sub-issue is attached to. It appears in the marker, so it must validate.
	Ref naming.Ref

	// ID is the story's id within the plan — "C1", "US-3". It appears in the
	// marker and at the front of the title, and it is what makes one story's
	// sub-issue findable again on a re-run, so it must match
	// [storyIDPattern].
	ID string

	// Title is the story's one-line title. It is rendered as the issue title,
	// prefixed with [Story.ID]. Empty produces a title naming the story id
	// alone rather than an untitled issue.
	Title string

	// Summary is the story in the business register: what the owner asked for
	// and what problem it solves, written to be read by someone who will not
	// open the plan (AC-C1.6).
	//
	// It is markdown, and it is rendered as markdown rather than quoted,
	// because its formatting is part of what it says. It is also written by a
	// model that read third-party issue comments, so it is sanitised and any
	// code fence it leaves open is closed — see [closeOpenFence].
	Summary string

	// Detail is the repository-relative path of the artefact holding the
	// technical detail for this story — the plan's own file. Empty renders no
	// detail line at all.
	//
	// It is a path rather than a URL because this package builds its links
	// itself, from the ref and a sha, and never from a string a caller could
	// point at another host (see link.go).
	Detail string

	// DetailSHA is the commit [Story.Detail] is linked at.
	//
	// Set it only for a commit that is on the remote. A permalink into a
	// commit that exists only in a local worktree is a guaranteed 404, and the
	// invitation to click it is what makes a dead link worse than none — the
	// same rule [Run.briefLink] applies to the brief. With no sha the path is
	// still named, as plain text, because "which file" is useful even when
	// "at which commit" is not yet answerable.
	DetailSHA string

	// Excerpt is the story's own text as it stands in the plan — its task
	// table, its acceptance criteria — copied out rather than rewritten.
	//
	// It is quoted verbatim inside a fence long enough to contain it, because
	// it is a copy of a technical artefact and its markup is not this comment's
	// to render. Empty renders no excerpt block, and it is the first thing
	// dropped if the body will not fit: it is a copy of text that is in the
	// plan already.
	Excerpt string
}

// SubIssue is a rendered sub-issue: everything internal/github needs to create
// one and to recognise it again.
//
// Both texts are [redact.Body] rather than strings, and for the same reason
// [Reporter.RenderRun]'s return value is: a title is outbound text like any
// other, and a token-shaped string in one would be just as public as one in a
// comment. Making the forge client take the type that only
// [redact.Redactor.Apply] can construct keeps "post something unredacted" from
// being an expressible program rather than a rule to remember (AC-4.6).
type SubIssue struct {
	// Title is the issue title.
	Title redact.Body

	// Body is the issue body. It begins with [Marker], at byte zero.
	Body redact.Body

	// Marker is the [StoryMarker] this story's sub-issue carries. It is
	// returned alongside the body rather than recomputed by the caller because
	// it is the identity the caller searches on, and a marker derived
	// separately from the body it is meant to identify is a marker that can
	// stop matching it.
	Marker string
}

// RenderSubIssue renders the sub-issue for one user story.
//
// A malformed task ref or a story id outside [storyIDPattern] is refused,
// because both are interpolated into the marker and a broken marker is worse
// than no comment: it is what tells litefactory its own output apart from an
// instruction. Everything else is allowed to be missing — a story with no
// summary, no detail path and no excerpt renders a short issue that says so,
// on the same reasoning as AC-4.1's "silence is not an outcome".
//
// The returned bodies have been through the S-7 pass and are within the
// configured limit.
//
// This renderer is dormant in production. The only caller is
// internal/turn/subissues.go's legacy file-discovery phase, and that phase
// returns before reaching this call whenever its SubIssueCreator satisfies
// turn.PlanForge — which the one SubIssueCreator ever wired into
// turn.Options (internal/daemon/setup.go's *github.Client) always does. The
// live sub-issue creation path, reached for both the legacy and typed
// content contracts, is turn.PublishPlan calling [Reporter.RenderCompleteStory]
// instead.
func (r *Reporter) RenderSubIssue(ctx context.Context, story Story) (SubIssue, error) {
	if err := story.Ref.Validate(); err != nil {
		return SubIssue{}, fmt.Errorf("report: %w: %w", ErrInvalidArgument, err)
	}
	if !storyIDPattern.MatchString(story.ID) {
		return SubIssue{}, fmt.Errorf("report: %w: story id %q is not a story id", ErrInvalidArgument, story.ID)
	}

	text, trimmed := r.subIssueBody(story)
	if trimmed {
		// Info rather than debug, as for a trimmed run report: a sub-issue that
		// does not carry the whole story is worth being able to explain later.
		r.log.LogAttrs(ctx, slog.LevelInfo, "trimmed sub-issue body to fit the comment size limit",
			slog.String("task", story.Ref.String()),
			slog.String("story", story.ID),
			slog.Int("bytes", len(text)),
			slog.Int("budget", r.budget),
		)
	}

	body, err := r.apply(ctx, text)
	if err != nil {
		return SubIssue{}, err
	}
	title, err := r.apply(ctx, storyTitle(story))
	if err != nil {
		return SubIssue{}, err
	}
	return SubIssue{Title: title, Body: body, Marker: StoryMarker(story.Ref, story.ID)}, nil
}

// storyTitle renders the issue title: the story id, then the story's own title.
//
// The id leads because the title is read in a list of a dozen siblings, where
// "which story is this" is the question, and because the story index in the plan
// refers to stories by id. The text is one-lined — a newline would end the title
// and start a body nobody asked for — and cut to [maxStoryTitleBytes] at a rune
// and token boundary, which is [cutAt]'s job.
func storyTitle(story Story) string {
	title := oneLine(story.Title)
	if title == "" {
		// No title at all still names the story, because an issue called
		// nothing is one nobody can refer to.
		title = "Story " + story.ID
	} else {
		title = story.ID + " — " + title
	}
	if len(title) <= maxStoryTitleBytes {
		return title
	}
	cut := cutAt(title, maxStoryTitleBytes-len(titleEllipsis))
	return strings.TrimRight(title[:cut], " \t") + titleEllipsis
}

// subIssueBody assembles the issue body and trims it to the budget, reporting
// whether it had to cut anything.
//
// The order of what goes is the same principle as a run report's (AC-4.8): the
// part that exists somewhere else goes first. The excerpt is a copy of the
// plan's own text, so it is dropped whole with a line saying where the original
// is; only after that is the summary — the part written for this reader — cut,
// and from the end, because its opening says what the story is.
func (r *Reporter) subIssueBody(story Story) (string, bool) {
	marker := StoryMarker(story.Ref, story.ID)
	summary := storySummary(story.Summary)
	detail := story.detailLine()
	excerpt := storyExcerpt(story.Excerpt)

	assemble := func(summaryText, excerptText string) string {
		sections := []string{marker, summaryText}
		if detail != "" {
			sections = append(sections, detail)
		}
		if excerptText != "" {
			sections = append(sections, excerptText)
		}
		// Every sub-issue is a comment somebody is about to answer, so it ends
		// the way every other body litefactory posts does (AC-S0.6).
		sections = append(sections, ReplyPlainlyNotice)
		return strings.Join(sections, sectionGap) + "\n"
	}

	if body := assemble(summary, excerpt); len(body) <= r.budget {
		return body, false
	}

	if excerpt != "" {
		excerpt = excerptDroppedNotice(story)
		if body := assemble(summary, excerpt); len(body) <= r.budget {
			return body, true
		}
	}

	// The summary, cut from the end. room is what is left once everything that
	// is never cut has been laid out; a body this pathological is one whose
	// marker and notice alone approach the budget, and redact.Apply's
	// hard-truncate is the backstop under this.
	room := r.budget - len(assemble("", excerpt)) - len(sectionGap)
	if room <= 0 {
		return assemble("", excerpt), true
	}
	summary, _ = truncateEnd(summary, storyCutNotice(story), room)
	return assemble(summary, excerpt), true
}

// storySummary renders the business-register half of the body.
//
// It is markdown written to be read, so it is rendered rather than quoted —
// the same treatment a run's final message gets, and for the same reason. An
// unbalanced fence in it would swallow the detail line and the reply notice
// underneath, so [closeOpenFence] closes one if it is left open.
func storySummary(text string) string {
	summary := closeOpenFence(strings.TrimSpace(sanitize(text)))
	if summary == "" {
		return "_The plan recorded no summary for this story._"
	}
	return summary
}

// storyExcerpt renders the verbatim block, or nothing when there is nothing to
// quote.
//
// The fence is sized to the content by [fenceFor]: an excerpt of a plan is full
// of code fences of its own, and a three-backtick fence around text containing
// three backticks lets that text out of the block and into the page as markup.
func storyExcerpt(text string) string {
	excerpt := strings.TrimRight(sanitize(text), "\n")
	if strings.TrimSpace(excerpt) == "" {
		return ""
	}
	fence := fenceFor(excerpt)

	var b strings.Builder
	b.WriteString("<details>\n<summary>")
	b.WriteString(excerptSummary)
	b.WriteString("</summary>\n\n")
	b.WriteString(fence)
	b.WriteString("text\n")
	b.WriteString(excerpt)
	b.WriteString("\n")
	b.WriteString(fence)
	b.WriteString("\n\n</details>")
	return b.String()
}

// excerptSummary is what the collapsed excerpt block is called.
const excerptSummary = "The plan's own text for this story"

// detailLead introduces the line pointing at the technical detail. It is a
// constant so the linked and unlinked forms cannot describe the same file two
// different ways.
const detailLead = "The technical detail for this story — its tasks, its acceptance criteria and " +
	"its architecture notes — is in "

// detailLine renders the pointer to the technical detail, or nothing when there
// is no usable path.
//
// A path that is not a plain relative path renders nothing at all rather than
// something approximate: [validRepoPath] is what keeps a path from closing the
// markdown link it sits inside or reaching outside the repository, and a path
// that fails it is not one this package will name.
func (s Story) detailLine() string {
	if !validRepoPath(s.Detail) {
		return ""
	}
	if url, ok := blobURL(s.Ref, s.DetailSHA, s.Detail); ok {
		return detailLead + "[`" + s.Detail + "`](" + url + ")."
	}
	// No sha, or one that is not an object id: the file is still named, but
	// nothing here is clickable, because the only link available would be
	// branch-relative and would show whatever the branch holds when it is
	// opened rather than what this story was written against.
	return detailLead + "`" + s.Detail + "`."
}

// excerptDroppedNotice replaces an excerpt there was no room for. It names the
// artefact when there is one to name, so a shortened issue never reads as a
// complete one.
func excerptDroppedNotice(story Story) string {
	if validRepoPath(story.Detail) {
		return "_The plan's own text for this story was omitted to fit the comment size limit. It is in `" +
			story.Detail + "`._"
	}
	return "_The plan's own text for this story was omitted to fit the comment size limit._"
}

// storyCutNotice ends a summary that had to be shortened.
func storyCutNotice(story Story) string {
	if validRepoPath(story.Detail) {
		return sectionGap + "_[litefactory: cut to fit the comment size limit. The whole story is in `" +
			story.Detail + "`.]_"
	}
	return sectionGap + "_[litefactory: cut to fit the comment size limit.]_"
}
