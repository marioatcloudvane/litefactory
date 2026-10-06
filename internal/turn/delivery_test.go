package turn

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/render"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/teamsource"
)

func deliveryFixture() (Options, Request, *teamsource.ResearchDefinition) {
	node := teamsource.ResearchNode{Role: render.ResolvedSource{Frontmatter: render.Frontmatter{Name: "researcher", Description: "Research", Tools: []string{"Read"}}, Body: "SPECIALIST_ONLY_BODY"}, Task: "Investigate", Tier: "balanced", Files: map[string][]byte{"research.md": []byte("SPECIALIST_ONLY_BODY")}}
	return Options{Tiers: config.Tiers{ModelByTier: map[config.Kind]map[config.Tier]string{"codex": {"balanced": "local-model"}}}}, Request{Agent: "codex", Mode: agent.ModeResearch, Tools: []string{"Read"}, CodexSandbox: "read-only"}, &teamsource.ResearchDefinition{Profile: &graphexec.Profile{}, Digest: strings.Repeat("a", 64), Nodes: map[string]teamsource.ResearchNode{"first": node}}
}

func TestPlanDeliverySelectedRosterAndIdentity(t *testing.T) {
	t.Parallel()
	opts, req, definition := deliveryFixture()
	baseline, err := planResearchDelivery(opts, req, definition, "first")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(baseline.roles, []string{"researcher"}) || baseline.mainModel != "local-model" {
		t.Fatalf("selected role/model: %+v", baseline)
	}
	same, err := planResearchDelivery(opts, req, definition, "first")
	if err != nil || same.identity != baseline.identity {
		t.Fatalf("stable identity: %v", err)
	}
	opts.Tiers.ModelByTier["codex"]["balanced"] = "other-model"
	changed, err := planResearchDelivery(opts, req, definition, "first")
	if err != nil || changed.identity == baseline.identity {
		t.Fatalf("changed model did not rotate identity: %v", err)
	}
}

func TestDeliveryConcurrentPublicationIsDisjoint(t *testing.T) {
	t.Parallel()
	o, r, s := deliveryFixture()
	p, err := planResearchDelivery(o, r, s, "first")
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	const count = 8
	roots := make(chan string, count)
	errors := make(chan error, count)
	var wg sync.WaitGroup
	for range count {
		wg.Go(func() {
			d, err := writeDelivery(t.Context(), home, "bundles/shared/1", p)
			if err != nil {
				errors <- err
				return
			}
			roots <- d.Root
		})
	}
	wg.Wait()
	close(roots)
	close(errors)
	for err := range errors {
		t.Error(err)
	}
	seen := map[string]bool{}
	for root := range roots {
		if seen[root] {
			t.Errorf("publication reused %s", root)
		}
		seen[root] = true
		if _, err := verifyDelivery(root, p); err != nil {
			t.Error(err)
		}
	}
	if len(seen) != count {
		t.Fatalf("independent bundles=%d, want %d", len(seen), count)
	}
}

func TestVerifyDeliveryRejectsAlteredResources(t *testing.T) {
	t.Parallel()
	for _, damage := range []string{"clean", "changed", "missing", "extra", "symlink"} {
		t.Run(damage, func(t *testing.T) {
			o, r, s := deliveryFixture()
			p, err := planResearchDelivery(o, r, s, "first")
			if err != nil {
				t.Fatal(err)
			}
			d, err := writeDelivery(t.Context(), t.TempDir(), "bundles/test/1", p)
			if err != nil {
				t.Fatal(err)
			}
			var rel string
			for path := range p.files {
				rel = path
				break
			}
			path := filepath.Join(d.Root, rel)
			switch damage {
			case "changed":
				err = os.WriteFile(path, []byte("tampered"), 0600)
			case "missing":
				err = os.Remove(path)
			case "extra":
				err = os.WriteFile(filepath.Join(d.Root, "extra.md"), []byte("injected"), 0600)
			case "symlink":
				err = os.Remove(path)
				if err == nil {
					err = os.Symlink(filepath.Join(t.TempDir(), "missing"), path)
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			_, err = verifyDelivery(d.Root, p)
			if (err == nil) != (damage == "clean") {
				t.Fatalf("verify %s: %v", damage, err)
			}
		})
	}
}

func TestDeliverySegmentRetainsAndRotatesExactIdentity(t *testing.T) {
	t.Parallel()
	f := newFixture(t, nil)
	opts, req, snapshot := deliveryFixture()
	opts.DB, opts.Paths, opts.Logger = f.db, f.paths, f.opts.Logger
	req.SessionID = f.session.ID
	p, err := planResearchDelivery(opts, req, snapshot, "first")
	if err != nil {
		t.Fatal(err)
	}
	first, bundle, err := prepareDeliverySegment(t.Context(), opts, req, f.session, testRef(t), "delivery-session", p)
	if err != nil {
		t.Fatal(err)
	}
	again, retained, err := prepareDeliverySegment(t.Context(), opts, req, f.session, testRef(t), "delivery-session", p)
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != first.ID || retained.Root != bundle.Root {
		t.Fatal("equal identity did not reuse exact retained segment and resources")
	}
	opts.Tiers.ModelByTier["codex"]["balanced"] = "changed-model"
	changed, err := planResearchDelivery(opts, req, snapshot, "first")
	if err != nil {
		t.Fatal(err)
	}
	next, replacement, err := prepareDeliverySegment(t.Context(), opts, req, f.session, testRef(t), "delivery-session", changed)
	if err != nil {
		t.Fatal(err)
	}
	if next.ID == first.ID || replacement.Root == bundle.Root {
		t.Fatal("changed identity reused segment or bundle")
	}
	if _, err := verifyDelivery(bundle.Root, p); err != nil {
		t.Fatalf("rotation invalidated resumable resources: %v", err)
	}
	identity, root, err := f.db.SegmentDelivery(t.Context(), first.ID)
	if err != nil || identity != p.identity || root != bundle.Root {
		t.Fatalf("persisted identity=%q root=%q error=%v", identity, root, err)
	}
	for name := range changed.files {
		if err := os.Remove(filepath.Join(replacement.Root, name)); err != nil {
			t.Fatal(err)
		}
		break
	}
	if _, _, err := prepareDeliverySegment(t.Context(), opts, req, f.session, testRef(t), "delivery-session", changed); err == nil {
		t.Fatal("resume accepted missing retained resource")
	}
}

// TestDeliverySegmentSwitchReconcilesDeliveryForTheNewAgent is AC-018: when a
// built-in workflow bundle is configured, a coding-agent switch re-pins delivery for the
// newly resolved agent on the very same segment row — agent.Request.Delivery
// comes back non-nil, and the identity recorded against the segment is
// exactly what [planDelivery] produces for the new agent, not a leftover
// pin from the agent the segment previously belonged to.
func TestDeliverySegmentSwitchReconcilesDeliveryForTheNewAgent(t *testing.T) {
	t.Parallel()
	f := newFixture(t, nil)

	claudeOpts, claudeReq, claudeSnapshot := deliveryFixture()
	claudeOpts.Tiers.ModelByTier["claude"] = map[config.Tier]string{"balanced": "claude-model"}
	claudeOpts.DB, claudeOpts.Paths = f.db, f.paths
	claudeReq.SessionID = f.session.ID
	claudeReq.Agent, claudeReq.CodexSandbox = "claude", ""
	claudePlan, err := planResearchDelivery(claudeOpts, claudeReq, claudeSnapshot, "first")
	if err != nil {
		t.Fatal(err)
	}
	first, firstDelivery, err := prepareDeliverySegment(
		t.Context(), claudeOpts, claudeReq, f.session, testRef(t), "claude-session-ref", claudePlan)
	if err != nil {
		t.Fatal(err)
	}
	if first.Agent != string(store.AgentClaude) || firstDelivery == nil {
		t.Fatalf("seeded segment = %+v delivery=%v, want a Claude segment with a delivery pinned", first, firstDelivery)
	}

	codexOpts, codexReq, codexSnapshot := deliveryFixture()
	codexOpts.DB, codexOpts.Paths = f.db, f.paths
	codexReq.SessionID = f.session.ID
	codexPlan, err := planResearchDelivery(codexOpts, codexReq, codexSnapshot, "first")
	if err != nil {
		t.Fatal(err)
	}
	if codexPlan.identity == claudePlan.identity {
		t.Fatal("fixture bug: the two agents' plans must not already share an identity")
	}

	switched, switchedDelivery, err := prepareDeliverySegment(
		t.Context(), codexOpts, codexReq, f.session, testRef(t), "claude-session-ref", codexPlan)
	if err != nil {
		t.Fatal(err)
	}
	if switched.ID != first.ID {
		t.Fatalf("switch started a new segment: first=%d switched=%d", first.ID, switched.ID)
	}
	if switched.Agent != string(store.AgentCodex) {
		t.Fatalf("switched segment agent = %q, want %q", switched.Agent, store.AgentCodex)
	}
	if switchedDelivery == nil {
		t.Fatal("agent.Request.Delivery is nil after a switch with a built-in workflow configured")
	}
	if switchedDelivery.Identity != codexPlan.identity {
		t.Errorf("delivered identity = %q, want the new agent's planDelivery identity %q",
			switchedDelivery.Identity, codexPlan.identity)
	}

	identity, root, err := f.db.SegmentDelivery(t.Context(), switched.ID)
	if err != nil {
		t.Fatalf("read the segment's recorded delivery: %v", err)
	}
	if identity != codexPlan.identity || root != switchedDelivery.Root {
		t.Errorf("recorded delivery = %q/%q, want %q/%q", identity, root, codexPlan.identity, switchedDelivery.Root)
	}
}

// planFixtureDelivery exercises native workflow rendering across admission modes.
func planFixtureDelivery(opts Options, req Request, definition *teamsource.ResearchDefinition, _ string) (*deliveryPlan, error) {
	policy := ResearchWorkflowPolicy()
	policy.Mode = req.Mode
	return planWorkflowDelivery(opts, req, definition, "first", policy)
}
