package turn

import (
	"context"
	"crypto/rand"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
)

type deliveryPlan struct {
	workflow  agent.Workflow
	node      string
	mainRole  string
	mainModel string
	identity  string
	files     map[string][]byte
	roles     []string
	skills    []string
	evidence  agent.DeliveryEvidence
}

// writeDelivery uses an exclusive directory per run. It never replaces retained
// bytes, and no age-based cleanup may delete resumable conversation resources.
func writeDelivery(ctx context.Context, home, relative string, p *deliveryPlan) (*agent.Delivery, error) {
	if p == nil {
		return nil, nil
	}
	if !filepath.IsAbs(home) || !filepath.IsLocal(relative) {
		return nil, fmt.Errorf("bundle path must be absolute")
	}
	homeRoot, err := os.OpenRoot(home)
	if err != nil {
		return nil, err
	}
	defer homeRoot.Close()
	if err := homeRoot.MkdirAll(relative, 0o700); err != nil {
		return nil, err
	}
	// All ancestry is resolved beneath the already-open runtime home before
	// allocating a private directory. A symlink may not redirect publication.
	for current := relative; current != "."; current = filepath.Dir(current) {
		info, err := homeRoot.Lstat(current)
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("bundle ancestry is symlinked")
		}
	}
	root := filepath.Join(home, relative)
	dir, err := os.MkdirTemp(root, "run-")
	if err != nil {
		return nil, fmt.Errorf("allocate bundle: %w", err)
	}
	r, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	keys := make([]string, 0, len(p.files))
	for path := range p.files {
		keys = append(keys, path)
	}
	sort.Strings(keys)
	for _, path := range keys {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !fs.ValidPath(path) {
			return nil, fmt.Errorf("bundle path is unsafe")
		}
		if err := r.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, err
		}
		if err := r.WriteFile(path, p.files[path], 0o600); err != nil {
			return nil, err
		}
	}
	d := agent.Delivery{MainRole: p.mainRole, MainModel: p.mainModel, Root: dir, Identity: p.identity, Roles: p.roles, Skills: p.skills, RequiredSkills: p.skills, RenderedFiles: renderedHashes(p), Evidence: p.evidence}.Clone()
	if err := agent.VerifyDelivery(ctx, d); err != nil {
		return nil, deliveryPlanRefusal(p, err)
	}
	return &d, nil
}

func renderedHashes(p *deliveryPlan) map[string]string {
	hashes := make(map[string]string, len(p.files))
	for name, raw := range p.files {
		hashes[name] = hashBytes(raw)
	}
	return hashes
}

// verifyDelivery refuses missing or edited retained bytes without rebuilding them.
func verifyDelivery(ctx context.Context, root string, p *deliveryPlan) (*agent.Delivery, error) {
	d := agent.Delivery{MainRole: p.mainRole, MainModel: p.mainModel, Root: root, Identity: p.identity, Roles: p.roles, Skills: p.skills, RequiredSkills: p.skills, RenderedFiles: renderedHashes(p), Evidence: p.evidence}.Clone()
	if err := agent.VerifyDelivery(ctx, d); err != nil {
		return nil, deliveryPlanRefusal(p, err)
	}
	return &d, nil
}

// The task lock owns this read/create sequence. A label change that resolves
// to a different coding-agent tool than the one already owning this session's
// last segment is a switch, not a refusal: the tail of this function
// reconciles the stale-agent row to the newly resolved tool in place — same
// id, same ordinal — via [reconcileSegmentAgent], writes a bundle rendered
// for the new tool, and re-pins delivery for it. This is checked first, ahead
// of anything mode-related, so it also decides the one turn where the agent
// *and* the admitted mode change together: the switch wins outright and the
// mode change is absorbed into it rather than separately rotating, which is
// what keeps this ladder from ever producing two new segments for one turn.
// Only a same-*agent* turn reaches the mode comparison at all: it rotates to
// a new segment (next ordinal, a distinct session_ref, no resume token, a
// freshly written bundle if one is configured) when either the effective
// instruction-bundle identity or the admitted mode ([Request.mode]) differs
// from what the segment's last terminal, non-refused run ([previousRun])
// recorded; a candidate with no such run is never rotated for a mode change,
// since there is nothing to compare against. Everything else — same agent,
// same identity, same mode — reuses and resumes the existing segment.
func prepareDeliverySegment(ctx context.Context, opts Options, req Request, session store.Session, ref naming.Ref, firstRef string, p *deliveryPlan) (store.Segment, *agent.Delivery, error) {
	if req.Continuation != nil {
		segment, err := opts.DB.GetSegment(ctx, req.Continuation.SegmentID)
		if err != nil {
			return store.Segment{}, nil, err
		}
		segment.DeliveryIdentity, segment.DeliveryRoot, err = opts.DB.SegmentDelivery(ctx, segment.ID)
		if err != nil {
			return store.Segment{}, nil, err
		}
		identity := ""
		if p != nil {
			identity = p.identity
		}
		if identity != segment.DeliveryIdentity {
			return store.Segment{}, nil, fmt.Errorf("automatic recovery instruction bundle changed; resume manually")
		}
		if p == nil {
			return segment, nil, nil
		}
		delivery, err := verifyDelivery(ctx, segment.DeliveryRoot, p)
		return segment, delivery, err
	}
	// No p==nil/HasSessionDelivery fast path here: a bundleless repository's
	// segment still has to clear the mode comparison below on every turn, not
	// just the vendor-switch reconciliation, so it goes through the same
	// ListSegments ladder as a bundle-configured one — the identity check
	// just degenerates to "always empty" for it, per this function's doc
	// comment.
	segments, err := opts.DB.ListSegments(ctx, session.ID)
	if err != nil {
		return store.Segment{}, nil, err
	}
	ordinal, sessionRef := store.FirstSegmentOrdinal, firstRef
	identity := ""
	if p != nil {
		identity = p.identity
	}
	if len(segments) > 0 {
		last := segments[len(segments)-1]
		last.DeliveryIdentity, last.DeliveryRoot, err = opts.DB.SegmentDelivery(ctx, last.ID)
		if err != nil {
			return store.Segment{}, nil, err
		}
		previous, found, err := previousRun(ctx, opts, session, last)
		if err != nil {
			return store.Segment{}, nil, err
		}
		modeChanged := found && previous.Mode != req.mode()
		switch {
		case last.Agent != string(req.Agent):
			// A switch: keep this row's ordinal and the session_ref that was
			// current before the switch (reconcileSegmentAgent below is what
			// actually replaces it with a fresh one) and fall through to the
			// same tail every other path uses — it writes a bundle already
			// rendered for the new agent, finds this same row again, and
			// reconciles it. Never the rotation case below, which is only
			// for a same-agent instruction or mode change: checking the
			// agent first here means a turn that changes both the agent and
			// the mode together still only reconciles, it never also
			// rotates for the mode.
			ordinal, sessionRef = last.Ordinal, last.SessionRef
		case last.DeliveryIdentity == identity && !modeChanged:
			if p == nil {
				return last, nil, nil
			}
			delivery, err := verifyDelivery(ctx, last.DeliveryRoot, p)
			return last, delivery, err
		default:
			ordinal = last.Ordinal + 1
			sessionRef, err = naming.SessionRef(ref, ordinal)
			if err != nil {
				return store.Segment{}, nil, err
			}
			// Only this case is an actual rotation — the switch case above
			// keeps last.Ordinal and never reaches here through anything but
			// a real mode-or-identity-driven rotation, so the log line stays
			// inside it rather than after the switch.
			opts.Logger.Info("rotating conversation segment for a mode or instruction-bundle change",
				slog.String("previous_mode", previous.Mode),
				slog.String("admitted_mode", req.mode()),
				slog.Int("old_ordinal", last.Ordinal),
				slog.Int("new_ordinal", ordinal))
		}
	}
	var delivery *agent.Delivery
	if p != nil {
		root := filepath.Join(opts.Paths.BundlesDir(), ref.Forge, ref.Owner, ref.Repo, strconv.Itoa(ref.Issue))
		relative, relErr := filepath.Rel(opts.Paths.Home(), root)
		if relErr != nil {
			return store.Segment{}, nil, relErr
		}
		delivery, err = writeDelivery(ctx, opts.Paths.Home(), relative, p)
		if err != nil {
			return store.Segment{}, nil, err
		}
	}
	segment, err := opts.DB.GetOrCreateSegment(ctx, session.ID, ordinal, sessionRef, string(req.Agent))
	if err != nil {
		return store.Segment{}, nil, err
	}
	segment, err = reconcileSegmentAgent(ctx, opts, segment, string(req.Agent))
	if err != nil {
		return store.Segment{}, nil, err
	}
	if delivery != nil {
		if err := opts.DB.SetSegmentDelivery(ctx, segment.ID, delivery.Identity, delivery.Root); err != nil {
			return store.Segment{}, nil, err
		}
		segment.DeliveryIdentity, segment.DeliveryRoot = delivery.Identity, delivery.Root
	}
	return segment, delivery, nil
}

// reconcileSegmentAgent brings segment's stored agent in line with kind when
// an earlier step in this function fetched or created a row for a different
// agent than the one this turn resolved to — the issue's label changed since
// that row was written. It is the single funnel both call sites in
// [prepareDeliverySegment] route through, so a switch is handled exactly
// once regardless of which path found the stale row.
//
// It is a no-op, and touches the store not at all, when segment already
// belongs to kind: every ordinary call through this function's tail —
// unchanged agent, a brand new segment created under kind directly, a
// same-agent rotation — reaches this as a no-op.
//
// Otherwise it performs the switch: a fresh session id this row has never
// held before, decoupled from internal/naming's derivation, so the newly
// selected agent always cold-starts, even switching back to a tool this
// segment used before an earlier switch away from it. AgentSwitched is set
// true on the returned [store.Segment] so [resumeDecision] can force that
// cold start unconditionally, without inspecting run history or any
// coding-agent-specific rule.
func reconcileSegmentAgent(ctx context.Context, opts Options, segment store.Segment, kind string) (store.Segment, error) {
	if segment.Agent == kind {
		return segment, nil
	}
	freshRef, err := freshSegmentSessionRef()
	if err != nil {
		return store.Segment{}, fmt.Errorf("mint a fresh session ref for the switched segment: %w", err)
	}
	if err := opts.DB.SetSegmentAgent(ctx, segment.ID, kind, freshRef); err != nil {
		return store.Segment{}, err
	}
	segment.Agent = kind
	segment.SessionRef = freshRef
	segment.ResumeToken = ""
	segment.AgentSwitched = true
	return segment, nil
}

// freshSegmentSessionRef mints a session id no segment row has ever held
// before, for the session_ref a coding-agent switch always assigns
// ([store.DB.SetSegmentAgent]). It matches the canonical UUID shape
// internal/naming's own derived refs share — [harness.Runner] still validates
// that shape regardless of how the id was produced — but the value here is
// random, not derived: a switch's entire point is a session id nothing has
// used before, and deriving it from anything defeats that.
func freshSegmentSessionRef() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate random session ref bytes: %w", err)
	}
	b[6] = b[6]&0x0f | 0x50 // version 5, matching the shape naming.SessionRef produces
	b[8] = b[8]&0x3f | 0x80 // RFC 4122 variant
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

func deliveryPlanRefusal(p *deliveryPlan, cause error) error {
	return agent.DeliveryRefusal(agent.Request{Workflow: p.workflow, Mode: p.evidence.Mode, Graph: &agent.GraphInvocation{Node: p.node}}, cause)
}
