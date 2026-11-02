package planbrief

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/cloudvane/litefactory/internal/linear/linearmd"
)

// PlanSetPromotionPrefix is the exact owner command text that promotes a proposed
// story set. The full command is the prefix followed by the set revision, and the
// published comment tells the owner to reply with exactly that text. Parsing and
// authority of the command belong to promotion, never to the proposing effect.
const PlanSetPromotionPrefix = "/approve-plan set "

// ErrPlanStorySetPromoted refuses a proposal once the owner promoted a set:
// stories then exist and splitting the scope again is not offered.
var ErrPlanStorySetPromoted = errors.New("the story set is already promoted and cannot be replaced")

// ErrPlanStorySetHasStories refuses a proposal for a scope that already has
// stories, which is the case for every session that did not start at a set.
var ErrPlanStorySetHasStories = errors.New("the scope already has stories, so no story set can be proposed")

// PlanSetPromotionCommand is the exact text the owner replies with to promote the
// proposed set of the given revision.
func PlanSetPromotionCommand(revision string) string {
	return PlanSetPromotionPrefix + revision
}

// PlanStorySetRevision derives the revision of a proposed set from its content
// alone: the ordered entries with their temporary identities, names, intents,
// dependencies and optional sizing. The same content always has the same revision
// and any change to it has another, so a review of one revision can never
// stand for a different set. The order in which one entry lists its dependencies
// carries no meaning and does not change the revision.
func PlanStorySetRevision(entries []PlanStorySetEntry) string {
	canonical := make([]PlanStorySetEntry, len(entries))
	for i, e := range entries {
		e.ID, e.Name, e.Intent = strings.TrimSpace(e.ID), strings.TrimSpace(e.Name), strings.TrimSpace(e.Intent)
		deps := slices.Clone(e.DependsOn)
		slices.Sort(deps)
		if len(deps) == 0 {
			deps = nil
		}
		e.DependsOn = deps
		canonical[i] = e
	}
	raw, _ := json.Marshal(struct {
		Kind    string              `json:"kind"`
		Entries []PlanStorySetEntry `json:"entries"`
	}{"plan-story-set/1", canonical})
	return fmt.Sprintf("%x", sha256.Sum256(raw))
}

// ProposeStorySet records a proposed set with its content revision and reports
// whether the saved state changed. The same content proposed again keeps its
// revision and changes nothing, so a repeated or recovered effect is harmless. A
// different proposal replaces an unpromoted set under a new revision. A promoted
// set is never replaced, and a scope that already has stories takes no set.
func (s *PlanState) ProposeStorySet(entries []PlanStorySetEntry) (revision string, changed bool, err error) {
	if len(entries) == 0 || len(entries) > MaxPlanStorySetEntries {
		return "", false, fmt.Errorf("a story set has 1 to %d entries, not %d", MaxPlanStorySetEntries, len(entries))
	}
	revision = PlanStorySetRevision(entries)
	if s.StorySet != nil {
		if s.StorySet.Revision == revision {
			return revision, false, nil
		}
		if s.StorySet.Promoted {
			return "", false, ErrPlanStorySetPromoted
		}
	}
	if len(s.Stories) > 0 {
		return "", false, ErrPlanStorySetHasStories
	}
	policy := PlanStorySetSizingPolicyVersion
	if s.StorySet != nil {
		policy = s.StorySet.SizingPolicyVersion
	}
	set := &PlanStorySetState{Revision: revision, Entries: make([]PlanStorySetEntry, len(entries)), SizingPolicyVersion: policy}
	for i, e := range entries {
		e.ID, e.Name, e.Intent = strings.TrimSpace(e.ID), strings.TrimSpace(e.Name), strings.TrimSpace(e.Intent)
		e.DependsOn = slices.Clone(e.DependsOn)
		set.Entries[i] = e
	}
	s.StorySet = set
	return revision, true, nil
}

// RenderPlanStorySet writes the owner-facing account of a proposed set: its
// revision, a table of the ordered stories with their temporary ids, names,
// intents and the ids of the stories each depends on, and the exact command
// that promotes it. The text only asks; it promotes nothing.
func RenderPlanStorySet(s *PlanStorySetState) (string, error) {
	return RenderPlanStorySetFor(s, false)
}

// RenderPlanStorySetFor renders the set for its destination; on Linear the
// agent-written cells are inert text instead of GitHub's pipe-only escaping.
func RenderPlanStorySetFor(s *PlanStorySetState, onLinear bool) (string, error) {
	if s == nil || s.Revision == "" || len(s.Entries) == 0 {
		return "", errors.New("there is no proposed story set to render")
	}
	ids := make(map[string]bool, len(s.Entries))
	for _, e := range s.Entries {
		ids[e.ID] = true
	}
	var out strings.Builder
	fmt.Fprintf(&out, "Proposed story set, revision `%s`\n\n", s.Revision)
	showSizing := s.SizingApplicable() || slices.ContainsFunc(s.Entries, func(e PlanStorySetEntry) bool { return e.Size != "" || e.SizeReason != "" })
	if showSizing {
		out.WriteString("| ID | Name | Description | Dependencies | Size | Size reason |\n|---|---|---|---|---|---|\n")
	} else {
		out.WriteString("| ID | Name | Description | Dependencies |\n|---|---|---|---|\n")
	}
	gaps := s.SizingGaps()
	for _, e := range s.Entries {
		deps := make([]string, 0, len(e.DependsOn))
		for _, id := range e.DependsOn {
			if !ids[id] {
				return "", fmt.Errorf("story %q depends on unknown %q", e.ID, id)
			}
			deps = append(deps, "`"+id+"`")
		}
		dependsOn := "none"
		if len(deps) > 0 {
			dependsOn = strings.Join(deps, ", ")
		}
		fmt.Fprintf(&out, "| `%s` | %s | %s | %s |", e.ID, tableCellFor(onLinear, e.Name), tableCellFor(onLinear, e.Intent), dependsOn)
		if showSizing {
			size, reason := tableCellFor(onLinear, e.Size), tableCellFor(onLinear, e.SizeReason)
			for _, gap := range gaps {
				if gap.EntryID != e.ID {
					continue
				}
				if gap.Field == "size" {
					if e.Size == "" {
						size = "missing"
					} else {
						size += " (invalid)"
					}
					size += "; correction requested: " + gap.Expected
				} else {
					reason = "missing; correction requested: " + gap.Expected
				}
			}
			fmt.Fprintf(&out, " %s | %s |", size, reason)
		}
		out.WriteString("\n")
	}
	out.WriteString("\nThis is a proposal only: no story exists until you promote it.\n")
	fmt.Fprintf(&out, "To promote exactly this set, reply `%s`.\n", PlanSetPromotionCommand(s.Revision))
	out.WriteString("To change it, reply with what to change. A changed set gets a new revision and needs a new review; a reply naming an older revision is refused.")
	return out.String(), nil
}

// tableCell keeps agent text inside one Markdown table cell: a pipe would end
// the cell and a line break would end the row.
// tableCellFor applies the destination's cell contract.
func tableCellFor(onLinear bool, text string) string {
	if onLinear {
		return linearmd.InlineText(text)
	}
	return tableCell(text)
}

func tableCell(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	return strings.ReplaceAll(text, "|", `\|`)
}
