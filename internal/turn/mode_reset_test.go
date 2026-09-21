package turn

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

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

// TestSimultaneousAgentAndModeChangeReconcilesRatherThanRotates covers the one
// case the #228/#254 merge left undecided: a single turn where the resolved
// coding-agent tool changes *and* the admitted mode changes together. The
// agent-switch check in [prepareDeliverySegment]'s ladder runs first, ahead of
// the mode comparison, so the switch wins outright — the existing segment
// row is reconciled to the new agent in place (same id, same ordinal, a fresh
// session_ref, [store.Segment.AgentSwitched] set) — and the mode change is
// absorbed into that reconciliation rather than separately rotating to a
// second new segment. Exactly one segment must exist afterward, never two,
// and the delivery it carries (when a bundle is configured) must be the one
// planned for the newly resolved agent, not a leftover pin from the old one.
func TestSimultaneousAgentAndModeChangeReconcilesRatherThanRotates(t *testing.T) {
	t.Parallel()

	f := newFixture(t, nil)
	f.opts.Tiers = tieredOptions()
	ref := testRef(t)

	_, _, snapshot := deliveryFixture()
	claudePlan, err := planDelivery(f.opts, Request{Agent: agent.Kind(store.AgentClaude), Mode: agent.ModeResearch}, snapshot, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	baseReq := Request{Agent: agent.Kind(store.AgentClaude), Mode: agent.ModeResearch, SessionID: f.session.ID, Trigger: Trigger{CommentID: testComment}}
	first, firstDelivery, err := prepareDeliverySegment(t.Context(), f.opts, baseReq, f.session, ref, "simultaneous-switch-session", claudePlan)
	if err != nil {
		t.Fatal(err)
	}
	if first.Agent != string(store.AgentClaude) || firstDelivery == nil {
		t.Fatalf("seeded segment = %+v delivery=%v, want a claude segment with a delivery pinned", first, firstDelivery)
	}
	finishTerminalRun(t, f, ref, first, agent.Kind(store.AgentClaude), agent.ModeResearch, testComment)

	codexPlan, err := planDelivery(f.opts, Request{Agent: agent.Kind(store.AgentCodex), Mode: agent.ModePlan}, snapshot, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if codexPlan.identity == claudePlan.identity {
		t.Fatal("fixture bug: the two agents' plans must not already share an identity")
	}

	// Both the agent and the mode change from the first admission together.
	req := baseReq
	req.Agent = agent.Kind(store.AgentCodex)
	req.Mode = agent.ModePlan
	req.Trigger.CommentID = testComment + 1
	got, gotDelivery, err := prepareDeliverySegment(t.Context(), f.opts, req, f.session, ref, "simultaneous-switch-session", codexPlan)
	if err != nil {
		t.Fatal(err)
	}

	segments, err := f.db.ListSegments(t.Context(), f.session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(segments) != 1 {
		t.Fatalf("segments = %d, want exactly one — a simultaneous agent+mode change must never produce two: %+v", len(segments), segments)
	}
	if got.ID != first.ID || got.Ordinal != first.Ordinal {
		t.Fatalf("a simultaneous agent+mode change rotated instead of reconciling in place: first=%+v got=%+v", first, got)
	}
	if got.Agent != string(store.AgentCodex) {
		t.Fatalf("agent = %q, want %q — the switch must win", got.Agent, store.AgentCodex)
	}
	if !got.AgentSwitched {
		t.Fatal("the segment was not marked switched — a rotation happened instead of an in-place reconciliation")
	}
	if got.SessionRef == first.SessionRef {
		t.Fatal("reconciling to the new agent must mint a fresh session_ref, not keep the old one")
	}
	if gotDelivery == nil || gotDelivery.Identity != codexPlan.identity {
		t.Fatalf("the reconciled segment did not carry the new agent's delivery: %+v", gotDelivery)
	}
	if _, err := verifyDelivery(firstDelivery.Root, claudePlan); err != nil {
		t.Fatalf("the simultaneous switch modified the bundle the segment retained before it: %v", err)
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
// last recorded run. That refusal only fires for an automatic-recovery
// continuation (prepare.go gates it on req.Continuation != nil, since an
// ordinary turn's own agent mismatch is reconciled in place, not refused), so
// this drives a genuine continuation through Execute rather than an
// ordinary turn. The one way a continuation's segment still disagrees with
// the resolved agent by the time it is replayed is an ordinary turn already
// having reconciled it to a different tool in between — prepare.go's own
// comment calls this "defence in depth, not the primary defence" — so that
// is what this test reproduces, by switching the segment's stored agent
// directly after the continuation was saved and before it is replayed.
func TestAgentSwitchRefusalFiresRegardlessOfModeChange(t *testing.T) {
	t.Parallel()

	var f *fixture
	calls := 0
	runner := newStubRunner(func(_ context.Context, req harness.Request) (*harness.RunResult, error) {
		calls++
		res := succeeded(req, f.paths.Home())
		switch calls {
		case 1:
			// A terminal, non-refused run in one mode: the "segment's last
			// recorded run" the refusal must still fire despite disagreeing
			// with.
			return res, nil
		case 2:
			// A second turn, in a different mode, that hits its session
			// limit and leaves a reserved continuation behind.
			res.Outcome = agent.OutcomeInterrupted
			res.Interruption = &agent.Interruption{
				Precision:  agent.ResetExact,
				ResetBound: res.FinishedAt.Add(time.Hour),
				DueAt:      res.FinishedAt.Add(time.Hour + time.Minute),
			}
			return res, nil
		default:
			t.Error("claude ran despite an agent-switch refusal")
			return nil, errors.New("must not be reached")
		}
	})
	f = newFixture(t, runner)
	f.opts.AutoResumeOnSessionLimit = true

	first := f.request()
	first.Mode = agent.ModeSpike
	f.execute(first)

	second := f.request()
	second.Mode = agent.ModeResearch
	second.Trigger.CommentID = testComment + 1
	second.Trigger.Body = "/research look again"
	second.Command = ParseCommand(second.Trigger.Body)
	f.execute(second)

	pending, err := f.db.ListPendingContinuations(t.Context(), f.session.ID)
	if err != nil || len(pending) != 1 {
		t.Fatalf("pending=%+v err=%v", pending, err)
	}
	c, ok, err := f.db.ReserveContinuation(t.Context(), pending[0].ID, pending[0].DueAt)
	if err != nil || !ok {
		t.Fatalf("reserve=%v %v", ok, err)
	}

	req := second
	req.Continuation = &c
	req.Trigger = Trigger{CommentID: c.TriggerID(), Body: c.Context.Request, Author: second.Trigger.Author}
	req.Command = Command{Kind: KindFreeText, Text: c.Context.Request}

	// The only way a continuation's segment still disagrees with the
	// resolved agent by the time it is replayed: an ordinary turn already
	// reconciled it to a different tool in between validateContinuation's own
	// read (which must still see the segment matching what the continuation
	// recorded) and prepareDeliverySegment's later, independent re-read of
	// the same row. [raceGate] places that switch deterministically inside
	// that window.
	f.opts.CloneGate = &raceGate{CloneGate: f.opts.CloneGate, hook: func() {
		if err := f.db.SetSegmentAgent(t.Context(), c.SegmentID, string(store.AgentCodex), "codex-switched-in-session"); err != nil {
			t.Fatalf("switch the segment's agent out from under the continuation: %v", err)
		}
	}}

	result := f.execute(req)
	if result.Disposition != DispositionAgentSwitchRefused {
		t.Fatalf("disposition = %s, want %s", result.Disposition, DispositionAgentSwitchRefused)
	}
	if calls != 2 {
		t.Fatalf("claude invoked %d times, want 2 (the terminal run and the interrupted one)", calls)
	}
	if runs := f.runs(); len(runs) != 2 {
		t.Fatalf("agent-switch refusal changed the run rows: %+v", runs)
	}
	segments, err := f.db.ListSegments(t.Context(), f.session.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Two segments already exist from the mode change between the first two
	// turns (spike, then research); the refused continuation must not add a
	// third.
	if len(segments) != 2 || segments[len(segments)-1].ID != c.SegmentID {
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

// TestModeResetPreservesOrdinaryColdTurnMachinery is AC-13: a mode-change
// reset's prepared values — base-sync outcome, attachment handling, recorded
// comment history and approved/frozen story context — match those of an
// equivalent cold turn that went cold for an unrelated reason. Two identical
// sequences are run (same seeded comment thread, same first turn), and the
// second turn in each goes cold for a different, independent cause: one
// changes the admitted mode (a rotation to a fresh segment), the other keeps
// the mode but switches the coding-agent tool (an in-place reconciliation
// [store.Segment.AgentSwitched] forces cold regardless of history). Neither
// is a resume, so this is a genuine cold-vs-cold comparison, not the
// cold-vs-resume difference AC-11 already covers elsewhere: everything this
// test asserts on is computed upstream of segment selection, so it must come
// out the same either way.
func TestModeResetPreservesOrdinaryColdTurnMachinery(t *testing.T) {
	t.Parallel()

	type secondTurn struct {
		result Result
		req    harness.Request
	}

	run := func(t *testing.T, secondMode agent.Mode, secondKind agent.Kind) secondTurn {
		t.Helper()

		var f *fixture
		runner := newStubRunner(func(_ context.Context, req harness.Request) (*harness.RunResult, error) {
			return succeeded(req, f.paths.Home()), nil
		})
		f = newFixture(t, runner)
		// Compare legacy single-run cold machinery, independent of graph dispatch.
		if secondMode == agent.ModePlan {
			admitLegacyPlanFixture(t, f)
		}
		f.opts.Runners = RunnerSet{
			agent.Kind(store.AgentClaude): runner,
			agent.Kind(store.AgentCodex):  runner,
		}
		f.seedThread()

		first := f.request()
		first.Mode = agent.ModeResearch
		first.Agent = agent.Kind(store.AgentClaude)
		f.execute(first)

		second := f.request()
		second.Mode = secondMode
		second.Agent = secondKind
		second.Trigger.CommentID = testComment + 1
		second.Trigger.Body = "/" + string(secondMode) + " keep going with the next step"
		second.Command = ParseCommand(second.Trigger.Body)
		result := f.execute(second)

		if len(runner.requests) != 2 {
			t.Fatalf("requests=%+v", runner.requests)
		}
		return secondTurn{result: result, req: runner.requests[1]}
	}

	// Cold via a mode change, same agent: the reset this story adds.
	reset := run(t, agent.ModePlan, agent.Kind(store.AgentClaude))
	// Cold via an agent switch, same mode: an unrelated, already-existing
	// reason for a turn to go cold, used here only as the "equivalent cold
	// turn" reference.
	switched := run(t, agent.ModeResearch, agent.Kind(store.AgentCodex))

	// Sanity: both are genuinely cold, for their own distinct reason, and
	// neither is a resume.
	if reset.req.Path != harness.PathCold {
		t.Fatalf("mode-changed second turn path = %v, want %v", reset.req.Path, harness.PathCold)
	}
	if switched.req.Path != harness.PathCold {
		t.Fatalf("agent-switched second turn path = %v, want %v", switched.req.Path, harness.PathCold)
	}

	// Base-sync outcome: identical repo state in both fixtures, so the
	// recorded outcome must agree regardless of what caused the cold start.
	if reset.result.Run.SyncOutcome != switched.result.Run.SyncOutcome ||
		reset.result.Run.SyncBehind != switched.result.Run.SyncBehind ||
		reset.result.Run.SyncAhead != switched.result.Run.SyncAhead {
		t.Fatalf("sync outcome diverged between the two cold turns: reset=%+v switched=%+v",
			reset.result.Run, switched.result.Run)
	}

	// Attachments: neither trigger carries any, so neither prompt gets the section.
	for name, req := range map[string]harness.Request{"reset": reset.req, "switched": switched.req} {
		if strings.Contains(req.Prompt, "## Attachments") {
			t.Errorf("%s prompt carries an attachments section nothing triggered:\n%s", name, req.Prompt)
		}
	}

	// Comment history: the thread seeded before either sequence's first turn
	// must be surfaced on the second turn exactly the same way regardless of
	// what caused it to go cold — a mode-change reset must not suppress,
	// duplicate or reorder it relative to any other cold turn.
	for name, req := range map[string]harness.Request{"reset": reset.req, "switched": switched.req} {
		for _, want := range []string{
			"[old_owner] @marioatcloudvane at 2026-08-20T09:00:00Z:\n" + historyOwnerBody,
			"[old_third_party] @carol at 2026-08-20T10:00:00Z:\n" + historyThirdPartyBody,
			"[litefactory] @litefactory-bot at 2026-08-20T11:00:00Z:\n" + historySelfBody,
		} {
			if !strings.Contains(req.Prompt, want) {
				t.Errorf("%s prompt is missing seeded history %q:\n%s", name, want, req.Prompt)
			}
		}
	}
	// The first turn's own trigger becomes history too, once a second turn is
	// underway, exactly the same way for both.
	const firstTurnBody = "/spike add a health endpoint that reports the build sha"
	if !strings.Contains(reset.req.Prompt, firstTurnBody) || !strings.Contains(switched.req.Prompt, firstTurnBody) {
		t.Errorf("the first turn's own comment did not carry forward as history:\nreset:\n%s\nswitched:\n%s",
			reset.req.Prompt, switched.req.Prompt)
	}

	// Approved/frozen story context: no managed feature is configured in
	// either sequence, so neither carries plan-handoff transport.
	for name, req := range map[string]harness.Request{"reset": reset.req, "switched": switched.req} {
		if req.PlanWorkingContext != "" || req.PlanProposalPath != "" || req.PlanContextPath != "" {
			t.Errorf("%s carries approved/frozen story context nothing configured: working=%q proposal=%q context=%q",
				name, req.PlanWorkingContext, req.PlanProposalPath, req.PlanContextPath)
		}
	}
}
