package teamsource

import (
	"context"
	"fmt"
	"io/fs"
	"os"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/team"
)

// Source identifies the provenance of a resolved catalog.
type Source int

const (
	// SourceCentral is the centrally installed operator-managed catalog.
	SourceCentral Source = iota
)

func (s Source) String() string {
	if s == SourceCentral {
		return "central"
	}
	return fmt.Sprintf("teamsource.Source(%d)", s)
}

// Resolved holds one validated catalog and the filesystem containing its sources.
type Resolved struct {
	Source Source
	Index  *team.Index
	FS     fs.FS
}

// Resolve reads the centrally installed catalog only. Distribution defaults are
// installed explicitly by management; a missing catalog is an actionable error.
func Resolve(ctx context.Context, paths config.Paths) (resolved *Resolved, err error) {
	err = team.WithCatalogLock(ctx, paths.TeamsDir(), func() error {
		r, e := os.OpenRoot(paths.TeamsDir())
		if e != nil {
			return e
		}
		defer r.Close()
		c := captureReader{ctx: ctx, root: r, base: paths.TeamsDir()}
		data, e := c.readFresh("index.json")
		if e != nil {
			return e
		}
		idx, e := team.Decode(data)
		if e != nil {
			return e
		}
		resolved = &Resolved{Source: SourceCentral, Index: idx, FS: os.DirFS(paths.TeamsDir())}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("resolve central teams (install with team add): %w", err)
	}
	return resolved, nil
}
