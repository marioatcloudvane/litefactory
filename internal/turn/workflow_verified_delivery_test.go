package turn

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/teamsource"
)

// These fixtures prove factory selection and retained-byte checks offline.
// Authentic provider instruction loading and provider resume remain unverified;
// no test here invokes a paid provider.
func TestRegisteredWorkflowsCarryVerifiedNativeDelivery(t *testing.T) {
	for _, policy := range RegisteredWorkflowPolicies() {
		name := string(policy.Mode)
		if policy.Workflow == agent.WorkflowBug {
			name = "bug"
		}
		for _, engine := range []agent.Kind{agent.Kind("claude"), agent.Kind("codex")} {
			t.Run(name+"/"+string(engine), func(t *testing.T) {
				opts, req, def := deliveryFixture()
				req.Agent, req.Mode, req.Workflow = engine, policy.Mode, policy.Workflow
				opts.Tiers.ModelByTier[config.Kind(engine)] = map[config.Tier]string{"balanced": "selected-model"}
				// A role with no required skills is a complete selection too.
				emptyPlan, err := planWorkflowDelivery(opts, req, def, "first", policy)
				if err != nil {
					t.Fatal(err)
				}
				empty, err := writeDelivery(t.Context(), t.TempDir(), "bundles/no-skills", emptyPlan)
				if err != nil {
					t.Fatal(err)
				}
				if len(empty.RequiredSkills) != 0 || len(empty.Skills) != 0 {
					t.Fatal("no-skill role gained skills")
				}
				if err := agent.VerifyRequestDelivery(t.Context(), agent.Request{Invocation: agent.InvocationResearchNode, Graph: &agent.GraphInvocation{Node: "first"}, Mode: string(policy.Mode), Workflow: policy.Workflow, Delivery: empty}); err != nil {
					t.Fatalf("no-skill role refused: %v", err)
				}
				node := def.Nodes["first"]
				node.Skills = []string{"evidence"}
				node.Files["skills/evidence/SKILL.md"] = []byte("---\nname: evidence\ndescription: Evidence\n---\nRead the selected resource")
				node.Files["skills/evidence/references/check.md"] = []byte("SELECTED_CLOSURE")
				def.Nodes["first"] = node
				p, err := planWorkflowDelivery(opts, req, def, "first", policy)
				if err != nil {
					t.Fatal(err)
				}
				d, err := writeDelivery(t.Context(), t.TempDir(), "bundles/native", p)
				if err != nil {
					t.Fatal(err)
				}
				if d.MainRole != node.Role.Frontmatter.Name || d.MainModel != "selected-model" || !reflect.DeepEqual(d.RequiredSkills, d.Skills) || len(d.Skills) != 1 {
					t.Fatalf("selected metadata: %+v", d)
				}
				if len(d.RenderedFiles) != len(p.files) {
					t.Fatalf("native manifest has %d entries; want %d", len(d.RenderedFiles), len(p.files))
				}
				for path, raw := range p.files {
					if d.RenderedFiles[path] != fmt.Sprintf("%x", sha256.Sum256(raw)) {
						t.Errorf("native hash mismatch for %s", path)
					}
				}
				closure := "plugin/skills/evidence/references/check.md"
				if _, ok := d.RenderedFiles[closure]; !ok {
					t.Fatal("selected skill resource closure missing")
				}
				launch := agent.Request{Invocation: agent.InvocationResearchNode, Graph: &agent.GraphInvocation{InstanceID: 1, AttemptID: 1, RunID: 1, Node: "first"}, Mode: string(policy.Mode), Workflow: policy.Workflow, Delivery: d, Ref: testRef(t), RunOrdinal: 1, SegmentOrdinal: 1, Session: agent.SessionRef{Ref: "saved-conversation"}, WorktreePath: t.TempDir(), Prompt: "task"}
				for _, path := range []agent.RunPath{agent.PathCold, agent.PathResume, agent.PathColdAfterResumeRefused} {
					launch.Path = path
					if err := agent.VerifyRequestDelivery(t.Context(), launch); err != nil {
						t.Fatalf("valid %s: %v", path, err)
					}
				}
				// Tampering after a valid attempt must be caught on each subsequent path.
				resource := filepath.Join(d.Root, closure)
				if err := os.WriteFile(resource, []byte("corrupted"), 0600); err != nil {
					t.Fatal(err)
				}
				for _, path := range []agent.RunPath{agent.PathCold, agent.PathResume, agent.PathColdAfterResumeRefused} {
					launch.Path = path
					if err := agent.VerifyRequestDelivery(t.Context(), launch); err == nil {
						t.Fatalf("%s accepted changed closure", path)
					}
				}
				// The plan owns captured bytes, and delivered metadata owns its manifest.
				node.Files["skills/evidence/references/check.md"][0] = 'X'
				if string(p.files[closure]) != "SELECTED_CLOSURE" {
					t.Fatal("plan shares captured bytes")
				}
				clone := d.Clone()
				clone.RenderedFiles[closure] = "changed"
				clone.RequiredSkills[0] = "changed"
				if d.RenderedFiles[closure] == "changed" || d.RequiredSkills[0] == "changed" {
					t.Fatal("delivery clone shares selection metadata")
				}
			})
		}
	}
}

func TestRegisteredWorkflowResumePreservesCorruptSavedBundle(t *testing.T) {
	for _, policy := range RegisteredWorkflowPolicies() {
		name := string(policy.Mode)
		if policy.Workflow == agent.WorkflowBug {
			name = "bug"
		}
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, nil)
			opts, req, def := deliveryFixture()
			opts.DB, opts.Paths, opts.Logger = f.db, f.paths, f.opts.Logger
			req.SessionID, req.Mode, req.Workflow = f.session.ID, policy.Mode, policy.Workflow
			segment, d, err := prepareWorkflowDeliveryForTest(t.Context(), opts, req, def, "first", f.session, testRef(t), "retained-session", policy)
			if err != nil {
				t.Fatal(err)
			}
			resource := "main-instructions.md"
			corrupted := []byte("CORRUPT_SAVED_INSTRUCTIONS")
			if err := os.WriteFile(filepath.Join(d.Root, resource), corrupted, 0600); err != nil {
				t.Fatal(err)
			}
			// A change to unrelated current installation data does not replace capture.
			def.Digest = "changed-installation-digest"
			for _, automatic := range []bool{false, true} {
				req.Continuation = nil
				if automatic {
					req.Continuation = &store.Continuation{SegmentID: segment.ID}
				}
				_, replacement, err := prepareWorkflowDeliveryForTest(context.Background(), opts, req, def, "first", f.session, testRef(t), "retained-session", policy)
				if err == nil || replacement != nil {
					t.Fatalf("automatic=%v resumed corrupt bundle: %v", automatic, err)
				}
				for _, want := range []string{name, "first", resource, "installed-workflow"} {
					if !strings.Contains(err.Error(), want) {
						t.Fatalf("refusal omits %q: %v", want, err)
					}
				}
				identity, root, err := f.db.SegmentDelivery(t.Context(), segment.ID)
				if err != nil || identity != d.Identity || root != d.Root {
					t.Fatalf("saved evidence changed: %s %s %v", identity, root, err)
				}
				actual, err := os.ReadFile(filepath.Join(root, resource))
				if err != nil || string(actual) != string(corrupted) {
					t.Fatalf("corrupt bytes replaced: %q %v", actual, err)
				}
				segments, err := f.db.ListSegments(t.Context(), f.session.ID)
				if err != nil || len(segments) != 1 {
					t.Fatalf("refusal allocated replacement: %d %v", len(segments), err)
				}
			}
		})
	}
}

func TestVerifiedDeliveryRefusalReachesWorkflowFailureReport(t *testing.T) {
	var starts, runnerCalls int
	runner := lifecycleMCPHost(func(ctx context.Context, req agent.Request) (*agent.RunResult, error) {
		runnerCalls++
		if req.Delivery == nil {
			t.Fatal("workflow request omitted captured delivery")
		}
		const resource = "bootstrap.md"
		if err := os.WriteFile(filepath.Join(req.Delivery.Root, resource), []byte("CORRUPT_BEFORE_LAUNCH"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := agent.VerifyRequestDelivery(ctx, req); err != nil {
			return nil, err
		}
		starts++
		return nil, fmt.Errorf("unexpected process start")
	})
	f, forge, req := assembledResearchFixture(t, runner)
	out, err := Execute(t.Context(), f.opts, req)
	if err == nil || out.Run.Status != store.RunFailed || starts != 0 {
		t.Fatalf("refusal run=%+v starts=%d err=%v", out.Run, starts, err)
	}
	posts := forge.all()
	if len(posts) != 1 {
		t.Fatalf("failure reports=%d", len(posts))
	}
	for _, want := range []string{"research", "investigate", "bootstrap.md", "installed"} {
		if !strings.Contains(posts[0].body, want) {
			t.Errorf("failure report omits %q: %s", want, posts[0].body)
		}
	}
	if _, err := Execute(t.Context(), f.opts, req); err != nil {
		t.Fatalf("failure replay: %v", err)
	}
	if starts != 0 || forge.attemptCount() != 1 {
		t.Fatal("replay bypassed refusal or republished report")
	}
	// A later owner retry refuses the same saved corruption in preparation,
	// before even reaching the runner. Its own failure report retains the cause.
	retry := req
	retry.Trigger.CommentID++
	retry.Trigger.AuthorID = 7
	retry.Trigger.Body = "retry"
	retry.Command = ParseCommand("retry")
	out, err = Execute(t.Context(), f.opts, retry)
	if err != nil || out.Err == nil || out.Disposition != DispositionPrepareFailed || !out.Reportable() || !out.ConsumesComment() || out.Run.ID != 0 || out.ResearchGraph || runnerCalls != 1 || starts != 0 {
		t.Fatalf("saved-bundle preparation refusal: run=%+v runnerCalls=%d starts=%d err=%v", out.Run, runnerCalls, starts, err)
	}
	reporter := newReporter(t, f, forge, nil)
	if _, err := reporter.ReportTurn(t.Context(), out); err != nil {
		t.Fatalf("report saved-bundle refusal: %v", err)
	}
	posts = forge.all()
	if len(posts) != 2 {
		t.Fatalf("retry failure reports=%d", len(posts))
	}
	for _, want := range []string{"research", "investigate", "bootstrap.md", "installed"} {
		if !strings.Contains(posts[1].body, want) {
			t.Errorf("saved-bundle report omits %q: %s", want, posts[1].body)
		}
	}
	replayed, err := Execute(t.Context(), f.opts, retry)
	if err != nil {
		t.Fatalf("saved-bundle retry replay: %v", err)
	}
	if _, err := reporter.ReportTurn(t.Context(), replayed); err != nil {
		t.Fatalf("replayed refusal report: %v", err)
	}
	if runnerCalls != 1 || starts != 0 || forge.attemptCount() != 2 {
		t.Fatal("saved-bundle replay relaunched or duplicated refusal")
	}
}

// fixtureNativeDelivery supplies real renderer output to injected graph requests
// whose tests focus on metering or retained results rather than preparation.
func fixtureNativeDelivery(t *testing.T, policy WorkflowPolicy) *agent.Delivery {
	t.Helper()
	opts, req, def := deliveryFixture()
	req.Agent, req.Mode, req.Workflow = "claude", policy.Mode, policy.Workflow
	opts.Tiers.ModelByTier["claude"] = map[config.Tier]string{"balanced": "test-model"}
	p, err := planWorkflowDelivery(opts, req, def, "first", policy)
	if err != nil {
		t.Fatal(err)
	}
	d, err := writeDelivery(t.Context(), t.TempDir(), "bundles/fixture", p)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// A provider resume refusal authorizes one cold retry, but never skips the
// instruction check on that retry. Distinct metering attempts retain both paths.
func TestWorkflowColdRetryRechecksTamperedDelivery(t *testing.T) {
	var starts int
	var requests []agent.Request
	runner := lifecycleMCPHost(func(ctx context.Context, req agent.Request) (*agent.RunResult, error) {
		requests = append(requests, req)
		if err := agent.VerifyRequestDelivery(ctx, req); err != nil {
			return nil, err
		}
		starts++
		if starts == 1 {
			submitLiveResult(t, req, researchAskJSON)
			return &agent.RunResult{Outcome: agent.OutcomeSucceeded, ResultText: researchAskJSON, FinishedAt: time.Now()}, nil
		}
		if starts == 2 {
			if err := os.WriteFile(filepath.Join(req.Delivery.Root, "bootstrap.md"), []byte("CORRUPTED_BETWEEN_RESUME_AND_RETRY"), 0600); err != nil {
				t.Fatal(err)
			}
			return &agent.RunResult{Outcome: agent.OutcomeFailed, Reason: "resume unavailable", FinishedAt: time.Now()}, agent.ErrSessionNotResumable
		}
		t.Fatal("corrupt cold retry started another process")
		return nil, nil
	})
	f, forge, req := assembledResearchFixture(t, runner)
	_, err := Execute(t.Context(), f.opts, req)
	mustProgress(t, err)
	instance, err := f.db.LatestResearchInstance(t.Context(), f.session.ID)
	mustProgress(t, err)
	wait, err := f.db.GetGraphOwnerWait(t.Context(), instance.ID)
	mustProgress(t, err)
	delivery, err := f.db.GetReportDelivery(t.Context(), wait.DeliveryKey)
	mustProgress(t, err)
	reply := store.GraphOwnerReply{InstanceID: instance.ID, OwnerID: 7, AuthorID: 7, CommentID: testComment + 1, QuestionCommentID: delivery.CommentID, TaskRef: f.session.TaskRef, SourceRef: f.session.TaskRef, Mode: "research", DeliveryKey: wait.DeliveryKey, Body: "Inspect the HTTP boundary", At: time.Now()}
	_, err = f.db.ClaimGraphOwnerReply(t.Context(), reply)
	mustProgress(t, err)
	req.Trigger.CommentID++
	req.Trigger.Body = reply.Body
	req.Command = ParseCommand(reply.Body)
	out, err := Execute(t.Context(), f.opts, req)
	if !errors.Is(err, agent.ErrDeliveryRefused) || out.Run.Status != store.RunFailed || starts != 2 || len(requests) != 3 {
		t.Fatalf("retry refusal starts=%d requests=%d run=%+v err=%v", starts, len(requests), out.Run, err)
	}
	if requests[1].Path != agent.PathResume || requests[2].Path != agent.PathColdAfterResumeRefused || requests[1].Graph.AttemptID != requests[2].Graph.AttemptID {
		t.Fatal("retry lost its resume/cold logical attempt binding")
	}
	rows := researchMeterRows(t, f.db, out.Run.ID)
	if len(rows) != 2 || rows[0].AttemptID == rows[1].AttemptID {
		t.Fatalf("distinct retry meter attempts missing: %+v", rows)
	}
	posts := forge.all()
	if len(posts) != 2 {
		t.Fatalf("refusal reports=%d", len(posts))
	}
	for _, want := range []string{"research", "investigate", "bootstrap.md", "installed-workflow"} {
		if !strings.Contains(posts[1].body, want) {
			t.Errorf("retry refusal report omits %q: %s", want, posts[1].body)
		}
	}
}

// prepareWorkflowDeliveryForTest plans the captured delivery and selects its
// provider segment through the same two steps live code uses.
func prepareWorkflowDeliveryForTest(ctx context.Context, opts Options, req Request, definition *teamsource.WorkflowDefinition, node string, session store.Session, ref naming.Ref, firstRef string, policy WorkflowPolicy) (store.Segment, *agent.Delivery, error) {
	p, err := planWorkflowDelivery(opts, req, definition, node, policy)
	if err != nil {
		return store.Segment{}, nil, err
	}
	return prepareDeliverySegment(ctx, opts, req, session, ref, firstRef, p)
}
