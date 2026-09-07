package team

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/cloudvane/litefactory/internal/config"
)

// WriteCatalogUnlocked writes a precomputed central catalog batch. The caller
// holds WithCatalogLock for capture, preflight and publication. Metadata is
// written last so failed publication never claims completed provenance.
func WriteCatalogUnlocked(ctx context.Context, root string, files map[string][]byte, metadata []byte) error {
	keys := make([]string, 0, len(files))
	for p := range files {
		keys = append(keys, p)
	}
	sort.Strings(keys)
	var errs []error
	for _, p := range keys {
		if !catalogSourcePath(p) {
			errs = append(errs, fmt.Errorf("catalog path %q is not owned", p))
			continue
		}
		if err := refuseSymlink(root, p); err != nil {
			errs = append(errs, err)
		}
		if _, ok := config.ClassifySecret(string(files[p])); ok {
			errs = append(errs, fmt.Errorf("catalog %s contains a credential", p))
		}
		if config.ContainsForbiddenFlag(string(files[p])) {
			errs = append(errs, fmt.Errorf("catalog %s contains a forbidden permission flag", p))
		}
		if st, err := os.Lstat(filepath.Join(root, p)); err == nil && !st.Mode().IsRegular() {
			errs = append(errs, fmt.Errorf("catalog target %s is not regular", p))
		} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	if metadata != nil {
		if err := refuseSymlink(root, ".litefactory/catalog.json"); err != nil {
			errs = append(errs, err)
		}
		if st, err := os.Lstat(filepath.Join(root, ".litefactory/catalog.json")); err == nil && !st.Mode().IsRegular() {
			errs = append(errs, fmt.Errorf("catalog metadata target must be regular"))
		} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
			errs = append(errs, err)
		}
		if len(metadata) > 1<<20 {
			errs = append(errs, fmt.Errorf("catalog metadata exceeds 1 MiB"))
		}
		if _, ok := config.ClassifySecret(string(metadata)); ok {
			errs = append(errs, fmt.Errorf("catalog metadata contains a credential"))
		}
		if config.ContainsForbiddenFlag(string(metadata)) {
			errs = append(errs, fmt.Errorf("catalog metadata contains a forbidden permission flag"))
		}
	}
	if err := errors.Join(errs...); err != nil {
		return err
	}
	for _, p := range keys {
		if err := ctx.Err(); err != nil {
			return err
		}
		if p == "index.json" {
			continue
		}
		if err := writeFileAtomicChecked(root, p, files[p], 0o600); err != nil {
			return err
		}
	}
	if data, ok := files["index.json"]; ok {
		if err := writeFileAtomicChecked(root, "index.json", data, 0o600); err != nil {
			return err
		}
	}
	if metadata != nil {
		return writeFileAtomicChecked(root, ".litefactory/catalog.json", metadata, 0o600)
	}
	return nil
}

func catalogSourcePath(p string) bool {
	return fs.ValidPath(p) && !strings.Contains(p, "\\") && !strings.HasPrefix(p, ".") && (p == "index.json" || strings.HasPrefix(p, "agents/") || strings.HasPrefix(p, "skills/"))
}
