package standards

import (
	"fmt"
	"slices"
	"strings"

	"github.com/cloudvane/litefactory/internal/store"
)

// Render shows standards grouped under their category, in book order. It is
// the one presentation of a standard for agents, owners and reviewers.
// Categories are taken from categories; a standard whose category is not
// there is grouped under its category ID.
func Render(categories []Category, items []Standard) string {
	var b strings.Builder
	group := func(id string) string {
		for _, c := range categories {
			if c.ID == id {
				return c.Name
			}
		}
		return id
	}
	ordered := append([]Standard{}, items...)
	slices.SortStableFunc(ordered, func(a, b Standard) int {
		return strings.Compare(a.CategoryID+"\x00"+a.ID, b.CategoryID+"\x00"+b.ID)
	})
	for i, s := range ordered {
		if i == 0 || ordered[i-1].CategoryID != s.CategoryID {
			if i > 0 {
				b.WriteString("\n")
			}
			b.WriteString("# " + group(s.CategoryID) + "\n\n")
		}
		renderStandard(&b, s)
	}
	return b.String()
}

func renderStandard(b *strings.Builder, s Standard) {
	exception := "No"
	if s.ExceptionPolicy != nil && s.ExceptionPolicy.Mode == "allow-task" {
		exception = "Yes"
	}
	b.WriteString("## " + s.Rule + " (" + s.ID + ")\n")
	for _, section := range [][2]string{
		{"Description", s.Description},
		{"Evidence", s.Evidence},
		{"Scope", s.Scope},
		{"Check", CheckLabel(s.Check)},
		{"Exception Allowed", exception},
		{"Applies to", strings.Join(s.AppliesTo, ", ")},
	} {
		b.WriteString("### " + section[0] + "\n" + strings.TrimSpace(section[1]) + "\n")
	}
	b.WriteString("\n")
}

// CheckLabel names a check kind for people.
func CheckLabel(check string) string {
	switch check {
	case store.StandardsCheckAutomated:
		return "Automated"
	case store.StandardsCheckSemiAutomated:
		return "Semi-automated"
	case store.StandardsCheckManual:
		return "Manual"
	}
	return check
}

// Load counts the standards that apply to each role and that sit in each
// category, so a reader can see how much one role or category carries.
type Load struct {
	Roles      map[string]int `json:"roles"`
	Categories map[string]int `json:"categories"`
}

// BookLoad counts a snapshot's standards per applies_to role and per category ID.
func BookLoad(snap Snapshot) Load {
	out := Load{Roles: map[string]int{}, Categories: map[string]int{}}
	for _, c := range snap.Categories {
		out.Categories[c.ID] = 0
	}
	for _, s := range snap.Standards {
		out.Categories[s.CategoryID]++
		for _, role := range s.AppliesTo {
			out.Roles[role]++
		}
	}
	return out
}

// FormatLoad lists a snapshot's load per role and per category for people.
func FormatLoad(snap Snapshot) string {
	load := BookLoad(snap)
	var b strings.Builder
	b.WriteString("# Load\n")
	roles := make([]string, 0, len(load.Roles))
	for role := range load.Roles {
		roles = append(roles, role)
	}
	slices.Sort(roles)
	for _, role := range roles {
		fmt.Fprintf(&b, "%s: %d\n", role, load.Roles[role])
	}
	for _, c := range snap.Categories {
		fmt.Fprintf(&b, "category %s: %d\n", c.Name, load.Categories[c.ID])
	}
	return b.String()
}
