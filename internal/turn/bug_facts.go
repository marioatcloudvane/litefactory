package turn

import (
	"fmt"
	"sort"

	"github.com/cloudvane/litefactory/internal/store"
)

// bugFactStageOrder places every recorded fact in the fixed stage order the
// owner reads a bug review in: what the analysis noticed, then repair, then
// checks, then delivery itself. The overflow marker (see store.BugFacts)
// always sorts last, after every real fact.
var bugFactStageOrder = map[string]int{
	store.BugFactStageAnalysis: 0,
	store.BugFactStageRepair:   1,
	store.BugFactStageChecks:   2,
	store.BugFactStageDelivery: 3,
	store.BugFactStageOverflow: 4,
}

// bugFactLines lays out every recorded fact as one line each, in stage order,
// for the "What the factory noticed" section shared by the analysis and
// delivery reviews (AC-006). Facts within one stage keep their recorded order.
func bugFactLines(facts []store.BugFact) []string {
	ordered := make([]store.BugFact, len(facts))
	copy(ordered, facts)
	sort.SliceStable(ordered, func(i, j int) bool { return bugFactStageOrder[ordered[i].Stage] < bugFactStageOrder[ordered[j].Stage] })
	lines := make([]string, 0, len(ordered))
	for _, f := range ordered {
		lines = append(lines, fmt.Sprintf("[%s] %s", f.Stage, f.Text))
	}
	return lines
}
