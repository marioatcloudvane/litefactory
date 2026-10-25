package report_test

import (
	"github.com/cloudvane/litefactory/internal/agent"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/report"
)

func TestLegacyToolsRefusalIsActionableAndEchoesNoPayload(t *testing.T) {
	t.Parallel()
	rep := newReporter(t)

	body, err := rep.RenderReply(t.Context(), report.Reply{Ref: testRef, Kind: report.ReplyLegacyToolLabel})
	if err != nil {
		t.Fatalf("RenderReply: %v", err)
	}
	text := body.String()
	containsAll(t, "legacy refusal", text, "no longer reads", "Remove every", "resolved run mode", "Nothing was run")
	if !strings.HasPrefix(text, report.ReplyMarker(testRef)) {
		t.Errorf("reply does not begin with the reply marker:\n%s", text)
	}
	if strings.Contains(text, "run=") {
		t.Error("a run-less reply carries a run attribute")
	}
	for _, forbidden := range []string{"Tools: <Profile>", "research-profile", "sentinel"} {
		if strings.Contains(text, forbidden) {
			t.Errorf("reply contains retired or payload text %q:\n%s", forbidden, text)
		}
	}
}

// A comment written while Research worked between two questions is refused in
// plain words: it arrived while Research was working, was not used, and should
// be resent in reply to the open question (#378).
func TestGraphReplyBetweenQuestionsRefusalSaysNotUsedAndResend(t *testing.T) {
	t.Parallel()
	rep := newReporter(t)

	body, err := rep.RenderReply(t.Context(), report.Reply{Ref: testRef, Kind: report.ReplyGraphReplyBetweenQuestions})
	if err != nil {
		t.Fatalf("RenderReply: %v", err)
	}
	text := body.String()
	if !strings.HasPrefix(text, report.ReplyMarker(testRef)) {
		t.Errorf("reply does not begin with the reply marker:\n%s", text)
	}
	if strings.Contains(text, "run=") {
		t.Error("a run-less reply carries a run attribute")
	}
	containsAll(t, "between-questions refusal", text,
		"arrived while the workflow was still working",
		"before its current question or review was posted",
		"it was not used",
		"nothing was run for it",
		"post it again as a reply to the open question or review",
	)
}

// A Bug comment that cannot be applied is answered through the mode-refusal
// path, which consumes it, and tells the owner how to approve.
func TestRenderReplyBugCommentNotApplied(t *testing.T) {
	rep := newReporter(t)
	body, err := rep.RenderReply(t.Context(), report.Reply{Ref: testRef, Kind: report.ReplyBugCommentNotApplied})
	if err != nil {
		t.Fatalf("RenderReply: %v", err)
	}
	text := body.String()
	if !strings.HasPrefix(text, report.ReplyMarker(testRef)) || !strings.Contains(text, "/approve-bug analysis <revision>") || !report.ReplyBugCommentNotApplied.RefusesMode() {
		t.Fatalf("reply:\n%s", text)
	}
}

// A Research comment refused because the wait already holds another reply is
// answered neutrally through the mode-refusal path, which consumes it.
func TestRenderReplyGraphOwnerWaitRefused(t *testing.T) {
	rep := newReporter(t)
	body, err := rep.RenderReply(t.Context(), report.Reply{Ref: testRef, Kind: report.ReplyGraphOwnerWaitRefused})
	if err != nil {
		t.Fatalf("RenderReply: %v", err)
	}
	text := body.String()
	if !strings.HasPrefix(text, report.ReplyMarker(testRef)) || !strings.Contains(text, "already handling another reply or question") || !strings.Contains(text, "already answered") || !strings.Contains(text, "nothing was run") {
		t.Fatalf("reply:\n%s", text)
	}
	kind := report.ReplyGraphOwnerWaitRefused
	if kind.String() != "graph_owner_wait_refused" || !kind.RefusesMode() {
		t.Fatalf("kind = %q, RefusesMode = %v", kind.String(), kind.RefusesMode())
	}
}

// A refused delivery command is answered through the mode-refusal path, which
// consumes it, and says nothing was recorded, pushed, merged or closed (#922).
func TestRenderReplyCodeControlRefused(t *testing.T) {
	rep := newReporter(t)
	kind := report.ReplyCodeControlRefused
	body, err := rep.RenderReply(t.Context(), report.Reply{Ref: testRef, Kind: kind})
	if err != nil {
		t.Fatalf("RenderReply: %v", err)
	}
	text := body.String()
	for _, want := range []string{report.ReplyMarker(testRef), "set aside", "nothing was recorded, pushed, merged or closed", "only the task's owner"} {
		if !strings.Contains(text, want) {
			t.Errorf("reply lacks %q:\n%s", want, text)
		}
	}
	if kind.String() != "code_control_refused" || !kind.RefusesMode() || kind.RefusesTools() || kind.RefusesAgent() || kind.RefusesSwitch() || kind.RefusesSync() || kind.IsConfirmationReply() {
		t.Fatalf("kind = %q in the wrong refusal family", kind.String())
	}
}

func TestRenderReplyImplementCommentNotApplied(t *testing.T) {
	rep := newReporter(t)
	kind := report.ReplyImplementCommentNotApplied
	body, err := rep.RenderReply(t.Context(), report.Reply{Ref: testRef, Kind: kind})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{report.ReplyMarker(testRef), "Implement", "did not select any parked stories", "no stories were resumed", "name no existing story key"} {
		if !strings.Contains(body.String(), want) {
			t.Errorf("reply lacks %q: %s", want, body.String())
		}
	}
	if !kind.RefusesMode() || kind.RefusesTools() || kind.RefusesAgent() || kind.RefusesSwitch() || kind.RefusesSync() || kind.IsConfirmationReply() {
		t.Fatal("wrong refusal family")
	}
}

// A reply that could not resume stopped work names the node and the reason,
// and drops either one that is not plain text.
func TestRenderReplyGraphResumeRefused(t *testing.T) {
	rep := newReporter(t)
	kind := report.ReplyGraphResumeRefused
	body, err := rep.RenderReply(t.Context(), report.Reply{Ref: testRef, Kind: kind, ResumeNode: "repair_agent", ResumeReason: "newer work on this issue has replaced it"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{report.ReplyMarker(testRef), "could not resume the stopped work at `repair_agent` because newer work on this issue has replaced it", "nothing was run"} {
		if !strings.Contains(body.String(), want) {
			t.Errorf("reply lacks %q: %s", want, body.String())
		}
	}
	body, err = rep.RenderReply(t.Context(), report.Reply{Ref: testRef, Kind: kind, ResumeNode: "a`b", ResumeReason: "@someone [x](y)"})
	if err != nil {
		t.Fatal(err)
	}
	if text := body.String(); strings.Contains(text, "a`b") || strings.Contains(text, "@someone") || !strings.Contains(text, "could not resume the stopped work, so") {
		t.Fatalf("unsafe detail rendered: %s", text)
	}
	if kind.String() != "graph_resume_refused" || !kind.RefusesMode() || kind.RefusesTools() || kind.RefusesAgent() || kind.RefusesSwitch() || kind.RefusesSync() || kind.IsConfirmationReply() {
		t.Fatal("wrong refusal family")
	}
}

func TestMissingModeMappingReply(t *testing.T) {
	t.Parallel()
	rep := newReporter(t)
	for _, mode := range []string{"research", "spike", "<!-- injected -->"} {
		body, err := rep.RenderReply(t.Context(), report.Reply{Ref: testRef, Kind: report.ReplyMissingModeMapping, MissingMode: agent.Mode(mode)})
		if err != nil {
			t.Fatal(err)
		}
		text := body.String()
		containsAll(t, "mapping refusal", text, "Nothing was run", "--mode-labels", "label the issue with a mapped mode", "comment again")
		if mode == "<!-- injected -->" {
			if strings.Contains(text, mode) {
				t.Fatal("unvalidated mode echoed")
			}
		} else if !strings.Contains(text, "`"+mode+"`") {
			t.Fatalf("mode missing: %s", text)
		}
	}
}

// #1123: a plain parent comment Implement cannot use is answered with a fixed
// reply that names the true state and next step, never parked-story guidance.
func TestRenderReplyPlainParentCommentRefusals(t *testing.T) {
	rep := newReporter(t)
	for _, tc := range []struct {
		kind report.ReplyKind
		name string
		want []string
	}{
		{report.ReplyValidateCommentNotApplied, "validate_comment_not_applied", []string{"Validate is running or waiting for you; nothing was run.", "The Validate report lists what blocks and the commands.", "its own comment"}},
		{report.ReplyImplementFinished, "implement_finished", []string{"Implement is finished: every story is merged.", "Nothing was run.", "commenting `/validate`"}},
		{report.ReplyImplementFinishedAutoValidate, "implement_finished_auto_validate", []string{"Implement is finished: every story is merged.", "Nothing was run.", "Validate starts automatically"}},
		{report.ReplyImplementFinishedValidated, "implement_finished_validated", []string{"Implement is finished and Validate has already finished on it.", "Nothing was run.", "The Validate report shows the result."}},
	} {
		body, err := rep.RenderReply(t.Context(), report.Reply{Ref: testRef, Kind: tc.kind})
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		text := body.String()
		for _, want := range append([]string{report.ReplyMarker(testRef)}, tc.want...) {
			if !strings.Contains(text, want) {
				t.Errorf("%s lacks %q:\n%s", tc.name, want, text)
			}
		}
		if strings.Contains(text, "parked") {
			t.Errorf("%s carries parked-story instructions:\n%s", tc.name, text)
		}
		if tc.kind.String() != tc.name || !tc.kind.RefusesMode() || tc.kind.RefusesTools() || tc.kind.RefusesAgent() || tc.kind.RefusesSwitch() || tc.kind.RefusesSync() || tc.kind.IsConfirmationReply() {
			t.Errorf("%s is in the wrong refusal family", tc.kind.String())
		}
	}
}

// Retained refusals must carry their actionable route through the final signed
// reply, rather than relying on a renderer fragment or a generic retry hint.
func TestRuntimeRefusalInspectionRoutes(t *testing.T) {
	t.Parallel()
	for _, kind := range []report.ReplyKind{report.ReplyDoctorUnsupported, report.ReplyGraphResumeRefused, report.ReplyScheduleBlocked} {
		t.Run(kind.String(), func(t *testing.T) {
			t.Parallel()
			body, err := newReporter(t).RenderReply(t.Context(), report.Reply{
				Ref: testRef, Kind: kind, Schedule: report.ScheduleReply{ID: 71},
				ResumeNode: "agent", ResumeReason: "saved launch is missing",
			})
			if err != nil {
				t.Fatal(err)
			}
			text := body.String()
			if strings.Count(text, report.TaskInspectionCommand(testRef.String())) != 1 || !strings.Contains(text, "factory host") || body.Truncated() {
				t.Fatalf("refusal lost its single affected-task inspection route: %s", text)
			}
			if !strings.Contains(text, report.ReplyMarker(testRef)) {
				t.Fatalf("missing affected-task marker: %s", text)
			}
		})
	}
}
