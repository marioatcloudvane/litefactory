package standards

import (
	"context"
	"github.com/cloudvane/litefactory/internal/store"
	"strings"
)

// Prepare validates an unapproved first version without writing it. Callers can
// commit a batch and its provenance together in the store's transaction.
func (s *Service) Prepare(ctx context.Context, id string, base int64, ops []Operation) (store.StandardsProposalVersion, error) {
	if strings.TrimSpace(id) == "" || base < 1 {
		return store.StandardsProposalVersion{}, ErrInvalid
	}
	before, err := s.base(ctx, base)
	if err != nil {
		return store.StandardsProposalVersion{}, err
	}
	after, err := apply(before, ops)
	if err != nil {
		return store.StandardsProposalVersion{}, err
	}
	return store.StandardsProposalVersion{ProposalID: id, Version: 1, BaseRevision: base, SchemaVersion: 1, Operations: canonicalOps(ops), ChangeDigest: changeDigest(base, ops), ResultDigest: snapshotDigest(after), Author: "prevention-analysis"}, nil
}
