package standards

import (
	"fmt"
	"strings"

	"github.com/cloudvane/litefactory/internal/store"
)

// PrepareProposal validates against an already verified retained snapshot without
// creating effects. The caller can persist the returned version in its own transaction.
// Base 0 is the empty book before the first publication.
func PrepareProposal(id string, base int64, before Snapshot, ops []Operation, author string) (store.StandardsProposalVersion, error) {
	if strings.TrimSpace(id) == "" || base < 0 || base == 0 && (len(before.Categories) > 0 || len(before.Standards) > 0) || strings.TrimSpace(author) == "" {
		return store.StandardsProposalVersion{}, fmt.Errorf("%w: proposal identity and base required", ErrInvalid)
	}
	if err := validateSnapshot(before); err != nil {
		return store.StandardsProposalVersion{}, err
	}
	after, err := apply(before, ops)
	if err != nil {
		return store.StandardsProposalVersion{}, err
	}
	return store.StandardsProposalVersion{ProposalID: id, Version: 1, BaseRevision: base, SchemaVersion: 1, Operations: canonicalOps(ops), ChangeDigest: changeDigest(base, ops), ResultDigest: snapshotDigest(after), Author: author}, nil
}
