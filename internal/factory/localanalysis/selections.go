package localanalysis

import (
	"context"
	"errors"
	"strings"

	"github.com/cloudvane/litefactory/internal/store"
)

// EvidenceRepositories resolves the explicit local repository coverage before any log read.
func EvidenceRepositories(ctx context.Context, db *store.DB, selections []string) ([]int64, error) {
	var ids []int64
	for _, selection := range selections {
		forge, slug, ok := strings.Cut(selection, ":")
		owner, name, valid := strings.Cut(slug, "/")
		if !ok || !valid || owner == "" || name == "" || strings.Contains(name, "/") {
			return nil, errors.New("mcp logs repository must be forge:owner/name")
		}
		repository, err := db.GetRepoBySlug(ctx, forge, owner, name)
		if err != nil {
			return nil, errors.New("mcp logs repository is not registered")
		}
		ids = append(ids, repository.ID)
	}
	return ids, nil
}
