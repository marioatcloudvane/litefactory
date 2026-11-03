package bugchecks

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"

	"github.com/cloudvane/litefactory/internal/config"
)

// WithGoBuildCaches applies routing before callers hash the isolation contract.
func (p Isolation) WithGoBuildCaches(caches config.GoBuildCaches) (Isolation, error) {
	if err := caches.Revalidate(""); err != nil {
		return Isolation{}, err
	}
	if p.Environment["GOCACHEPROG"] != "" {
		return Isolation{}, fmt.Errorf("GOCACHEPROG is incompatible with dedicated Go caches")
	}
	p.GoBuildCaches = caches
	p.WritableRoots = slices.Clone(p.WritableRoots)
	if !slices.Contains(p.WritableRoots, caches.Main) {
		p.WritableRoots = append(p.WritableRoots, caches.Main)
	}
	digest := sha256.Sum256([]byte(p.Revision + "\x00managed-go-cache-v1\x00" + caches.Main))
	p.Revision = hex.EncodeToString(digest[:])
	return p, nil
}

func (p Isolation) cacheEnvironment(root string, env []string) ([]string, error) {
	if p.GoBuildCaches.Main == "" {
		return env, nil
	}
	if err := p.GoBuildCaches.Revalidate(root); err != nil {
		return nil, err
	}
	if p.Environment["GOCACHEPROG"] != "" {
		return nil, fmt.Errorf("GOCACHEPROG is incompatible with dedicated Go caches")
	}
	if err := config.ValidateGoCacheEnvironment(env); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(env)+2)
	for _, entry := range env {
		if !strings.HasPrefix(entry, "GOCACHE=") && !strings.HasPrefix(entry, "GOCACHEPROG=") {
			out = append(out, entry)
		}
	}
	return append(out, "GOCACHE="+p.GoBuildCaches.Main, "GOCACHEPROG="), nil
}
