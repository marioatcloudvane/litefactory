package turn

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
)

// A Research or Plan process that exits non-zero after its own accepted result
// keeps that result; without one it still fails; a shutdown still interrupts.
func TestAgentActivityExecute_AcceptAfterProcessFailure(t *testing.T) {
	for _, mode := range []string{"research", "plan"} {
		for _, tc := range []struct {
			name        string
			submit      bool
			cancel      bool
			wantSuccess bool
		}{
			{"accepted result then non-zero exit/result used", true, false, true},
			{"no accepted result then non-zero exit/still fails", false, false, false},
			{"accepted result then shutdown/still interrupts", true, true, false},
		} {
			t.Run(mode+"/"+tc.name, func(t *testing.T) {
				f := newRetainedFixture(t, mode)
				at := time.Now().UTC()
				def, err := f.db.CreateGraphDefinition(t.Context(), store.GraphDefinition{Team: "builtin", Mode: mode, ProfileVersion: 1, Path: mode + ".dot", Digest: strings.Repeat("e", 64), CreatedAt: at})
				mustProgress(t, err)
				instance, err := f.db.CreateGraphInstance(t.Context(), store.GraphInstance{SessionID: f.session.ID, DefinitionID: def.ID, CurrentNode: "work", StartedAt: at})
				mustProgress(t, err)
				profile := &graphexec.Profile{Mode: f.policy.Mode, Team: "builtin", ProfileVersion: 1, Digest: def.Digest, Nodes: []graphexec.Node{{Name: "work", Action: ResearchAgentAction, Limit: 1}}}
				run, err := f.db.GetRun(t.Context(), f.opts.Attempt.RunID)
				mustProgress(t, err)
				segment, err := f.db.GetSegment(t.Context(), run.SegmentID)
				mustProgress(t, err)
				ref, err := naming.ParseRef(f.session.TaskRef)
				mustProgress(t, err)
				worktree := t.TempDir()
				mustProgress(t, os.MkdirAll(filepath.Join(worktree, ".aiteam/session"), 0700))
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				runner := newStubRunner(func(runctx context.Context, _ agent.Request) (*agent.RunResult, error) {
					if tc.submit {
						f.submit(t, f.result)
					}
					if tc.cancel {
						cancel()
					}
					return &agent.RunResult{Outcome: agent.OutcomeFailed, Reason: "exit status 1"}, errors.New("process failure")
				})
				delivered := fixtureNativeDelivery(t, f.policy)
				handler := AgentActivity{Policy: f.policy, Prepare: func(context.Context, Options, ActivityRequest) (ResearchInvocation, error) {
					request := agent.Request{Delivery: delivered, Mode: string(f.policy.Mode), Ref: ref, RunOrdinal: run.Ordinal, SegmentOrdinal: segment.Ordinal, Session: agent.SessionRef{Ref: segment.SessionRef, Resume: segment.ResumeToken}, WorktreePath: worktree, Prompt: "Work", Permissions: agent.Permissions{Profile: mode}}
					return ResearchInvocation{Request: request, Kind: agent.Kind(store.AgentClaude), SessionPath: ".aiteam/session", Results: f.service}, nil
				}}
				var registry ActivityRegistry
				mustProgress(t, registry.Register(ResearchAgentAction, handler))

				out, _ := ExecuteNodeAction(ctx, Options{DB: f.db, Runners: runnerSet(runner)}, GraphNodeRequest{Profile: profile, Activities: registry, InstanceID: instance.ID, RunID: run.ID, At: at})

				// The activity result, not graph routing, is under test: a used result
				// carries the retained node as its success output.
				if got := out.Activity.Outcome == "success" && len(out.Activity.Output) > 0; got != tc.wantSuccess {
					t.Fatalf("activity outcome = %q output=%d bytes, used-result want %v", out.Activity.Outcome, len(out.Activity.Output), tc.wantSuccess)
				}
			})
		}
	}
}
