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
	"github.com/cloudvane/litefactory/internal/render"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/team"
	"github.com/cloudvane/litefactory/internal/teamsource"
)

func deliveryFixture() (Options, Request, *teamsource.Snapshot) {
	return Options{Tiers: config.Tiers{ModelByTier: map[config.Kind]map[config.Tier]string{"codex": {"balanced": "local-model"}}}}, Request{Agent: "codex", Mode: agent.ModeResearch, Tools: []string{"Read"}, CodexSandbox: "read-only"}, &teamsource.Snapshot{
		Team: "go-team", Mode: "research", Digest: strings.Repeat("a", 64),
		Agents: []teamsource.CapturedAgent{{Definition: team.AgentEntry{ID: "researcher", Desc: "Research", Path: "agents/researcher.md", Tier: "balanced"}, Source: render.ResolvedSource{Frontmatter: render.Frontmatter{Name: "researcher", Description: "Research", Tools: []string{"Read"}}, Body: "SPECIALIST_ONLY_BODY"}}},
		Files:  map[string][]byte{"agents/researcher.md": []byte("SPECIALIST_ONLY_BODY")}, Hashes: map[string]string{"agents/researcher.md": strings.Repeat("b", 64)},
		Provenance: &teamsource.ReleaseProvenance{SourceURL: "https://example.com/teams.json", Version: "1.0.0", ManifestDigest: strings.Repeat("c", 64)},
	}
}

func TestPlanDeliverySelectedRosterAndIdentity(t *testing.T) {
	t.Parallel()
	opts, req, snapshot := deliveryFixture()
	worktree := t.TempDir()
	baseline, err := planDelivery(opts, req, snapshot, worktree)
	if err != nil {
		t.Fatal(err)
	}
	for _, rule := range []string{"instead of reopening its local SKILL.md", "fallback only when no definition was supplied", "not already supplied in full"} {
		if !strings.Contains(string(baseline.files["bootstrap.md"]), rule) {
			t.Errorf("bootstrap lost role precedence: %s", rule)
		}
	}
	if !reflect.DeepEqual(baseline.roles, []string{"researcher"}) {
		t.Fatalf("roster=%v", baseline.roles)
	}
	if len(baseline.evidence.Specialists) != 1 || baseline.evidence.Specialists[0].Model != "local-model" || baseline.evidence.Version != "1.0.0" {
		t.Fatalf("provenance=%+v", baseline.evidence)
	}
	for _, tc := range []struct {
		name   string
		mutate func(*Options, *Request, *teamsource.Snapshot)
	}{
		{"same", func(*Options, *Request, *teamsource.Snapshot) {}},
		{"version", func(_ *Options, _ *Request, s *teamsource.Snapshot) { s.Provenance.Version = "1.1.0" }},
		{"source", func(_ *Options, _ *Request, s *teamsource.Snapshot) {
			s.Provenance.SourceURL = "https://corporate.example/teams.json"
		}},
		{"manifest", func(_ *Options, _ *Request, s *teamsource.Snapshot) {
			s.Provenance.ManifestDigest = strings.Repeat("d", 64)
		}},
		{"team", func(_ *Options, _ *Request, s *teamsource.Snapshot) { s.Team = "python-team" }},
		{"mode", func(_ *Options, r *Request, s *teamsource.Snapshot) { r.Mode = agent.ModeValidate; s.Mode = "validate" }},
		{"model", func(o *Options, _ *Request, _ *teamsource.Snapshot) {
			o.Tiers.ModelByTier["codex"]["balanced"] = "other-model"
		}},
		{"body", func(_ *Options, _ *Request, s *teamsource.Snapshot) { s.Agents[0].Source.Body = "changed" }},
		{"override", func(_ *Options, _ *Request, s *teamsource.Snapshot) { s.Agents[0].Override = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o, r, s := deliveryFixture()
			tc.mutate(&o, &r, s)
			got, err := planDelivery(o, r, s, worktree)
			if err != nil {
				t.Fatal(err)
			}
			if (got.identity == baseline.identity) != (tc.name == "same") {
				t.Fatalf("identity equality=%v", got.identity == baseline.identity)
			}
		})
	}
}

func TestDeliveryConcurrentPublicationIsDisjoint(t *testing.T) {
	t.Parallel()
	o, r, s := deliveryFixture()
	p, err := planDelivery(o, r, s, t.TempDir())
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
			p, err := planDelivery(o, r, s, t.TempDir())
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
	p, err := planDelivery(opts, req, snapshot, t.TempDir())
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
	snapshot.Provenance.Version = "2.0.0"
	changed, err := planDelivery(opts, req, snapshot, t.TempDir())
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
// team snapshot is configured, a coding-agent switch re-pins delivery for the
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
	claudePlan, err := planDelivery(claudeOpts, claudeReq, claudeSnapshot, t.TempDir())
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
	codexPlan, err := planDelivery(codexOpts, codexReq, codexSnapshot, t.TempDir())
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
		t.Fatal("agent.Request.Delivery is nil after a switch with a team snapshot configured")
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
