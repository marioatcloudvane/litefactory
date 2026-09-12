package turn

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
)

// bothAgentKinds is the pair every AC-001/AC-002/AC-005 case in this file is
// run against: [prepareDeliverySegment]'s rotation ladder and
// [resumeDecision]'s cold-vs-resume choice apply the same way to a segment
// whichever coding-agent tool owns it (US-253).
var bothAgentKinds = []agent.Kind{agent.Kind(store.AgentClaude), agent.Kind(store.AgentCodex)}

// tieredOptions is a minimal [config.Tiers] that resolves a model for either
// agent kind's "balanced" tier, the only tier [deliveryFixture]'s snapshot
// asks for. It exists so a bundle-configured case can plan a delivery for
// whichever kind the table is currently exercising, claude included —
// [deliveryFixture] itself only configures codex.
func tieredOptions() config.Tiers {
	return config.Tiers{ModelByTier: map[config.Kind]map[config.Tier]string{
		"claude": {"balanced": "local-model"},
		"codex":  {"balanced": "local-model"},
	}}
}

// finishTerminalRun allocates and immediately finishes a non-refused run on
// segment, in mode, for kind — the fact [previousRun] and
// [prepareDeliverySegment]'s mode comparison both read back afterward. For a
// non-Claude kind it also records the resume token the way a real run's
// harvest does, since [resumeDecision] requires one before such a segment can
// resume; Claude needs none of its own (its --session-id already is the
// segment's SessionRef).
func finishTerminalRun(t *testing.T, f *fixture, ref naming.Ref, segment store.Segment, kind agent.Kind, mode agent.Mode, commentID int64) {
	t.Helper()

	run, err := f.db.AllocateRun(t.Context(), store.AllocateRunParams{
		SessionID: f.session.ID, SegmentID: segment.ID, Mode: string(mode),
		Agent: string(kind), TriggerCommentID: commentID,
		LogPath: func(ordinal int) string {
			path, pathErr := naming.RunLogPath(f.paths.Home(), ref, ordinal)
			if pathErr != nil {
				t.Fatalf("derive the seeded log path: %v", pathErr)
			}
			return path
		},
	})
	if err != nil {
		t.Fatalf("allocate a terminal predecessor run: %v", err)
	}
	exitCode := int64(0)
	if _, err := f.db.FinishRun(t.Context(), run.ID, store.RunOutcome{Status: store.RunSucceeded, ExitCode: &exitCode}); err != nil {
		t.Fatalf("finish the predecessor run: %v", err)
	}
	if kind != agent.Kind(store.AgentClaude) {
		if err := f.db.SetSegmentResumeToken(t.Context(), segment.ID, "codex-thread-"+string(mode)); err != nil {
			t.Fatalf("record the tool-reported resume token: %v", err)
		}
	}
}

// TestModeAdmissionAcrossAgentsAndBundleConfigs is AC-001, AC-002, AC-003 and
// the offline routing half of AC-005: an admission whose mode differs from
// the segment's last terminal, non-refused run's mode resets to a fresh, cold
// segment — next ordinal, a distinct session_ref, no resume token — and an
// admission in the same mode reuses and resumes the existing one. Both hold
// for claude and codex, and for a repository with no instruction bundle
// configured as well as one that has (US-253).
func TestModeAdmissionAcrossAgentsAndBundleConfigs(t *testing.T) {
	t.Parallel()

	for _, kind := range bothAgentKinds {
		for _, withBundle := range []bool{false, true} {
			name := string(kind) + "/no-bundle"
			if withBundle {
				name = string(kind) + "/with-bundle"
			}
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				f := newFixture(t, nil)
				if withBundle {
					f.opts.Tiers = tieredOptions()
				}
				ref := testRef(t)

				var bundle *deliveryPlan
				if withBundle {
					_, _, snapshot := deliveryFixture()
					var err error
					bundle, err = planDelivery(f.opts, Request{Agent: kind, Mode: agent.ModeResearch}, snapshot, t.TempDir())
					if err != nil {
						t.Fatal(err)
					}
				}

				baseReq := Request{Agent: kind, Mode: agent.ModeResearch, SessionID: f.session.ID, Trigger: Trigger{CommentID: testComment}}
				first, firstDelivery, err := prepareDeliverySegment(t.Context(), f.opts, baseReq, f.session, ref, "mode-admission-session", bundle)
				if err != nil {
					t.Fatal(err)
				}
				if withBundle && firstDelivery == nil {
					t.Fatal("expected the first admission to publish a bundle")
				}
				finishTerminalRun(t, f, ref, first, kind, agent.ModeResearch, testComment)

				t.Run("same mode reuses and resumes the segment", func(t *testing.T) {
					req := baseReq
					req.Trigger.CommentID = testComment + 1
					got, gotDelivery, err := prepareDeliverySegment(t.Context(), f.opts, req, f.session, ref, "mode-admission-session", bundle)
					if err != nil {
						t.Fatal(err)
					}
					if got.ID != first.ID || got.Ordinal != first.Ordinal || got.SessionRef != first.SessionRef {
						t.Fatalf("same-mode admission rotated the segment: got=%+v want ordinal/ref of %+v", got, first)
					}
					if withBundle && (gotDelivery == nil || gotDelivery.Root != firstDelivery.Root) {
						t.Fatalf("same-mode admission replaced the retained bundle: %+v", gotDelivery)
					}
					previous, found, err := previousRun(t.Context(), f.opts, f.session, got)
					if err != nil {
						t.Fatal(err)
					}
					if !resumeDecision(found, got, kind) {
						t.Fatalf("same-mode admission did not resume: found=%v previous=%+v segment=%+v", found, previous, got)
					}
				})

				t.Run("a different mode resets to a fresh cold segment", func(t *testing.T) {
					req := baseReq
					req.Mode = agent.ModePlan
					req.Trigger.CommentID = testComment + 2
					got, gotDelivery, err := prepareDeliverySegment(t.Context(), f.opts, req, f.session, ref, "mode-admission-session", bundle)
					if err != nil {
						t.Fatal(err)
					}
					if got.ID == first.ID {
						t.Fatal("a mode change did not rotate the segment")
					}
					wantOrdinal := first.Ordinal + 1
					if got.Ordinal != wantOrdinal {
						t.Fatalf("ordinal = %d, want %d", got.Ordinal, wantOrdinal)
					}
					wantRef, err := naming.SessionRef(ref, wantOrdinal)
					if err != nil {
						t.Fatal(err)
					}
					if got.SessionRef != wantRef {
						t.Fatalf("session_ref = %q, want the recomputed %q", got.SessionRef, wantRef)
					}
					if got.SessionRef == first.SessionRef {
						t.Fatal("the rotated segment reused the previous session_ref")
					}
					if got.ResumeToken != "" {
						t.Fatalf("a freshly rotated segment already carries a resume token: %q", got.ResumeToken)
					}
					if withBundle {
						if gotDelivery == nil || gotDelivery.Root == firstDelivery.Root {
							t.Fatalf("a mode change did not publish its own bundle: %+v", gotDelivery)
						}
						if _, err := verifyDelivery(firstDelivery.Root, bundle); err != nil {
							t.Fatalf("rotation modified the retained bundle it left behind: %v", err)
						}
					}
					previous, found, err := previousRun(t.Context(), f.opts, f.session, got)
					if err != nil {
						t.Fatal(err)
					}
					if found {
						t.Fatal("a freshly rotated segment must read back with no previous run")
					}
					if resumeDecision(found, got, kind) {
						t.Fatalf("a mode change still resumed: found=%v previous=%+v", found, previous)
					}
				})
			})
		}
	}
}

// TestSimultaneousBundleAndModeChangeRotatesExactlyOnce is AC-004: an
// instruction-bundle identity change and a mode change admitted together do
// not each cause their own rotation — exactly one new segment is created —
// and the bundle the previous segment retained is left byte-identical
// afterward.
func TestSimultaneousBundleAndModeChangeRotatesExactlyOnce(t *testing.T) {
	t.Parallel()

	for _, kind := range bothAgentKinds {
		t.Run(string(kind), func(t *testing.T) {
			t.Parallel()

			f := newFixture(t, nil)
			f.opts.Tiers = tieredOptions()
			ref := testRef(t)

			_, _, snapshot := deliveryFixture()
			original, err := planDelivery(f.opts, Request{Agent: kind, Mode: agent.ModeResearch}, snapshot, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}

			baseReq := Request{Agent: kind, Mode: agent.ModeResearch, SessionID: f.session.ID, Trigger: Trigger{CommentID: testComment}}
			first, firstDelivery, err := prepareDeliverySegment(t.Context(), f.opts, baseReq, f.session, ref, "simultaneous-session", original)
			if err != nil {
				t.Fatal(err)
			}
			finishTerminalRun(t, f, ref, first, kind, agent.ModeResearch, testComment)

			snapshot.Provenance.Version = "9.9.9"
			changed, err := planDelivery(f.opts, Request{Agent: kind, Mode: agent.ModePlan}, snapshot, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if changed.identity == original.identity {
				t.Fatal("the fixture mutation did not actually change the bundle identity")
			}

			req := baseReq
			req.Mode = agent.ModePlan
			req.Trigger.CommentID = testComment + 1
			got, gotDelivery, err := prepareDeliverySegment(t.Context(), f.opts, req, f.session, ref, "simultaneous-session", changed)
			if err != nil {
				t.Fatal(err)
			}

			segments, err := f.db.ListSegments(t.Context(), f.session.ID)
			if err != nil {
				t.Fatal(err)
			}
			if len(segments) != 2 {
				t.Fatalf("segments = %d, want exactly one rotation on top of the first: %+v", len(segments), segments)
			}
			if got.Ordinal != first.Ordinal+1 {
				t.Fatalf("ordinal = %d, want %d", got.Ordinal, first.Ordinal+1)
			}
			if gotDelivery == nil || gotDelivery.Identity != changed.identity {
				t.Fatalf("the rotated segment did not carry the changed bundle: %+v", gotDelivery)
			}
			if _, err := verifyDelivery(firstDelivery.Root, original); err != nil {
				t.Fatalf("the simultaneous rotation modified the bundle the first segment retained: %v", err)
			}
		})
	}
}

// TestModeSequenceProducesForwardOnlyRecomputableOrdinals is AC-001's
// sequencing half: research -> plan -> implement -> plan again produces
// strictly increasing, distinct, independently recomputable ordinals and
// session refs. The return to plan gets a brand new segment, never the one
// plan already used earlier in the same sequence.
func TestModeSequenceProducesForwardOnlyRecomputableOrdinals(t *testing.T) {
	t.Parallel()

	f := newFixture(t, nil)
	ref := testRef(t)
	kind := agent.Kind(store.AgentClaude)

	sequence := []agent.Mode{agent.ModeResearch, agent.ModePlan, agent.ModeImplement, agent.ModePlan}
	var ordinals []int
	seenRefs := map[string]bool{}
	commentID := testComment
	for i, mode := range sequence {
		req := Request{Agent: kind, Mode: mode, SessionID: f.session.ID, Trigger: Trigger{CommentID: commentID}}
		segment, _, err := prepareDeliverySegment(t.Context(), f.opts, req, f.session, ref, "sequence-first-session", nil)
		if err != nil {
			t.Fatalf("step %d (%s): %v", i, mode, err)
		}

		wantOrdinal := store.FirstSegmentOrdinal + i
		if segment.Ordinal != wantOrdinal {
			t.Fatalf("step %d (%s): ordinal = %d, want %d", i, mode, segment.Ordinal, wantOrdinal)
		}
		wantRef := "sequence-first-session"
		if i > 0 {
			wantRef, err = naming.SessionRef(ref, wantOrdinal)
			if err != nil {
				t.Fatal(err)
			}
		}
		if segment.SessionRef != wantRef {
			t.Fatalf("step %d (%s): session_ref = %q, want the recomputed %q", i, mode, segment.SessionRef, wantRef)
		}
		if seenRefs[segment.SessionRef] {
			t.Fatalf("step %d (%s): session_ref %q was reused from an earlier step", i, mode, segment.SessionRef)
		}
		seenRefs[segment.SessionRef] = true
		ordinals = append(ordinals, segment.Ordinal)

		finishTerminalRun(t, f, ref, segment, kind, mode, commentID)
		commentID++
	}

	for i := 1; i < len(ordinals); i++ {
		if ordinals[i] <= ordinals[i-1] {
			t.Fatalf("ordinals are not forward-only: %v", ordinals)
		}
	}
	// The return to plan (step 3) must not have reused plan's earlier segment
	// (step 1, ordinal 2).
	if ordinals[3] == ordinals[1] {
		t.Fatalf("returning to an earlier mode reused its earlier segment: %v", ordinals)
	}
}

// TestPrepareDeliverySegmentNeverResetsWithoutATerminalNonRefusedRun is
// AC-007: a candidate segment with no terminal, non-refused run is never
// rotated for a mode change, because there is nothing yet to compare the
// admitted mode against. This covers a task's first-ever run, a re-prepared
// prelaunch allocation that never completed, and a run that ran but was
// refused.
func TestPrepareDeliverySegmentNeverResetsWithoutATerminalNonRefusedRun(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		seed func(t *testing.T, f *fixture, segment store.Segment)
	}{
		{name: "the task's first-ever run", seed: func(*testing.T, *fixture, store.Segment) {}},
		{
			name: "a prelaunch allocation still running",
			seed: func(t *testing.T, f *fixture, segment store.Segment) {
				if _, err := allocateRun(t.Context(), f.opts, Request{
					Mode: agent.ModeResearch, Trigger: Trigger{CommentID: testComment},
				}, f.session, segment, testRef(t)); err != nil {
					t.Fatalf("seed the prelaunch allocation: %v", err)
				}
			},
		},
		{
			name: "a refused run",
			seed: func(t *testing.T, f *fixture, segment store.Segment) {
				run, err := allocateRun(t.Context(), f.opts, Request{
					Mode: agent.ModeResearch, Trigger: Trigger{CommentID: testComment},
				}, f.session, segment, testRef(t))
				if err != nil {
					t.Fatalf("seed the refused run: %v", err)
				}
				if _, err := f.db.FinishRun(t.Context(), run.ID, store.RunOutcome{
					Status: store.RunFailed, SemanticOutcome: "refused",
				}); err != nil {
					t.Fatalf("finish the refused run: %v", err)
				}
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newFixture(t, nil)
			ref := testRef(t)
			kind := agent.Kind(store.AgentClaude)

			first := Request{Agent: kind, Mode: agent.ModeResearch, SessionID: f.session.ID, Trigger: Trigger{CommentID: testComment}}
			segment, _, err := prepareDeliverySegment(t.Context(), f.opts, first, f.session, ref, "prelaunch-session", nil)
			if err != nil {
				t.Fatal(err)
			}
			tc.seed(t, f, segment)

			// A different admitted mode than the (nonexistent) predecessor's:
			// nothing to compare against, so nothing may rotate.
			retry := first
			retry.Mode = agent.ModePlan
			retry.Trigger.CommentID = testComment + 1
			got, _, err := prepareDeliverySegment(t.Context(), f.opts, retry, f.session, ref, "prelaunch-session", nil)
			if err != nil {
				t.Fatal(err)
			}
			if got.ID != segment.ID || got.Ordinal != segment.Ordinal || got.SessionRef != segment.SessionRef {
				t.Fatalf("rotated despite no terminal, non-refused predecessor: got=%+v want=%+v", got, segment)
			}
		})
	}
}

// TestAgentSwitchRefusalFiresRegardlessOfModeChange is AC-006: a requested
// agent that differs from the segment's recorded agent still produces the
// existing agent-switch refusal, with no new segment created and no run
// allocated, even when the admitted mode also differs from the segment's
// last recorded run.
func TestAgentSwitchRefusalFiresRegardlessOfModeChange(t *testing.T) {
	t.Parallel()

	runner := newStubRunner(func(context.Context, harness.Request) (*harness.RunResult, error) {
		t.Error("claude ran despite an agent-switch refusal")
		return nil, errors.New("must not be reached")
	})
	f := newFixture(t, runner)
	f.seedRun(store.FirstSegmentOrdinal, storedClaudeSessionID, seededComment, store.ModeSpike, store.RunReported)

	req := f.request()
	req.Agent = agent.Kind(store.AgentCodex)
	req.Mode = agent.ModeResearch
	req.Trigger.Body = "/research look again"
	req.Command = ParseCommand(req.Trigger.Body)

	result := f.execute(req)
	if result.Disposition != DispositionAgentSwitchRefused {
		t.Fatalf("disposition = %s, want %s", result.Disposition, DispositionAgentSwitchRefused)
	}
	if runs := f.runs(); len(runs) != 1 {
		t.Fatalf("agent-switch refusal changed the run rows: %+v", runs)
	}
	segments, err := f.db.ListSegments(t.Context(), f.session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(segments) != 1 || segments[0].Ordinal != store.FirstSegmentOrdinal {
		t.Fatalf("agent-switch refusal rotated the segment: %+v", segments)
	}
}

// TestContinuationRefusesWhenTheBundleIdentityChanged is AC-008's other half:
// a continuation still reuses its recorded segment (covered elsewhere by
// TestAutomaticResumeNeverColdFallsBack), and still refuses with the existing
// message when the instruction bundle identity changed underneath it — never
// silently rotating or resuming against different resources.
func TestContinuationRefusesWhenTheBundleIdentityChanged(t *testing.T) {
	t.Parallel()

	f := newFixture(t, nil)
	f.opts.Tiers = tieredOptions()
	ref := testRef(t)
	kind := agent.Kind(store.AgentClaude)

	_, _, snapshot := deliveryFixture()
	original, err := planDelivery(f.opts, Request{Agent: kind, Mode: agent.ModeResearch}, snapshot, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	baseReq := Request{Agent: kind, Mode: agent.ModeResearch, SessionID: f.session.ID, Trigger: Trigger{CommentID: testComment}}
	segment, delivery, err := prepareDeliverySegment(t.Context(), f.opts, baseReq, f.session, ref, "continuation-session", original)
	if err != nil {
		t.Fatal(err)
	}
	if delivery == nil {
		t.Fatal("expected the first admission to publish a bundle")
	}

	snapshot.Provenance.Version = "9.9.9"
	changed, err := planDelivery(f.opts, Request{Agent: kind, Mode: agent.ModeResearch}, snapshot, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if changed.identity == original.identity {
		t.Fatal("the fixture mutation did not actually change the bundle identity")
	}

	req := baseReq
	req.Continuation = &store.Continuation{SegmentID: segment.ID}
	if _, _, err := prepareDeliverySegment(t.Context(), f.opts, req, f.session, ref, "continuation-session", changed); err == nil ||
		!strings.Contains(err.Error(), "automatic recovery instruction bundle changed") {
		t.Fatalf("a continuation with a changed bundle identity did not refuse: %v", err)
	}

	// The retained bundle from the accepted segment is untouched by the refusal.
	if _, err := verifyDelivery(delivery.Root, original); err != nil {
		t.Fatalf("the refused continuation modified the retained bundle: %v", err)
	}
}
