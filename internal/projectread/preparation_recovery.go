package projectread

import (
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/retainedevidence"
)

// This derived cache belongs to the trusted reader service lifetime. It holds
// only explicitly budget-paused private source preparation, not agent grants,
// invocation cursors or budgets. Expiry/eviction releases its spill and child.
var pausedRepositoryPreparations = retainedevidence.NewPreparationCache[*repoView](8, 10*time.Minute, func(entry *repoView) { entry.close() })

// Pause ends this invocation's view ownership after its budget is exhausted.
// Unfinished source work can be moved once to a later independently authorized
// invocation using its locator; ordinary Close and cancellation discard it.
func (v *Views) Pause() error {
	if v == nil {
		return nil
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	v.closed = true
	for token, entry := range v.entries {
		if !entry.ready && entry.sourceBytes > 0 && entry.stream != nil && entry.file != nil {
			pausedRepositoryPreparations.Put(entry.preparationID, entry)
		} else {
			entry.close()
		}
		delete(v.entries, token)
	}
	return nil
}

// The caller has already rechecked its grant and validated all selectors. A
// locator is only a position: the private state must independently match it.
func takeRepositoryPreparation(locator *repoLocator, project int64, commit, path, policy string) (*repoView, error) {
	entry, ok := pausedRepositoryPreparations.Take(locator.PreparationID)
	if !ok {
		return nil, errors.New("private preparation unavailable")
	}
	if entry.project != project || entry.commit != commit || entry.path != path || entry.policy != policy || entry.sourceBytes != locator.Prepared || hex.EncodeToString(entry.hash.Sum(nil)) != locator.Prefix {
		entry.close()
		return nil, errors.New("private preparation identity mismatch")
	}
	return entry, nil
}

func decoratePage(content string, firstLine int) string {
	var out strings.Builder
	for _, line := range strings.Split(strings.TrimSuffix(content, "\n"), "\n") {
		_, _ = fmt.Fprintf(&out, "%d\t%s\n", firstLine, line)
		firstLine++
	}
	return out.String()
}
