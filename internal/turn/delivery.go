package turn

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/render"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/team"
	"github.com/cloudvane/litefactory/internal/teamsource"
)

type deliveryPlan struct {
	identity string
	files    map[string][]byte
	roles    []string
	skills   []string
	evidence agent.DeliveryEvidence
}

// planDelivery reads only the locally installed closure. Network refresh belongs
// to the management CLI, never to work, resume, classification or daemon startup.
func planDelivery(opts Options, req Request, snapshot *teamsource.Snapshot, worktree string) (*deliveryPlan, error) {
	if snapshot == nil {
		return nil, nil
	}
	p := &deliveryPlan{files: make(map[string][]byte)}
	p.evidence = agent.DeliveryEvidence{Team: snapshot.Team, Mode: snapshot.Mode, SnapshotDigest: snapshot.Digest, ResourceHashes: map[string]string{}}
	if source := snapshot.Provenance; source != nil {
		p.evidence.SourceURL = source.SourceURL
		p.evidence.SourceAlias = source.SourceAlias
		p.evidence.Version = source.Version
		p.evidence.ManifestDigest = source.ManifestDigest
	}
	for path, raw := range snapshot.Files {
		if path != "index.json" {
			p.files["sources/"+path] = slices.Clone(raw)
			p.evidence.SourceBytes += len(raw)
			p.evidence.ResourceHashes[path] = snapshot.Hashes[path]
		}
		if strings.HasPrefix(path, "skills/") {
			p.files["plugin/"+path] = slices.Clone(raw)
			if strings.HasSuffix(path, "/SKILL.md") {
				p.skills = append(p.skills, "sources/"+path)
			}
		}
	}
	sort.Strings(p.skills)
	sandbox := req.CodexSandbox
	if sandbox == "" {
		sandbox = "read-only"
	}
	for _, captured := range snapshot.Agents {
		captured.Source.Body = "Supporting resource paths in this definition are relative to sources/" + filepath.ToSlash(filepath.Dir(captured.Definition.Path)) + " inside the instruction bundle.\n\n" + captured.Source.Body
		model, ok := opts.Tiers.ResolveModel(config.Kind(req.Agent), config.Tier(captured.Definition.Tier))
		if !ok {
			return nil, fmt.Errorf("missing local %s model for specialist tier %s", req.Agent, captured.Definition.Tier)
		}
		p.evidence.Specialists = append(p.evidence.Specialists, agent.SpecialistEvidence{ID: captured.Definition.ID, Tier: captured.Definition.Tier, Model: model, SourcePath: captured.Definition.Path, SourceHash: snapshot.Hashes[captured.Definition.Path], Override: captured.Override})
		// The parent mode is a ceiling, not an additive permission hint.
		tools := []string{}
		for _, tool := range captured.Source.Frontmatter.Tools {
			if slices.Contains(req.Tools, tool) {
				tools = append(tools, tool)
			}
		}
		file, err := render.Native(string(req.Agent), render.NativeRole{Source: captured.Source, Model: model, Tools: tools, Skills: slices.Clone(captured.Definition.Skills)}, sandbox)
		if err != nil {
			return nil, err
		}
		p.roles = append(p.roles, captured.Definition.ID)
		p.files[file.Path] = file.Bytes
	}
	p.files["plugin/.claude-plugin/plugin.json"] = []byte("{\"name\":\"litefactory-team\",\"version\":\"1.0.0\"}\n")
	provenance, err := json.Marshal(struct {
		Team     string                        `json:"team"`
		Mode     string                        `json:"mode"`
		Snapshot string                        `json:"snapshot"`
		Source   *teamsource.ReleaseProvenance `json:"source,omitempty"`
		Roles    []string                      `json:"roles"`
	}{snapshot.Team, snapshot.Mode, snapshot.Digest, snapshot.Provenance, p.roles})
	if err != nil {
		return nil, fmt.Errorf("encode delivery provenance: %w", err)
	}
	p.files["provenance.json"] = append(provenance, '\n')
	listedRoles := slices.Clone(p.roles)
	if string(req.Agent) == "claude" {
		for i, name := range listedRoles {
			listedRoles[i] = "litefactory-team:" + name
		}
	}
	bootstrap := "The daemon has selected mode " + req.mode() + ". Use only its ordered specialist roster: " + strings.Join(listedRoles, ", ") + ". The first orchestrator, when present, sequences the remaining specialists. Do not change mode. Supporting resources are retained in this bundle's sources/ directory. Available supporting skills: " + strings.Join(p.skills, ", ") + ". Use the supplied role definition instead of reopening its local SKILL.md or agent file. Local role files are fallback only when no definition was supplied. Read a supporting skill only when relevant and not already supplied in full. Respect ordinary project instructions and the parent permission boundary.\n"
	p.files["bootstrap.md"] = []byte(bootstrap)
	projectName := "AGENTS.md"
	if string(req.Agent) == "claude" {
		projectName = "CLAUDE.md"
	}
	projectRoot, err := os.OpenRoot(worktree)
	if err != nil {
		return nil, err
	}
	defer projectRoot.Close()
	info, err := projectRoot.Lstat(projectName)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	if err == nil {
		if !info.Mode().IsRegular() || info.Size() > 1<<20 {
			return nil, fmt.Errorf("project instructions must be a regular file of at most 1 MiB")
		}
		project, err := projectRoot.ReadFile(projectName)
		if err != nil {
			return nil, err
		}
		ordinary, err := ordinaryProjectInstructions(string(project))
		if err != nil {
			return nil, err
		}
		p.files["bootstrap.md"] = []byte(bootstrap + "\nProject instructions (" + projectName + "):\n" + ordinary)
	}
	parentIdentity := ""
	if runner, ok := opts.Runners.runner(req.Agent); ok {
		if identified, ok := runner.(interface{ InstructionIdentity() string }); ok {
			parentIdentity = identified.InstructionIdentity()
		}
	}
	encoded, err := json.Marshal(struct {
		Parent   string
		Version  string
		Snapshot string
		Kind     agent.Kind
		Files    map[string][]byte
		Tools    []string
		Allowed  []string
		Sandbox  string
		Evidence agent.DeliveryEvidence
	}{parentIdentity, render.NativeVersion, snapshot.Digest, req.Agent, p.files, req.Tools, req.AllowedTools, sandbox, p.evidence})
	if err != nil {
		return nil, fmt.Errorf("hash delivery: %w", err)
	}
	p.identity = team.HashBytes(encoded)
	return p, nil
}

// Legacy managed root sections contain complete specialists. Preserve ordinary
// instructions around them, but never reintroduce the full old catalog.
func ordinaryProjectInstructions(raw string) (string, error) {
	var out strings.Builder
	inside := false
	for _, line := range strings.SplitAfter(raw, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "<!-- litefactory:team BEGIN ") {
			if inside {
				return "", fmt.Errorf("nested managed project instruction section")
			}
			inside = true
			continue
		}
		if trimmed == "<!-- litefactory:team END -->" {
			if !inside {
				return "", fmt.Errorf("unmatched managed project instruction section")
			}
			inside = false
			continue
		}
		if !inside {
			out.WriteString(line)
		}
	}
	if inside {
		return "", fmt.Errorf("unterminated managed project instruction section")
	}
	return out.String(), nil
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
	d := agent.Delivery{Root: dir, Identity: p.identity, Roles: p.roles, Skills: p.skills, Evidence: p.evidence}.Clone()
	return &d, nil
}

// verifyDelivery refuses missing or edited retained bytes rather than silently
// rebuilding the resources a previous conversation actually saw.
func verifyDelivery(root string, p *deliveryPlan) (*agent.Delivery, error) {
	r, err := os.OpenRoot(root)
	if err != nil {
		return nil, fmt.Errorf("open retained bundle: %w", err)
	}
	defer r.Close()
	if err := fs.WalkDir(r.FS(), ".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("retained bundle contains a symlink")
		}
		if !entry.IsDir() {
			if _, ok := p.files[path]; !ok {
				return fmt.Errorf("retained bundle contains undeclared file: %s", path)
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}
	for path, expected := range p.files {
		info, err := r.Lstat(path)
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() || info.Size() != int64(len(expected)) {
			return nil, fmt.Errorf("retained bundle resource is not the expected regular file")
		}
		actual, err := r.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read retained bundle %s: %w", path, err)
		}
		if team.HashBytes(actual) != team.HashBytes(expected) {
			return nil, fmt.Errorf("retained bundle was modified: %s", path)
		}
	}
	d := agent.Delivery{Root: root, Identity: p.identity, Roles: p.roles, Skills: p.skills, Evidence: p.evidence}.Clone()
	return &d, nil
}

// The task lock owns this read/create sequence. A label change that resolves
// to a different coding-agent tool than the one already owning this session's
// last segment is a switch, not a refusal: the tail of this function
// reconciles the stale-agent row to the newly resolved tool in place — same
// id, same ordinal — via [reconcileSegmentAgent], writes a bundle rendered
// for the new tool, and re-pins delivery for it. Only a change in effective
// instructions under the *same* tool still rotates to a new segment.
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
		delivery, err := verifyDelivery(segment.DeliveryRoot, p)
		return segment, delivery, err
	}
	if p == nil {
		hadDelivery, err := opts.DB.HasSessionDelivery(ctx, session.ID)
		if err != nil {
			return store.Segment{}, nil, err
		}
		if !hadDelivery {
			segment, err := opts.DB.GetOrCreateSegment(ctx, session.ID, store.FirstSegmentOrdinal, firstRef, string(req.Agent))
			if err != nil {
				return store.Segment{}, nil, err
			}
			segment, err = reconcileSegmentAgent(ctx, opts, segment, string(req.Agent))
			return segment, nil, err
		}
	}
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
		switch {
		case last.Agent != string(req.Agent):
			// A switch: keep this row's ordinal and the session_ref that was
			// current before the switch (reconcileSegmentAgent below is what
			// actually replaces it with a fresh one) and fall through to the
			// same tail every other path uses — it writes a bundle already
			// rendered for the new agent, finds this same row again, and
			// reconciles it. Never the rotation case below, which is only
			// for a same-agent instruction change.
			ordinal, sessionRef = last.Ordinal, last.SessionRef
		case last.DeliveryIdentity == identity:
			if p == nil {
				return last, nil, nil
			}
			delivery, err := verifyDelivery(last.DeliveryRoot, p)
			return last, delivery, err
		default:
			ordinal = last.Ordinal + 1
			sessionRef, err = naming.SessionRef(ref, ordinal)
			if err != nil {
				return store.Segment{}, nil, err
			}
		}
		opts.Logger.Info("rotating conversation segment for a mode or instruction-bundle change",
			slog.String("previous_mode", previous.Mode),
			slog.String("admitted_mode", req.mode()),
			slog.Int("old_ordinal", last.Ordinal),
			slog.Int("new_ordinal", ordinal))
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
