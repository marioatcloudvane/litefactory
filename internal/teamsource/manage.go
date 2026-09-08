package teamsource

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/team"
)

const metadataPath = ".litefactory/catalog.json"

// Installation records imported provenance without making runtime reads depend
// on the original package. Hashes describe the last successful publication.
type Installation struct {
	Remote      *ReleaseProvenance         `json:"remote,omitempty"`
	Observation *UpstreamObservation       `json:"observation,omitempty"`
	Origin      string                     `json:"origin"`
	Hashes      map[string]string          `json:"hashes"`
	Definition  team.TeamEntry             `json:"definition"`
	Shared      map[string]team.AgentEntry `json:"shared"`
}

// CatalogMetadata owns installation baselines and explicit source overrides.
type CatalogMetadata struct {
	DigestHistory map[string]string       `json:"digest_history,omitempty"`
	Teams         map[string]Installation `json:"teams"`
	Overrides     map[string]Override     `json:"overrides"`
}

// Override records the effective central source and its retained base definition.
type Override struct {
	Origin   string                  `json:"origin"`
	Path     string                  `json:"path"`
	Hash     string                  `json:"hash"`
	Original team.AgentEntry         `json:"original"`
	Closure  map[string]OverrideFile `json:"closure"`
}

// OverrideFile maps one installed dependency to its origin-relative source and
// the bytes recorded at the last successful override publication.
type OverrideFile struct {
	Source string `json:"source"`
	Hash   string `json:"hash"`
}

// CatalogStatus is one deterministic drift observation; Error qualifies missing
// upstream evidence without inventing a sixth drift state.
type CatalogStatus struct {
	Version          string `json:"version,omitempty"`
	CandidateVersion string `json:"candidate_version,omitempty"`
	LatestStable     string `json:"latest_stable,omitempty"`
	Policy           string `json:"policy,omitempty"`
	Freshness        string `json:"freshness,omitempty"`
	LastAttempt      string `json:"last_attempt,omitempty"`
	LastSuccess      string `json:"last_success,omitempty"`
	Team             string `json:"team"`
	Path             string `json:"path"`
	State            string `json:"state"`
	Origin           string `json:"origin"`
	InstalledHash    string `json:"installed_hash"`
	UpstreamHash     string `json:"upstream_hash"`
	Error            string `json:"error,omitempty"`
}

// Metadata returns a separately owned provenance view under the catalog lock.
func Metadata(ctx context.Context, root string) (m *CatalogMetadata, err error) {
	err = team.WithCatalogLock(ctx, root, func() error { var e error; m, e = readMetadata(ctx, root); return e })
	return m, err
}

func baseCatalog(idx *team.Index, m *CatalogMetadata) (*team.Index, error) {
	b, err := json.Marshal(idx)
	if err != nil {
		return nil, err
	}
	cloned, err := team.Decode(b)
	if err != nil {
		return nil, err
	}
	for _, scope := range sortedFileKeys(m.Overrides) {
		if _, err := scopedAgent(cloned, scope); err != nil {
			return nil, err
		}
		replaceScopedAgent(cloned, scope, m.Overrides[scope].Original)
	}
	return cloned, nil
}
func definitionHash(def team.TeamEntry, shared map[string]team.AgentEntry) string {
	b, _ := json.Marshal(struct {
		Definition team.TeamEntry
		Shared     map[string]team.AgentEntry
	}{def, shared})
	return team.HashBytes(b)
}

func readMetadata(ctx context.Context, root string) (*CatalogMetadata, error) {
	r, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	b, err := (&captureReader{ctx: ctx, root: r, base: root}).readFresh(metadataPath)
	if errors.Is(err, fs.ErrNotExist) {
		return &CatalogMetadata{Teams: map[string]Installation{}, Overrides: map[string]Override{}}, nil
	}
	if err != nil {
		return nil, err
	}
	var m CatalogMetadata
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err = d.Decode(&m); err != nil {
		return nil, fmt.Errorf("catalog metadata: %w", err)
	}
	var trailing json.RawMessage
	if err := d.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("catalog metadata must contain exactly one JSON object")
	}
	if m.Teams == nil {
		m.Teams = map[string]Installation{}
	}
	if m.Overrides == nil {
		m.Overrides = map[string]Override{}
	}
	return &m, nil
}

func readIndex(ctx context.Context, root string) (*team.Index, error) {
	r, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	b, err := (&captureReader{ctx: ctx, root: r, base: root}).readFresh("index.json")
	if errors.Is(err, fs.ErrNotExist) {
		return &team.Index{SchemaVersion: 2, Teams: map[string]team.TeamEntry{}, Shared: map[string]team.AgentEntry{}}, nil
	}
	if err != nil {
		return nil, err
	}
	idx, err := team.Decode(b)
	if err != nil {
		return nil, err
	}
	for _, name := range sortedFileKeys(idx.Teams) {
		if err := portableTeamPaths(idx, name, root); err != nil {
			return nil, err
		}
	}
	return idx, nil
}

func capturePackage(ctx context.Context, origin, name string) (*team.Index, map[string][]byte, error) {
	files := map[string][]byte{}
	for _, mode := range team.Modes() {
		s, err := CaptureUnlocked(ctx, origin, name, mode)
		if err != nil {
			return nil, nil, fmt.Errorf("capture package %s: %w", name, err)
		}
		for p, b := range s.Files {
			if old, ok := files[p]; ok && !bytes.Equal(old, b) {
				return nil, nil, fmt.Errorf("source changed during package capture: %s", p)
			}
			files[p] = b
		}
	}
	// Recheck the complete cross-mode closure; disjoint modes must not combine
	// bytes from different source generations.
	r, err := os.OpenRoot(origin)
	if err != nil {
		return nil, nil, err
	}
	defer r.Close()
	c := captureReader{ctx: ctx, root: r, base: origin}
	for _, p := range sortedFileKeys(files) {
		b, e := c.readFresh(p)
		if e != nil {
			return nil, nil, e
		}
		if !bytes.Equal(b, files[p]) {
			return nil, nil, fmt.Errorf("source changed during capture: %s", p)
		}
	}
	idx, err := team.Decode(files["index.json"])
	if err != nil {
		return nil, nil, err
	}
	if err := portableTeamPaths(idx, name, origin); err != nil {
		return nil, nil, err
	}
	return idx, files, nil
}

func portableTeamPaths(idx *team.Index, name, root string) error {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	canonical, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return err
	}
	c := captureReader{base: canonical}
	entry := idx.Teams[name]
	for _, roster := range [][]team.AgentEntry{entry.Research, entry.Plan, entry.Implement, entry.Validate, entry.Spike} {
		for i := range roster {
			a := roster[i]
			if a.Ref != "" {
				a = idx.Shared[a.Ref]
			}
			p, e := c.relative(a.Path)
			if e != nil {
				return e
			}
			a.Path = p
			if roster[i].Ref != "" {
				idx.Shared[roster[i].Ref] = a
			} else {
				roster[i] = a
			}
		}
	}
	idx.Teams[name] = entry
	return nil
}

func sortedFileKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func selectedShared(idx *team.Index, name string) map[string]team.AgentEntry {
	out := map[string]team.AgentEntry{}
	t := idx.Teams[name]
	for _, mode := range team.Modes() {
		roster, _ := t.Mode(mode)
		for _, a := range roster {
			if a.Ref != "" {
				out[a.Ref] = idx.Shared[a.Ref]
			}
		}
	}
	return out
}

// Install explicitly imports one complete team closure. It never renders into
// a repository and never records provenance until every source validates.
func Install(ctx context.Context, root, name, origin string, force bool) error {
	if origin == "" {
		return fmt.Errorf("local import requires an explicit package root; use remote team add for configured default")
	}
	{
		p, err := filepath.Abs(origin)
		if err != nil {
			return err
		}
		origin = p
		canonical, e := filepath.EvalSymlinks(origin)
		if e != nil {
			return e
		}
		origin = canonical
		target, e := filepath.Abs(root)
		if e != nil {
			return e
		}
		if resolved, e := filepath.EvalSymlinks(target); e == nil {
			target = resolved
		}
		for _, pair := range [][2]string{{origin, target}, {target, origin}} {
			rel, e := filepath.Rel(pair[0], pair[1])
			if e != nil {
				return e
			}
			if rel == "." || rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				return fmt.Errorf("source package and installed catalog must not overlap")
			}
		}
	}
	source, files, err := capturePackage(ctx, origin, name)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(root, 0o700); err != nil {
		return err
	}
	return team.WithCatalogLock(ctx, root, func() error { return installUnlocked(ctx, root, name, origin, source, files, force) })
}

func installUnlocked(ctx context.Context, root, name, origin string, source *team.Index, files map[string][]byte, force bool) error {
	return installReleaseUnlocked(ctx, root, name, origin, source, files, force, nil, nil)
}

func installReleaseUnlocked(ctx context.Context, root, name, origin string, source *team.Index, files map[string][]byte, force bool, remote *ReleaseProvenance, observation *UpstreamObservation) error {
	idx, err := readIndex(ctx, root)
	if err != nil {
		return err
	}
	m, err := readMetadata(ctx, root)
	if err != nil {
		return err
	}
	baseIndex, err := baseCatalog(idx, m)
	if err != nil {
		return err
	}
	shared := selectedShared(source, name)
	var faults []error
	for _, id := range sortedFileKeys(shared) {
		a := shared[id]
		if old, ok := baseIndex.Shared[id]; ok && !reflect.DeepEqual(old, a) {
			for other := range idx.Teams {
				if other != name {
					if _, used := selectedShared(idx, other)[id]; used {
						faults = append(faults, fmt.Errorf("shared agent %s conflicts with installed team %s", id, other))
					}
				}
			}
		}
	}
	previous, installed := m.Teams[name]
	var removed []string
	for _, p := range sortedFileKeys(previous.Hashes) {
		if _, retained := files[p]; retained {
			continue
		}
		used := false
		for other, record := range m.Teams {
			if other != name {
				if _, ok := record.Hashes[p]; ok {
					used = true
				}
			}
		}
		for _, o := range m.Overrides {
			if o.Path == p {
				used = true
			}
			if _, ok := o.Closure[p]; ok {
				used = true
			}
		}
		if used {
			continue
		}
		if err := checkInstalledFile(ctx, root, p, nil, previous.Hashes[p], true, force); err != nil {
			faults = append(faults, err)
		}
		removed = append(removed, p)
	}
	for scope := range m.Overrides {
		if scopeAffectsTeam(baseIndex, name, scope) && !scopeAffectsTeam(source, name, scope) {
			faults = append(faults, fmt.Errorf("update removes overridden agent %s; unset override explicitly first", scope))
		}
	}
	if installed && !force && definitionHash(baseIndex.Teams[name], selectedShared(baseIndex, name)) != definitionHash(previous.Definition, previous.Shared) {
		faults = append(faults, fmt.Errorf("team %s registry metadata is locally modified; inspect it or use --force", name))
	}
	for _, p := range sortedFileKeys(files) {
		if p == "index.json" {
			continue
		}
		data := files[p]
		if e := checkInstalledFile(ctx, root, p, data, previous.Hashes[p], installed, force); e != nil {
			faults = append(faults, e)
		}
		for other, record := range m.Teams {
			if other != name {
				if hash, used := record.Hashes[p]; used && hash != team.HashBytes(data) {
					faults = append(faults, fmt.Errorf("source %s conflicts with installed team %s", p, other))
				}
			}
		}
	}
	if err := errors.Join(faults...); err != nil {
		return err
	}
	idx.Teams[name] = source.Teams[name]
	for id := range previous.Shared {
		used := false
		for other := range idx.Teams {
			if _, ok := selectedShared(idx, other)[id]; ok {
				used = true
				break
			}
		}
		if !used {
			delete(idx.Shared, id)
		}
	}
	for id, a := range shared {
		idx.Shared[id] = a
	}
	// An upstream update refreshes the base definition while retaining the
	// explicit override. Unset subsequently returns to this new base.
	for _, scope := range sortedFileKeys(m.Overrides) {
		if scopeAffectsTeam(idx, name, scope) {
			o := m.Overrides[scope]
			base, e := scopedAgent(idx, scope)
			if e != nil {
				return e
			}
			o.Original = base
			effective := base
			effective.Path = o.Path
			replaceScopedAgent(idx, scope, effective)
			m.Overrides[scope] = o
		}
	}
	b, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}
	files["index.json"] = append(b, '\n')
	hashes := map[string]string{}
	for p, data := range files {
		if p != "index.json" {
			hashes[p] = team.HashBytes(data)
		}
	}
	m.Teams[name] = Installation{Origin: origin, Hashes: hashes, Definition: source.Teams[name], Shared: shared, Remote: remote, Observation: observation}
	if remote != nil {
		if err := rememberTeamDigest(m, name, remote); err != nil {
			return err
		}
	}
	if err := validatePublication(ctx, root, idx, m, files); err != nil {
		return err
	}
	metadata, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return team.WriteCatalogTransactionUnlocked(ctx, root, files, append(metadata, '\n'), removed)
}

func validatePublication(ctx context.Context, root string, idx *team.Index, m *CatalogMetadata, files map[string][]byte) error {
	stage, err := os.MkdirTemp("", "litefactory-catalog-check-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	combined := map[string][]byte{}
	r, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer r.Close()
	reader := captureReader{ctx: ctx, root: r, base: root, files: combined, hashes: map[string]string{}, skills: map[string]int{}}
	for _, name := range sortedFileKeys(m.Teams) {
		for _, p := range sortedFileKeys(m.Teams[name].Hashes) {
			b, e := reader.readFresh(p)
			if e == nil {
				combined[p] = b
			} else if !errors.Is(e, fs.ErrNotExist) {
				return e
			}
		}
	}
	for _, scope := range sortedFileKeys(m.Overrides) {
		a, e := scopedAgent(idx, scope)
		if e != nil {
			return e
		}
		if _, provided := files[a.Path]; provided {
			continue
		}
		raw, e := reader.read(a.Path)
		if e != nil {
			return e
		}
		src, e := parseAgentSource(raw)
		if e != nil {
			return e
		}
		if e = reader.agentDependencies(a.Path, src); e != nil {
			return e
		}
		for _, s := range a.Skills {
			if e = reader.skill(s); e != nil {
				return e
			}
		}
	}
	for p, b := range files {
		combined[p] = b
	}
	if err := team.WriteCatalogUnlocked(ctx, stage, combined, nil); err != nil {
		return err
	}
	for _, name := range sortedFileKeys(idx.Teams) {
		for _, mode := range team.Modes() {
			if _, e := CaptureUnlocked(ctx, stage, name, mode); e != nil {
				return fmt.Errorf("effective catalog preflight: %w", e)
			}
		}
	}
	return nil
}

func checkInstalledFile(ctx context.Context, root, p string, data []byte, baseline string, installed, force bool) error {
	r, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer r.Close()
	b, err := (&captureReader{ctx: ctx, root: r, base: root}).readFresh(p)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if bytes.Equal(b, data) {
		return nil
	}
	if !installed || baseline == "" {
		return fmt.Errorf("untracked conflicting source %s", p)
	}
	if team.HashBytes(b) != baseline && !force {
		return fmt.Errorf("locally modified source %s; inspect it or use --force", p)
	}
	return nil
}

// Status compares installed bytes with their last import and current origin.
// Origin failures are explicit evidence; runtime capture remains independent.
func Status(ctx context.Context, root string) (records []CatalogStatus, err error) {
	err = team.WithCatalogLock(ctx, root, func() error {
		m, e := readMetadata(ctx, root)
		if e != nil {
			return e
		}
		idx, e := readIndex(ctx, root)
		if e != nil {
			return e
		}
		baseIndex, e := baseCatalog(idx, m)
		if e != nil {
			return e
		}
		r, e := os.OpenRoot(root)
		if e != nil {
			return e
		}
		defer r.Close()
		for _, name := range sortedFileKeys(m.Teams) {
			record := m.Teams[name]
			if record.Remote != nil {
				remote, e := remoteStatus(ctx, root, name, record, baseIndex, time.Now().UTC())
				if e != nil {
					return e
				}
				records = append(records, remote...)
				continue
			}
			sourceIndex, upstream, originErr := capturePackage(ctx, record.Origin, name)
			if originErr != nil && len(record.Hashes) == 0 {
				records = append(records, CatalogStatus{Team: name, Path: "index.json#" + name, Origin: record.Origin, State: "in-sync", Error: fmt.Sprintf("origin unavailable: %v; installed team remains usable", originErr)})
			}
			if originErr == nil {
				base := definitionHash(record.Definition, record.Shared)
				current := definitionHash(baseIndex.Teams[name], selectedShared(baseIndex, name))
				next := definitionHash(sourceIndex.Teams[name], selectedShared(sourceIndex, name))
				state := driftState(base, current, next)
				if state != "in-sync" || len(record.Hashes) == 0 {
					records = append(records, CatalogStatus{Team: name, Path: "index.json#" + name, State: state, Origin: record.Origin, InstalledHash: current, UpstreamHash: next})
				}
			}
			for _, p := range sortedFileKeys(record.Hashes) {
				b, readErr := (&captureReader{ctx: ctx, root: r, base: root}).readFresh(p)
				if readErr != nil && !errors.Is(readErr, fs.ErrNotExist) {
					return readErr
				}
				item := CatalogStatus{Team: name, Path: p, Origin: record.Origin}
				if readErr == nil {
					item.InstalledHash = team.HashBytes(b)
				}
				if next, ok := upstream[p]; ok {
					item.UpstreamHash = team.HashBytes(next)
				}
				item.State = driftState(record.Hashes[p], item.InstalledHash, item.UpstreamHash)
				if originErr != nil {
					item.Error = fmt.Sprintf("origin unavailable: %v; installed bytes remain usable", originErr)
					item.State = driftState(record.Hashes[p], item.InstalledHash, record.Hashes[p])
				}
				records = append(records, item)
			}
		}
		for _, scope := range sortedFileKeys(m.Overrides) {
			o := m.Overrides[scope]
			a, e := scopedAgent(idx, scope)
			if e != nil {
				return e
			}
			observations, _, e := overrideStatus(ctx, root, scope, o, a)
			if e != nil {
				return e
			}
			records = append(records, observations...)
		}
		return nil
	})
	return records, err
}

func driftState(base, current, next string) string {
	if current == "" {
		return "missing"
	}
	if current == next {
		return "in-sync"
	}
	if current == base {
		return "upstream-updated"
	}
	if next == base {
		return "locally-modified"
	}
	return "conflict"
}

// Update refreshes installed packages and effective overrides from their origins.
func Update(ctx context.Context, root string, force bool) error {
	return UpdateLocalTeam(ctx, root, "", force)
}

// UpdateLocalTeam refreshes one explicit local import and its overrides. Empty
// name preserves the existing all-local update entry point; no HTTP is used.
func UpdateLocalTeam(ctx context.Context, root, name string, force bool) error {
	return team.WithCatalogLock(ctx, root, func() error {
		m, err := readMetadata(ctx, root)
		if err != nil {
			return err
		}
		idx, err := readIndex(ctx, root)
		if err != nil {
			return err
		}
		var errs []error
		failedOverrides := map[string]bool{}
		for _, scope := range sortedFileKeys(m.Overrides) {
			if name != "" && !scopeAffectsTeam(idx, name, scope) {
				continue
			}
			a, e := scopedAgent(idx, scope)
			if e == nil {
				e = refreshOverrideUnlocked(ctx, root, scope, m.Overrides[scope], a, force)
			}
			if e != nil {
				errs = append(errs, e)
				failedOverrides[scope] = true
			}
		}
		for _, teamName := range sortedFileKeys(m.Teams) {
			if name != "" && teamName != name {
				continue
			}
			name := teamName
			blocked := false
			for scope := range failedOverrides {
				if scopeAffectsTeam(idx, name, scope) {
					blocked = true
				}
			}
			if blocked {
				continue
			}
			record := m.Teams[name]
			if record.Remote != nil {
				continue
			}
			source, files, e := capturePackage(ctx, record.Origin, name)
			if e == nil {
				e = installUnlocked(ctx, root, name, record.Origin, source, files, force)
			}
			if e != nil {
				errs = append(errs, fmt.Errorf("update %s: %w", name, e))
			}
		}
		return errors.Join(errs...)
	})
}

func readCatalogFile(ctx context.Context, root, p string) ([]byte, error) {
	r, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return (&captureReader{ctx: ctx, root: r, base: root}).readFresh(p)
}
