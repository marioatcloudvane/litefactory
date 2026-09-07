package team

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// overridesName is the per-repo agent source override file's fixed name
// within litefactoryDir.
//
// Its shape is a flat JSON object: {agentKey: localPath}. localPath is a
// repo-relative, forward-slash path to a markdown file inside root that
// supersedes AgentEntry.Source for that agent key alone at render time
// (internal/teamsource's RenderTeam/RenderCurrent) — Dest, the lockfile
// schema, and internal/team's own three-tier index source resolution are
// all untouched by an override; substitution happens only at the point
// render.go reads an agent's raw source bytes.
const overridesName = "team.overrides.json"

// overridesPath returns the repo-relative path to team.overrides.json under
// root.
func overridesPath(root string) string {
	return filepath.Join(root, litefactoryDir, overridesName)
}

// decodeOverrides parses team.overrides.json's flat {agentKey: localPath}
// shape, in the same two passes team index decoding uses (decode.go): a
// generic walk that checks every key against KeyPattern and every value
// decodes as a non-empty JSON string, with every fault aggregated via
// joinSorted rather than stopping at the first one, and only once that pass
// is clean, a typed decode into map[string]string. A plain json.Unmarshal
// straight into map[string]string would report only the first decode
// failure and would not itself catch a malformed key or a non-string value
// with a usable, per-entry error.
func decodeOverrides(data []byte) (map[string]string, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse team overrides: %w", err)
	}

	var errs []error
	for key, val := range raw {
		if !KeyPattern.MatchString(key) {
			errs = append(errs, &KeyError{Path: "team.overrides.json", Key: key})
			continue
		}
		var s string
		if err := json.Unmarshal(val, &s); err != nil {
			errs = append(errs, fmt.Errorf("team.overrides.json.%s: value must be a JSON string: %w", key, err))
			continue
		}
		if s == "" {
			errs = append(errs, &EmptyFieldError{Path: "team.overrides.json." + key, Field: "source"})
		}
	}
	if err := joinSorted(errs); err != nil {
		return nil, err
	}

	overrides := make(map[string]string, len(raw))
	for key, val := range raw {
		var s string
		// Errors are impossible here: pass one above already confirmed every
		// value in raw decodes cleanly as a non-empty string.
		_ = json.Unmarshal(val, &s)
		overrides[key] = s
	}
	return overrides, nil
}

// loadOverrides reads and decodes root's overrides file. A missing file is
// the expected common case — most agents in most repos have no override —
// and yields an empty, non-nil map with a nil error, unlike loadLockfile's
// ErrNoLockfile: a lockfile's absence is meaningful state (nothing installed
// yet) that callers branch on, while an overrides file's absence carries no
// information beyond "nothing is overridden here".
func loadOverrides(root string) (map[string]string, error) {
	data, err := os.ReadFile(overridesPath(root))
	if os.IsNotExist(err) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read team overrides: %w", err)
	}
	overrides, err := decodeOverrides(data)
	if err != nil {
		return nil, err
	}
	if overrides == nil {
		overrides = map[string]string{}
	}
	return overrides, nil
}

// saveOverrides writes overrides to root's overrides file atomically.
// encoding/json sorts a map's keys when marshaling, so two writers
// converging on the same set of entries produce byte-identical JSON, the
// same guarantee Lockfile.save relies on for team.lock.json.
func saveOverrides(root string, overrides map[string]string) error {
	data, err := json.MarshalIndent(overrides, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal team overrides: %w", err)
	}
	data = append(data, '\n')
	path := overridesPath(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o777); err != nil {
		return fmt.Errorf("create %s: %w", litefactoryDir, err)
	}
	if err := writeFileAtomic(path, data, 0o666); err != nil {
		return fmt.Errorf("write team overrides: %w", err)
	}
	return nil
}

// validateOverrideSource checks that localPath, as given to SetOverride for
// agentKey, is usable as an override source: a repo-relative, forward-slash
// path with no ".." segment, naming an existing regular markdown (.md) file
// inside root. It returns the cleaned repo-relative path to record on
// success, or an *OverridePathError naming why it was refused — SetOverride
// must record no override at all when this fails (AC-4).
func validateOverrideSource(root, agentKey, localPath string) (string, error) {
	if localPath == "" {
		return "", &OverridePathError{AgentKey: agentKey, Path: localPath, Reason: "path must not be empty"}
	}
	if filepath.IsAbs(localPath) || strings.Contains(localPath, "\\") {
		return "", &OverridePathError{AgentKey: agentKey, Path: localPath, Reason: "must be a repo-relative, forward-slash path"}
	}
	clean := filepath.ToSlash(filepath.Clean(localPath))
	if clean == ".." || strings.HasPrefix(clean, "../") || strings.Contains(clean, "/../") || filepath.IsAbs(clean) {
		return "", &OverridePathError{AgentKey: agentKey, Path: localPath, Reason: "must resolve inside the target repo (no .. segments)"}
	}
	if !strings.HasSuffix(clean, ".md") {
		return "", &OverridePathError{AgentKey: agentKey, Path: localPath, Reason: "must be a markdown (.md) file"}
	}

	if err := refuseSymlink(root, clean); err != nil {
		return "", err
	}
	info, err := os.Lstat(filepath.Join(root, clean))
	if err != nil {
		if os.IsNotExist(err) {
			return "", &OverridePathError{AgentKey: agentKey, Path: localPath, Reason: "file does not exist"}
		}
		return "", fmt.Errorf("lstat %s: %w", clean, err)
	}
	if !info.Mode().IsRegular() {
		return "", &OverridePathError{AgentKey: agentKey, Path: localPath, Reason: "not a regular file"}
	}
	return clean, nil
}

// ActiveOverride reports whether agentKey has an active override recorded in
// root, and if so, the repo-relative path of the local markdown file that
// supersedes its index-resolved source at render time.
func ActiveOverride(root, agentKey string) (path string, ok bool, err error) {
	overrides, err := loadOverrides(root)
	if err != nil {
		return "", false, err
	}
	path, ok = overrides[agentKey]
	return path, ok, nil
}

// SetOverride records agentKey's override in root as localPath, replacing
// any override already recorded for that key. localPath is validated first
// (validateOverrideSource): it must be a repo-relative, forward-slash path
// with no ".." segment, naming an existing regular markdown (.md) file
// inside root. No override is recorded when validation fails (AC-4).
//
// Concurrent SetOverride/UnsetOverride/Add calls against the same root
// serialize on the same per-repo flock Add uses (lock.go), so a set racing a
// concurrent add's read of the overrides file cannot produce a torn read.
func SetOverride(root, agentKey, localPath string) error {
	if !KeyPattern.MatchString(agentKey) {
		return &KeyError{Path: "agent-key", Key: agentKey}
	}
	clean, err := validateOverrideSource(root, agentKey, localPath)
	if err != nil {
		return err
	}

	lock, err := acquireRepoLock(root)
	if err != nil {
		return fmt.Errorf("acquire repo lock: %w", err)
	}
	defer func() {
		_ = lock.release()
	}()

	overrides, err := loadOverrides(root)
	if err != nil {
		return err
	}
	overrides[agentKey] = clean
	return saveOverrides(root, overrides)
}

// UnsetOverride removes agentKey's override from root, if one is set. It is
// idempotent: unsetting a key with no active override succeeds without
// error, so a caller does not need to check ActiveOverride first just to
// avoid one — matching this package's general policy of not treating an
// already-satisfied goal as a failure.
func UnsetOverride(root, agentKey string) error {
	if !KeyPattern.MatchString(agentKey) {
		return &KeyError{Path: "agent-key", Key: agentKey}
	}

	lock, err := acquireRepoLock(root)
	if err != nil {
		return fmt.Errorf("acquire repo lock: %w", err)
	}
	defer func() {
		_ = lock.release()
	}()

	overrides, err := loadOverrides(root)
	if err != nil {
		return err
	}
	if _, ok := overrides[agentKey]; !ok {
		return nil
	}
	delete(overrides, agentKey)
	return saveOverrides(root, overrides)
}

// ReadOverrideSource reads the local markdown file recorded as agentKey's
// active override in root, if any. ok is false, with raw and path zero and
// err nil, when agentKey has no active override in root — the common case,
// not an error condition, so a caller (internal/teamsource) can try an
// override unconditionally before falling back to the index's own source
// without special-casing "no overrides file at all".
//
// Reading refuses to follow a symlink anywhere along path (refuseSymlink),
// the same guarantee every other read or write this package does gives.
func ReadOverrideSource(root, agentKey string) (raw []byte, path string, ok bool, err error) {
	path, ok, err = ActiveOverride(root, agentKey)
	if err != nil || !ok {
		return nil, "", ok, err
	}
	if err := refuseSymlink(root, path); err != nil {
		return nil, "", false, err
	}
	raw, err = os.ReadFile(filepath.Join(root, path))
	if err != nil {
		return nil, "", false, fmt.Errorf("read override %s for agent %q: %w", path, agentKey, err)
	}
	return raw, path, true, nil
}
