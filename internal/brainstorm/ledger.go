package brainstorm

import (
	"fmt"
	"strings"

	"github.com/cloudvane/litefactory/internal/agentresult"
)

// ledgerShapeDiagnostics checks the shape of a brainstorm ledger, which reuses
// the Research payload: every finding, decision and assumption has a valid,
// collection-unique ID and nonblank text, and every evidence reference is a
// clean relative path. The brainstorm keeps and renders these entries, so it
// owns this check; Research itself takes free notes.
func ledgerShapeDiagnostics(at string, r agentresult.Research) []agentresult.Diagnostic {
	var out []agentresult.Diagnostic
	entry := func(path, id, text string, seen map[string]bool) {
		if !agentresult.ValidID(id) {
			out = append(out, diag(path+".id", "identity", "stable identifier", "invalid identifier", "Use a letter followed by letters, digits, underscores or hyphens, up to 128 bytes."))
		}
		if seen[id] {
			out = append(out, diag(path+".id", "duplicate", "unused identifier", "duplicate identifier", "Use collection-local unique identities."))
		}
		seen[id] = true
		if strings.TrimSpace(text) == "" {
			out = append(out, diag(path+".text", "blank", "nonblank string", "blank", "Supply meaningful content."))
		}
	}
	seen := map[string]bool{}
	for i, f := range r.Findings {
		fp := fmt.Sprintf("%s.findings[%d]", at, i)
		entry(fp, f.ID, f.Text, seen)
		for j, e := range f.Evidence {
			if !agentresult.ValidPath(e.Path) || e.Line < 0 {
				out = append(out, diag(fmt.Sprintf("%s.evidence[%d]", fp, j), "reference", "relative path and nonnegative line", "invalid reference", "Supply a rooted source reference."))
			}
		}
	}
	for name, items := range map[string][]agentresult.Statement{"decisions": r.Decisions, "assumptions": r.Assumptions} {
		ids := map[string]bool{}
		for i, s := range items {
			entry(fmt.Sprintf("%s.%s[%d]", at, name, i), s.ID, s.Text, ids)
		}
	}
	return out
}
