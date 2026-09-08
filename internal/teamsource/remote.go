package teamsource

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"reflect"
	"time"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/team"
	"github.com/cloudvane/litefactory/internal/teamremote"
)

// ReleaseProvenance is safe to persist and pass to runner evidence: no secrets
// or credential references. A saved URL never follows later alias retargeting.
type ReleaseProvenance struct {
	SourceAlias    string `json:"source_alias,omitempty"`
	SourceURL      string `json:"source_url"`
	CanonicalURL   string `json:"canonical_url"`
	Version        string `json:"version"`
	ManifestDigest string `json:"manifest_digest"`
	Policy         string `json:"policy"`
	ExpectedSHA256 string `json:"expected_sha256,omitempty"`
}

// UpstreamObservation stores verified hashes, allowing offline status even if
// the disposable download cache has been evicted.
type UpstreamObservation struct {
	LastAttempt    time.Time         `json:"last_attempt"`
	LastSuccess    time.Time         `json:"last_success"`
	Error          string            `json:"error,omitempty"`
	Version        string            `json:"version,omitempty"`
	LatestStable   string            `json:"latest_stable,omitempty"`
	ManifestDigest string            `json:"manifest_digest,omitempty"`
	DefinitionHash string            `json:"definition_hash,omitempty"`
	Hashes         map[string]string `json:"hashes,omitempty"`
}

// RemoteOptions describes explicit operator choices for one install or refresh.
type RemoteOptions struct {
	SwitchSource bool
	SourceAlias  string
	SourceURL    string
	Version      string
	SHA256       string
	ClearPin     bool
	Force        bool
	RefreshOnly  bool
	Now          time.Time
}

// Fetcher permits deterministic fixture acquisition while CLI supplies HTTPS.
type Fetcher interface {
	Fetch(context.Context, string, string, string, string) (*teamremote.Package, error)
}

// RemoteInstall fetches outside the catalog lock and rechecks the entire
// captured installation baseline under lock before publication or observation.
func RemoteInstall(ctx context.Context, paths config.Paths, name string, fetcher Fetcher, opts RemoteOptions) error {
	if !team.KeyPattern.MatchString(name) {
		return fmt.Errorf("invalid team identifier")
	}
	if opts.Now.IsZero() {
		opts.Now = time.Now().UTC()
	}
	if err := os.MkdirAll(paths.TeamsDir(), 0o700); err != nil {
		return err
	}
	before, err := Metadata(ctx, paths.TeamsDir())
	if err != nil {
		return err
	}
	old, exists := before.Teams[name]
	var localBefore map[string]string
	if err := team.WithCatalogLock(ctx, paths.TeamsDir(), func() error {
		m, e := readMetadata(ctx, paths.TeamsDir())
		if e != nil {
			return e
		}
		if !reflect.DeepEqual(m, before) {
			return fmt.Errorf("catalog changed before fetch; retry")
		}
		localBefore, e = publicationFingerprint(ctx, paths.TeamsDir(), m)
		return e
	}); err != nil {
		return err
	}
	if opts.RefreshOnly && !exists {
		return fmt.Errorf("team is not installed")
	}
	if opts.ClearPin && (opts.Version == "" || opts.SHA256 != "") {
		return fmt.Errorf("--clear-pin requires --version and cannot combine with --sha256")
	}
	if old.Remote != nil {
		if opts.SourceURL != "" && opts.SourceURL != old.Remote.SourceURL && !opts.SwitchSource {
			return fmt.Errorf("installed source cannot be retargeted by add/update")
		}
		if !opts.SwitchSource {
			opts.SourceURL = old.Remote.SourceURL
			opts.SourceAlias = old.Remote.SourceAlias
		}
		if opts.SHA256 != "" && opts.SHA256 != old.Remote.ExpectedSHA256 && opts.Version == "" {
			return fmt.Errorf("changing a digest pin requires explicit --version")
		}
		if opts.SHA256 == "" && !opts.ClearPin {
			opts.SHA256 = old.Remote.ExpectedSHA256
		}
		if opts.Version == "" && old.Remote.Policy == "exact" {
			opts.Version = old.Remote.Version
		}
	}
	if opts.SourceURL == "" {
		return fmt.Errorf("remote source is required")
	}
	if opts.SwitchSource && (!exists || opts.RefreshOnly) {
		return fmt.Errorf("source switching requires an installed team and explicit update")
	}
	p, fetchErr := fetcher.Fetch(ctx, opts.SourceURL, name, opts.Version, opts.SHA256)
	var source *team.Index
	if fetchErr == nil {
		source, fetchErr = validateRemotePackage(ctx, p, name)
		if fetchErr != nil {
			fetchErr = fmt.Errorf("invalid release catalog or dependency closure")
		}
	}
	policy := "stable"
	if opts.Version != "" {
		policy = "exact"
	}
	var provenance *ReleaseProvenance
	var observation *UpstreamObservation
	if fetchErr == nil {
		provenance = &ReleaseProvenance{SourceAlias: opts.SourceAlias, SourceURL: opts.SourceURL, CanonicalURL: p.CanonicalURL, Version: p.Manifest.Version, ManifestDigest: p.ManifestDigest, Policy: policy, ExpectedSHA256: opts.SHA256}
		if old.Remote != nil && opts.Version == "" {
			cmp, e := teamremote.CompareVersions(p.Manifest.Version, old.Remote.Version)
			if e != nil {
				fetchErr = e
			} else if cmp < 0 {
				fetchErr = fmt.Errorf("stable release rollback refused; choose an explicit --version")
			}
		}
		observation = &UpstreamObservation{LastAttempt: opts.Now, LastSuccess: opts.Now, Version: p.Manifest.Version, LatestStable: p.LatestStable, ManifestDigest: p.ManifestDigest, DefinitionHash: definitionHash(source.Teams[name], selectedShared(source, name)), Hashes: map[string]string{}}
		for path, b := range p.Files {
			if path != "index.json" {
				observation.Hashes[path] = team.HashBytes(b)
			}
		}
	}
	if fetchErr == nil {
		fetchErr = rememberTeamDigest(before, name, provenance)
	}
	if fetchErr == nil {
		fetchErr = teamremote.Cache(ctx, paths.TeamsCacheDir(), p)
	}
	err = team.WithCatalogLock(ctx, paths.TeamsDir(), func() error {
		m, err := readMetadata(ctx, paths.TeamsDir())
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(m.Teams[name], old) {
			return fmt.Errorf("team changed during fetch; retry")
		}
		localNow, e := publicationFingerprint(ctx, paths.TeamsDir(), m)
		if e != nil {
			return e
		}
		if !reflect.DeepEqual(localBefore, localNow) {
			return fmt.Errorf("catalog files changed during fetch; retry (force does not bypass concurrent edits)")
		}
		if fetchErr == nil {
			fetchErr = rememberTeamDigest(m, name, provenance)
		}
		if fetchErr != nil {
			if !exists {
				return fetchErr
			}
			record := m.Teams[name]
			o := record.Observation
			if o == nil {
				o = &UpstreamObservation{}
			}
			o.LastAttempt = opts.Now
			o.Error = fetchErr.Error()
			record.Observation = o
			m.Teams[name] = record
			return errors.Join(fetchErr, writeRemoteMetadata(ctx, paths.TeamsDir(), m))
		}
		// Remember every verified observation independently of whether a local
		// conflict later refuses installation. Baseline hashes and installed
		// provenance are unchanged until the transactional publication succeeds.
		if exists {
			record := m.Teams[name]
			record.Observation = observation
			m.Teams[name] = record
		}
		if err := writeRemoteMetadata(ctx, paths.TeamsDir(), m); err != nil {
			return err
		}
		if opts.RefreshOnly {
			return nil
		}
		return installReleaseUnlocked(ctx, paths.TeamsDir(), name, opts.SourceURL, source, p.Files, opts.Force, provenance, observation)
	})
	return err
}

func rememberTeamDigest(m *CatalogMetadata, name string, p *ReleaseProvenance) error {
	if m.DigestHistory == nil {
		m.DigestHistory = map[string]string{}
	}
	key := p.CanonicalURL + "\n" + name + "\n" + p.Version
	if old, ok := m.DigestHistory[key]; ok && old != p.ManifestDigest {
		return fmt.Errorf("immutable team version changed its manifest digest")
	}
	m.DigestHistory[key] = p.ManifestDigest
	return nil
}

func writeRemoteMetadata(ctx context.Context, root string, m *CatalogMetadata) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return team.WriteCatalogUnlocked(ctx, root, nil, append(b, '\n'))
}

func validateRemotePackage(ctx context.Context, p *teamremote.Package, name string) (*team.Index, error) {
	if p == nil || p.Manifest == nil {
		return nil, fmt.Errorf("missing verified package")
	}
	idx, err := team.Decode(p.Files["index.json"])
	if err != nil {
		return nil, err
	}
	if len(idx.Teams) != 1 {
		return nil, fmt.Errorf("release must contain exactly one team")
	}
	if _, ok := idx.Teams[name]; !ok {
		return nil, fmt.Errorf("release catalog names a different team")
	}
	if len(selectedShared(idx, name)) != len(idx.Shared) {
		return nil, fmt.Errorf("release contains unreferenced shared definitions")
	}
	stage, err := os.MkdirTemp("", "litefactory-verified-release-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(stage)
	if err := team.WriteCatalogUnlocked(ctx, stage, p.Files, nil); err != nil {
		return nil, err
	}
	_, closure, err := capturePackage(ctx, stage, name)
	if err != nil {
		return nil, err
	}
	if len(closure) != len(p.Files) {
		return nil, fmt.Errorf("release contains files outside its declared dependency closure")
	}
	for path, b := range closure {
		if original, ok := p.Files[path]; !ok || team.HashBytes(original) != team.HashBytes(b) {
			return nil, fmt.Errorf("release dependency closure differs from manifest")
		}
	}
	return idx, nil
}

func remoteStatus(ctx context.Context, root, name string, record Installation, idx *team.Index, now time.Time) ([]CatalogStatus, error) {
	base := CatalogStatus{Team: name, Origin: record.Remote.SourceURL, Version: record.Remote.Version, Policy: record.Remote.Policy, Freshness: "never-checked"}
	o := record.Observation
	if o != nil {
		base.CandidateVersion = o.Version
		base.LatestStable = o.LatestStable
		base.Error = o.Error
		if !o.LastAttempt.IsZero() {
			base.LastAttempt = o.LastAttempt.UTC().Format(time.RFC3339)
		}
		if !o.LastSuccess.IsZero() {
			base.LastSuccess = o.LastSuccess.UTC().Format(time.RFC3339)
			base.Freshness = "fresh"
			if now.Sub(o.LastSuccess) > 24*time.Hour {
				base.Freshness = "stale"
			}
		}
		if o.Error != "" {
			base.Freshness = "unavailable"
		}
	}
	known := o != nil && !o.LastSuccess.IsZero()
	if !known && base.Error == "" {
		base.Error = "upstream has never been verified; drift is relative to installed baseline only"
	}
	keys := map[string]bool{}
	for p := range record.Hashes {
		keys[p] = true
	}
	if known {
		for p := range o.Hashes {
			keys[p] = true
		}
	}
	var results []CatalogStatus
	definition := base
	definition.Path = "index.json#" + name
	definition.InstalledHash = definitionHash(idx.Teams[name], selectedShared(idx, name))
	next := definitionHash(record.Definition, record.Shared)
	if known {
		next = o.DefinitionHash
		definition.UpstreamHash = next
	}
	definition.State = driftState(definitionHash(record.Definition, record.Shared), definition.InstalledHash, next)
	results = append(results, definition)
	for _, p := range sortedFileKeys(keys) {
		item := base
		item.Path = p
		b, err := readCatalogFile(ctx, root, p)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
		if err == nil {
			item.InstalledHash = team.HashBytes(b)
		}
		next := record.Hashes[p]
		if known {
			next = o.Hashes[p]
			item.UpstreamHash = next
		}
		item.State = driftState(record.Hashes[p], item.InstalledHash, next)
		results = append(results, item)
	}
	return results, nil
}

func publicationFingerprint(ctx context.Context, root string, m *CatalogMetadata) (map[string]string, error) {
	keys := map[string]bool{"index.json": true, metadataPath: true}
	for _, record := range m.Teams {
		for p := range record.Hashes {
			keys[p] = true
		}
	}
	for _, o := range m.Overrides {
		keys[o.Path] = true
		for p := range o.Closure {
			keys[p] = true
		}
	}
	out := map[string]string{}
	for _, p := range sortedFileKeys(keys) {
		b, err := readCatalogFile(ctx, root, p)
		if errors.Is(err, fs.ErrNotExist) {
			out[p] = ""
			continue
		}
		if err != nil {
			return nil, err
		}
		out[p] = team.HashBytes(b)
	}
	return out, nil
}
