package teamsource

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/cloudvane/litefactory/internal/team"
)

type overrideSource struct {
	origin   string
	filename string
	files    map[string][]byte
}

func captureOverride(ctx context.Context, source string, a team.AgentEntry) (*overrideSource, error) {
	absolute, err := filepath.Abs(source)
	if err != nil {
		return nil, err
	}
	originRoot := filepath.Dir(absolute)
	r, err := os.OpenRoot(originRoot)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	reader := captureReader{ctx: ctx, root: r, base: originRoot, files: map[string][]byte{}, hashes: map[string]string{}, skills: map[string]int{}}
	filename := filepath.Base(absolute)
	raw, err := reader.read(filename)
	if err != nil {
		return nil, err
	}
	src, err := parseAgentSource(raw)
	if err != nil {
		return nil, err
	}
	if err := reader.agentDependencies(filename, src); err != nil {
		return nil, err
	}
	for _, skill := range a.Skills {
		if err := reader.skill(skill); err != nil {
			return nil, err
		}
	}
	for _, p := range sortedFileKeys(reader.files) {
		fresh, e := reader.readFresh(p)
		if e != nil {
			return nil, e
		}
		if !bytes.Equal(fresh, reader.files[p]) {
			return nil, fmt.Errorf("override source changed during capture: %s", p)
		}
	}
	return &overrideSource{origin: absolute, filename: filename, files: reader.files}, nil
}

func (s *overrideSource) publication(scope string) (string, map[string][]byte, map[string]OverrideFile) {
	rootBytes := s.files[s.filename]
	prefix := "agents/overrides/" + team.HashBytes([]byte(scope + string(rootBytes)))[:24]
	files := map[string][]byte{}
	closure := map[string]OverrideFile{}
	for _, p := range sortedFileKeys(s.files) {
		b := s.files[p]
		dest := path.Join(prefix, p)
		files[dest] = b
		closure[dest] = OverrideFile{Source: p, Hash: team.HashBytes(b)}
		if strings.HasPrefix(p, "skills/") {
			files[p] = b
			closure[p] = OverrideFile{Source: p, Hash: team.HashBytes(b)}
		}
	}
	return path.Join(prefix, s.filename), files, closure
}

func overrideStatus(ctx context.Context, root, scope string, o Override, a team.AgentEntry) ([]CatalogStatus, *overrideSource, error) {
	next, originErr := captureOverride(ctx, o.Origin, a)
	records := make([]CatalogStatus, 0, len(o.Closure))
	seen := map[string]bool{}
	for _, p := range sortedFileKeys(o.Closure) {
		base := o.Closure[p]
		seen[base.Source] = true
		item := CatalogStatus{Team: scope, Path: p, Origin: o.Origin}
		current, err := readCatalogFile(ctx, root, p)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, nil, err
		}
		if err == nil {
			item.InstalledHash = team.HashBytes(current)
		}
		if originErr == nil {
			if b, ok := next.files[base.Source]; ok {
				item.UpstreamHash = team.HashBytes(b)
			}
			item.State = driftState(base.Hash, item.InstalledHash, item.UpstreamHash)
		} else {
			item.Error = fmt.Sprintf("override origin closure unavailable: %v; installed bytes remain usable", originErr)
			item.State = driftState(base.Hash, item.InstalledHash, base.Hash)
		}
		records = append(records, item)
	}
	if originErr == nil {
		_, files, closure := next.publication(scope)
		for _, p := range sortedFileKeys(closure) {
			base := closure[p]
			if seen[base.Source] {
				continue
			}
			item := CatalogStatus{Team: scope, Path: p, Origin: o.Origin, UpstreamHash: team.HashBytes(files[p])}
			current, e := readCatalogFile(ctx, root, p)
			if e != nil && !errors.Is(e, fs.ErrNotExist) {
				return nil, nil, e
			}
			if e == nil {
				item.InstalledHash = team.HashBytes(current)
			}
			item.State = driftState("", item.InstalledHash, item.UpstreamHash)
			records = append(records, item)
		}
	}
	if len(records) == 0 {
		return nil, nil, fmt.Errorf("override %s has no recorded closure; set its source again", scope)
	}
	return records, next, nil
}

func refreshOverrideUnlocked(ctx context.Context, root, scope string, o Override, a team.AgentEntry, force bool) error {
	records, next, err := overrideStatus(ctx, root, scope, o, a)
	if err != nil {
		return err
	}
	changed := false
	var faults []error
	for _, record := range records {
		if record.Error != "" {
			faults = append(faults, fmt.Errorf("%s: %s", record.Path, record.Error))
			continue
		}
		if record.State != "in-sync" {
			changed = true
		}
		if !force && (record.State == "locally-modified" || record.State == "conflict") {
			faults = append(faults, fmt.Errorf("override %s dependency %s is %s; inspect it or use --force", scope, record.Path, record.State))
		}
	}
	if err := errors.Join(faults...); err != nil {
		return err
	}
	if !changed {
		return nil
	}
	_, err = manageCapturedUnlocked(ctx, root, scope, "set", o.Origin, next, force)
	return err
}
