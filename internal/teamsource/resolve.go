package teamsource

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/cloudvane/litefactory/internal/repos"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/team"
)

// Source identifies which of US-2's three tiers a [Resolved] value came
// from. It exists for observability only (e.g. logging which tier is in
// effect) — it never causes any field-level mixing between tiers.
type Source int

const (
	// SourceEmbedded is tier 1: the shipped default team index.
	SourceEmbedded Source = iota
	// SourceStaticPath is tier 2: config.TeamConfig's static-path override.
	SourceStaticPath
	// SourceRegisteredRepo is tier 3: an already-registered repo's clone.
	SourceRegisteredRepo
)

func (s Source) String() string {
	switch s {
	case SourceEmbedded:
		return "embedded"
	case SourceStaticPath:
		return "static-path"
	case SourceRegisteredRepo:
		return "registered-repo"
	default:
		return fmt.Sprintf("teamsource.Source(%d)", int(s))
	}
}

// Resolved is the outcome of resolving exactly one team index source: which
// tier it came from, the decoded and validated index, and the filesystem an
// agent entry's Source path should be read from.
type Resolved struct {
	// Source names which tier produced this result.
	Source Source
	// Index is the decoded, validated team index for the chosen tier.
	Index *team.Index
	// FS is rooted so that fs.ReadFile(FS, entry.Source) reads the markdown
	// file an AgentEntry.Source names, for any entry in Index, regardless of
	// which tier produced it.
	FS fs.FS
}

// teamsIndexPath returns the <root>/teams/index.json convention shared by
// every tier that reads from a filesystem root (tier 2's configured
// override and tier 3's registered-repo clone path).
func teamsIndexPath(root string) string {
	return filepath.Join(root, "teams", "index.json")
}

// teamsDir returns the <root>/teams directory a tier's markdown tree is
// rooted at, matching teamsIndexPath's sibling convention.
func teamsDir(root string) string {
	return filepath.Join(root, "teams")
}

// loadFromRoot reads and migrates the teams/index.json document under root,
// returning the decoded index and its markdown filesystem. It is the shared
// implementation behind the static-path (tier 2) and registered-repo (tier
// 3) loaders; both read a directory root the same way, differing only in
// where that root comes from and how a missing index is reported.
func loadFromRoot(root string) (*team.Index, fs.FS, error) {
	indexPath := teamsIndexPath(root)
	data, err := os.ReadFile(indexPath)
	if err != nil {
		return nil, nil, err
	}
	idx, err := team.Migrate(data)
	if err != nil {
		return nil, nil, fmt.Errorf("load team index at %s: %w", indexPath, err)
	}
	return idx, os.DirFS(teamsDir(root)), nil
}

// ResolveEmbedded loads tier 1, the shipped default team index. It is
// exposed as its own function so [Resolve] falls back to it explicitly once
// tiers 2 and 3 have both been ruled out, and so a caller that already knows
// no override applies can load it directly without going through the full
// precedence chain.
func ResolveEmbedded() (*Resolved, error) {
	idx, err := team.Embedded()
	if err != nil {
		return nil, fmt.Errorf("resolve embedded team index: %w", err)
	}
	fsys, err := team.EmbeddedFS()
	if err != nil {
		return nil, fmt.Errorf("resolve embedded team index: %w", err)
	}
	return &Resolved{Source: SourceEmbedded, Index: idx, FS: fsys}, nil
}

// ResolveStaticPath resolves tier 2: the static-path override configured via
// config.TeamConfig (T-10). An empty override means tier 2 does not apply —
// (nil, nil) is returned so the caller falls through to tier 1 — but once an
// override is configured, a missing teams/index.json there is a hard error
// (*StaticPathMissingError, US-2 AC-3): it never falls back silently to the
// embedded default.
func ResolveStaticPath(override string) (*Resolved, error) {
	if override == "" {
		return nil, nil
	}
	idx, fsys, err := loadFromRoot(override)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, &StaticPathMissingError{Path: override, IndexPath: teamsIndexPath(override)}
		}
		return nil, fmt.Errorf("resolve static-path team index override %q: %w", override, err)
	}
	return &Resolved{Source: SourceStaticPath, Index: idx, FS: fsys}, nil
}

// ResolveRegisteredRepo resolves tier 3: an already-registered repo named by
// the operator, ready for US-9's write engine to call once it lands. It
// reuses internal/repos.ResolveIdentifier (US-19's fuzzy resolution) to find
// the repo's clone path, then reads that clone's teams/index.json and
// markdown tree exactly as ResolveStaticPath does for a configured path,
// rooted at the clone instead.
//
// An empty identifier means tier 3 does not apply — (nil, nil) is returned
// so the caller falls through to tier 2 — db is not consulted in that case.
// A non-empty identifier that fails to resolve to a registered repo, or that
// resolves but has no teams/index.json at its clone root, is a hard error;
// there is no fallback to a lower-precedence tier once tier 3 has been named
// explicitly.
func ResolveRegisteredRepo(ctx context.Context, db *store.DB, identifier string) (*Resolved, error) {
	if identifier == "" {
		return nil, nil
	}
	repo, err := repos.ResolveIdentifier(ctx, db, identifier)
	if err != nil {
		return nil, fmt.Errorf("resolve registered repo %q for team index: %w", identifier, err)
	}
	idx, fsys, err := loadFromRoot(repo.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, &RegisteredRepoIndexMissingError{
				Identifier: identifier,
				Path:       repo.Path,
				IndexPath:  teamsIndexPath(repo.Path),
			}
		}
		return nil, fmt.Errorf("resolve registered repo %q team index: %w", identifier, err)
	}
	return &Resolved{Source: SourceRegisteredRepo, Index: idx, FS: fsys}, nil
}

// Resolve picks exactly one team index source, in precedence order embedded
// < static-path override < registered-repo override (US-2 T-08): the
// highest-precedence tier that applies wins in full, and no lower tier is
// even read once one applies.
//
// registeredRepoIdentifier is the operator-named repo for tier 3, as a
// future command (US-9) will supply it; an empty string means tier 3 does
// not apply and db is never consulted. staticPathOverride is
// config.TeamConfig (or equivalent); an empty string means tier 2 does not
// apply.
//
// Tier 3 is checked first, and only when registeredRepoIdentifier is empty
// is tier 2 checked at all: US-2 AC-2 requires that when both a static-path
// override and a registered-repo override are available, "the static-path
// override is not consulted" — not merely overridden — so ResolveStaticPath
// is never called once a registered-repo override applies.
func Resolve(ctx context.Context, db *store.DB, staticPathOverride, registeredRepoIdentifier string) (*Resolved, error) {
	resolved, err := ResolveRegisteredRepo(ctx, db, registeredRepoIdentifier)
	if err != nil {
		return nil, err
	}
	if resolved != nil {
		return resolved, nil
	}

	resolved, err = ResolveStaticPath(staticPathOverride)
	if err != nil {
		return nil, err
	}
	if resolved != nil {
		return resolved, nil
	}

	return ResolveEmbedded()
}
