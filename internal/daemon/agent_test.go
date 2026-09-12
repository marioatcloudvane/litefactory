package daemon_test

import (
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/daemon"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

// testDaemonAgents is the registered coding-agent-tool set every behavioural
// resolveAgent test in this file builds its daemon with: one implementation,
// keyed under [store.AgentClaude], which is what buildRunners registers in
// production today. Membership is all resolveAgent asks of it, so a nil
// Runner value is enough to populate it — see [daemon_test.testAgents]'s
// internal-test counterpart in agent_internal_test.go.
func testDaemonAgents() turn.RunnerSet {
	return turn.RunnerSet{agent.Kind(store.AgentClaude): nil}
}

// TestNoAgentLabelRequestIsByteIdenticalToAResolvedOne is AC-2.1's argv half,
// proxied through turn.Request — the value the harness derives Claude's argv
// from. This story adds no field to turn.Request for the agent a label
// resolves (routing to a different Runner is later work, US-3), so the
// request an issue with no lf:agent= label produces and the request the same
// issue produces with a well-formed, registered lf:agent=claude label are not
// merely similar, they are structurally identical — and so, therefore, is any
// argv the harness would build from either of them.
func TestNoAgentLabelRequestIsByteIdenticalToAResolvedOne(t *testing.T) {
	unlabelled := newFixture(t)
	unlabelled.daemon = unlabelled.newDaemon(daemon.Options{Agents: testDaemonAgents()})
	unlabelled.plantCursor()
	unlabelled.issue("Add a /health endpoint")
	unlabelled.settle() // first sight
	unlabelled.forge.addComment(testIssue, 701, owner(), "/spike add a health endpoint")
	unlabelled.settle()

	labelled := newFixture(t)
	labelled.daemon = labelled.newDaemon(daemon.Options{Agents: testDaemonAgents()})
	labelled.plantCursor()
	labelled.issueWithLabels("Add a /health endpoint", "Coding Agent: Claude")
	labelled.settle() // first sight
	labelled.forge.addComment(testIssue, 701, owner(), "/spike add a health endpoint")
	labelled.settle()

	unlabelledCalls := unlabelled.turns.calls()
	labelledCalls := labelled.turns.calls()
	if len(unlabelledCalls) != 1 || len(labelledCalls) != 1 {
		t.Fatalf("executor calls = %d (unlabelled), %d (labelled), want 1 each",
			len(unlabelledCalls), len(labelledCalls))
	}
	unlabelledReq, labelledReq := unlabelledCalls[0], labelledCalls[0]
	// Issue.Labels is the tick's raw label snapshot, carried on turn.Request
	// only as a lookup key for tool-profile (and now agent) resolution; it
	// never reaches the prompt or argv (turn.Issue.Labels's doc comment,
	// S-13). It is the one field this test's own label addition necessarily
	// changes, so it is cleared on both sides before the comparison that
	// matters: everything argv is actually built from.
	unlabelledReq.Issue.Labels = nil
	labelledReq.Issue.Labels = nil
	if !reflect.DeepEqual(unlabelledReq, labelledReq) {
		t.Errorf("a resolved lf:agent=claude label changed the turn.Request the harness builds "+
			"argv from:\nunlabelled: %+v\nlabelled:   %+v", unlabelledReq, labelledReq)
	}
}

// TestAgentLabelResolutionAddsNoForgeCall is AC-2.4: resolveAgent reads
// labels off the tick's already-fetched issue snapshot, the same one
// resolveTools already reads, so resolving a well-formed, registered
// lf:agent= label costs exactly the forge calls an ordinary comment-triggered
// tick already costs (AC-2.8: one issue listing per registered repository per
// tick, one comment listing for the issue that moved) plus the run's own
// acknowledgement and report — nothing dedicated to the label at all.
func TestAgentLabelResolutionAddsNoForgeCall(t *testing.T) {
	f := newFixture(t)
	f.daemon = f.newDaemon(daemon.Options{Agents: testDaemonAgents()})
	f.plantCursor()
	f.issueWithLabels("Add a /health endpoint", "Coding Agent: Claude")
	f.settle() // first sight

	issuesBefore, commentsBefore, postsBefore, reactionsBefore := f.forge.counts()
	f.forge.addComment(testIssue, 702, owner(), "/spike add a health endpoint")
	f.settle()
	issues, comments, posts, reactions := f.forge.counts()

	if issues-issuesBefore != 1 {
		t.Errorf("the tick made %d issue listings while resolving an lf:agent= label, want exactly "+
			"the ordinary per-tick 1", issues-issuesBefore)
	}
	if comments-commentsBefore != 1 {
		t.Errorf("the tick made %d comment listings, want exactly the ordinary 1", comments-commentsBefore)
	}
	if posts-postsBefore != 1 {
		t.Errorf("the tick posted %d comments, want exactly the run's own report", posts-postsBefore)
	}
	if reactions-reactionsBefore != 1 {
		t.Errorf("the tick added %d reactions, want exactly the ordinary acknowledgement", reactions-reactionsBefore)
	}
	if calls := f.turns.calls(); len(calls) != 1 {
		t.Errorf("the executor ran %d turns, want 1: a resolved label must not refuse the run", len(calls))
	}
}

// TestAgentRefusalPrecedesToolsRefusal is AC-2.6, the stage-1-before-stage-2
// half of the plan brief's four-stage refusal precedence: when an lf:agent=
// label and an lf:tools= label both fail to resolve in the same tick, only
// the agent refusal reaches the thread. The tools ladder is never evaluated
// once the agent ladder has already refused the run.
func TestAgentRefusalPrecedesToolsRefusal(t *testing.T) {
	f := newFixture(t)
	f.daemon = f.newDaemon(daemon.Options{
		Agents:       testDaemonAgents(),
		ToolProfiles: map[string]config.ToolProfile{"research": researchProfile()},
	})
	f.plantCursor()
	// Both ladders would refuse on their own: "codex" names no registered
	// coding-agent-tool implementation, and "nosuch" names no configured
	// profile. If the tools ladder ran at all, its own refusal would also
	// reach the thread — the assertion below is that only one reply is ever
	// posted, and it is the agent one.
	f.issueWithLabels("Add a /health endpoint", "Coding Agent: Codex", "lf:tools=nosuch")
	f.settle() // first sight

	f.forge.addComment(testIssue, 703, owner(), "/spike add a health endpoint")
	f.settle()

	if calls := f.turns.calls(); len(calls) != 0 {
		t.Errorf("the executor ran %d turns, want none: either refusal must be run-less", len(calls))
	}
	posted, _, _ := f.forge.snapshot()
	if len(posted) != 1 {
		t.Fatalf("posted %d comments, want exactly 1", len(posted))
	}
	body := posted[0]
	if !strings.HasPrefix(body, turn.MarkerPrefix) {
		t.Errorf("reply does not carry the litefactory marker:\n%s", body)
	}
	if !strings.Contains(body, "coding-agent tool") {
		t.Errorf("the reply does not read as the agent-selection refusal:\n%s", body)
	}
	if strings.Contains(body, "tool profile") {
		t.Errorf("the reply also carries the tools-refusal wording; the tools ladder must never run "+
			"once the agent ladder has refused:\n%s", body)
	}
	if strings.Contains(body, "nosuch") {
		t.Errorf("the reply names the unresolved lf:tools= label; the tools ladder must never be "+
			"evaluated once the agent ladder has refused:\n%s", body)
	}
}

// TestMalformedAgentLabelReplyNeverEchoesInjectedText is AC-2.3, the
// agent-selection counterpart of TestMalformedLabelReplyNeverEchoesInjectedText:
// the malformed and ambiguous agent refusals give raw label text no field to
// travel in (S-9), so this pushes several hostile shapes at the label value
// and asserts none of them reach the rendered reply.
func TestMalformedAgentLabelReplyNeverEchoesInjectedText(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{"markdown", "*bold*`code`"},
		{"an html comment", "<!--evil-->"},
		{"a second lf marker", "<!-- lf:status task=x run=1 -->"},
		{"a mention", "@everyone"},
		{"an embedded newline", "line1\nline2"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			f.daemon = f.newDaemon(daemon.Options{Agents: testDaemonAgents()})
			f.plantCursor()
			f.issueWithLabels("Add a /health endpoint", "Coding Agent: "+tc.value)
			f.settle()

			f.forge.addComment(testIssue, 951, owner(), "/spike add a health endpoint")
			f.settle()

			posted, _, _ := f.forge.snapshot()
			if len(posted) != 1 {
				t.Fatalf("posted %d comments, want 1", len(posted))
			}
			body := posted[0]
			if strings.Contains(body, tc.value) {
				t.Errorf("the reply echoed the raw label value %q:\n%s", tc.value, body)
			}
			if strings.Count(body, turn.MarkerPrefix) != 2 || !strings.Contains(body, "<!-- lf:reply task="+f.session().TaskRef+" -->") || !regexp.MustCompile(`<!-- lf:delivery v=1 key=[0-9a-f]{64} -->`).MatchString(body) {
				t.Errorf("the reply carries %d lf: markers, want exactly its own reply and durable delivery markers:\n%s",
					strings.Count(body, turn.MarkerPrefix), body)
			}
			if strings.Contains(body, "@everyone") {
				t.Errorf("the reply echoed a mention back into the comment:\n%s", body)
			}
		})
	}
}
