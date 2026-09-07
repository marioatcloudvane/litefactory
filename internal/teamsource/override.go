package teamsource

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"github.com/cloudvane/litefactory/internal/team"
)

func scopedAgent(idx *team.Index, scope string) (team.AgentEntry, error) {
	parts := strings.Split(scope, ":")
	if len(parts) == 2 && parts[0] == "shared" {
		if a, ok := idx.Shared[parts[1]]; ok {
			return a, nil
		}
	}
	if len(parts) == 3 && parts[0] == "team" {
		if t, ok := idx.Teams[parts[1]]; ok {
			for _, mode := range team.Modes() {
				list, _ := t.Mode(mode)
				for _, a := range list {
					if a.Ref == "" && a.ID == parts[2] {
						return a, nil
					}
				}
			}
		}
	}
	return team.AgentEntry{}, fmt.Errorf("unknown agent scope %q; use shared:<id> or team:<team>:<inline-agent>", scope)
}
func scopeAffectsTeam(idx *team.Index, name, scope string) bool {
	parts := strings.Split(scope, ":")
	if len(parts) == 3 {
		return parts[0] == "team" && parts[1] == name
	}
	if len(parts) == 2 && parts[0] == "shared" {
		_, ok := selectedShared(idx, name)[parts[1]]
		return ok
	}
	return false
}
func replaceScopedAgent(idx *team.Index, scope string, a team.AgentEntry) {
	parts := strings.Split(scope, ":")
	if parts[0] == "shared" {
		idx.Shared[parts[1]] = a
		return
	}
	t := idx.Teams[parts[1]]
	for _, list := range [][]team.AgentEntry{t.Research, t.Plan, t.Implement, t.Validate, t.Spike} {
		for i := range list {
			if list[i].Ref == "" && list[i].ID == parts[2] {
				list[i] = a
			}
		}
	}
	idx.Teams[parts[1]] = t
}

// Manage copies overrides into the owned catalog. Shared overrides change only
// explicit references; an inline same-named agent remains independent.
func Manage(ctx context.Context, root, scope, action, source string) (result Override, err error) {
	err = team.WithCatalogLock(ctx, root, func() error {
		var e error
		result, e = manageUnlocked(ctx, root, scope, action, source)
		return e
	})
	return result, err
}

func manageUnlocked(ctx context.Context, root, scope, action, source string) (Override, error) {
	return manageCapturedUnlocked(ctx, root, scope, action, source, nil, false)
}

func manageCapturedUnlocked(ctx context.Context, root, scope, action, source string, captured *overrideSource, force bool) (result Override, err error) {
	err = func() error {
		idx, e := readIndex(ctx, root)
		if e != nil {
			return e
		}
		a, e := scopedAgent(idx, scope)
		if e != nil {
			return e
		}
		m, e := readMetadata(ctx, root)
		if e != nil {
			return e
		}
		current, active := m.Overrides[scope]
		if action == "show" {
			if active {
				result = current
			} else {
				result = Override{Path: a.Path}
			}
			return nil
		}
		files := map[string][]byte{}
		switch action {
		case "unset":
			if !active {
				result = Override{Path: a.Path}
				return nil
			}
			replaceScopedAgent(idx, scope, current.Original)
			delete(m.Overrides, scope)
			result = Override{Path: current.Original.Path}
		case "set":
			if captured == nil {
				captured, e = captureOverride(ctx, source, a)
				if e != nil {
					return e
				}
			}
			newPath, newFiles, closure := captured.publication(scope)
			for _, p := range sortedFileKeys(newFiles) {
				b := newFiles[p]
				if strings.HasPrefix(p, "skills/") {
					existing, e := readCatalogFile(ctx, root, p)
					if e != nil && !errors.Is(e, fs.ErrNotExist) {
						return e
					}
					if e == nil && !bytes.Equal(existing, b) && (!force || !restoreSharedSkill(m, current, p, team.HashBytes(b))) {
						return fmt.Errorf("override skill %s conflicts with installed shared skill", p)
					}
				}
				files[p] = b
			}
			original := a
			if active {
				original = current.Original
			}
			a.Path = newPath
			replaceScopedAgent(idx, scope, a)
			result = Override{Origin: captured.origin, Path: a.Path, Hash: team.HashBytes(captured.files[captured.filename]), Original: original, Closure: closure}
			m.Overrides[scope] = result
		default:
			return fmt.Errorf("unknown manage action %q; expected set, unset or show", action)
		}
		b, e := json.MarshalIndent(idx, "", "  ")
		if e != nil {
			return e
		}
		files["index.json"] = append(b, '\n')
		if e := validatePublication(ctx, root, idx, m, files); e != nil {
			return e
		}
		metadata, e := json.MarshalIndent(m, "", "  ")
		if e != nil {
			return e
		}
		return team.WriteCatalogUnlocked(ctx, root, files, append(metadata, '\n'))
	}()
	return result, err
}

// A force refresh may restore corrupt shared bytes to their recorded value,
// but may not introduce a competing shared definition through an override.
func restoreSharedSkill(m *CatalogMetadata, current Override, p, want string) bool {
	if current.Closure[p].Hash != want {
		return false
	}
	for _, record := range m.Teams {
		if h, ok := record.Hashes[p]; ok && h != want {
			return false
		}
	}
	for _, record := range m.Overrides {
		if f, ok := record.Closure[p]; ok && f.Hash != want {
			return false
		}
	}
	return true
}
