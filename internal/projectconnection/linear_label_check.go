package projectconnection

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/linear"
	"github.com/cloudvane/litefactory/internal/store"
)

// maxRemedy is the longest remedy the store saves.
const maxRemedy = 512

// labelFinding is one reason of the label check and the names it applies to.
type labelFinding struct {
	Severity string // "Blocking" or "Warning"
	Lead     string // the reason, such as "missing in Linear"
	Noun     string // the plural used when only a count fits
	Items    []string
}

// labelDiagnostics is what the label check adds to a Linear remedy.
type labelDiagnostics struct {
	Findings []labelFinding
	// Tail follows the findings: the setup command, or the read failure.
	Tail string
	// RunnerLines says the per-runner warnings replace the generic one.
	RunnerLines bool
}

func (d *labelDiagnostics) add(severity, lead, noun, item string) {
	for i := range d.Findings {
		if d.Findings[i].Severity == severity && d.Findings[i].Lead == lead {
			d.Findings[i].Items = append(d.Findings[i].Items, item)
			return
		}
	}
	d.Findings = append(d.Findings, labelFinding{Severity: severity, Lead: lead, Noun: noun, Items: []string{item}})
}

// linearLabelDiagnostics reads the team and workspace labels and compares
// them with the wanted labels and the stored maps. It never writes.
func (f *Factory) linearLabelDiagnostics(ctx context.Context, client *linear.Client, v store.ConnectionVersion) labelDiagnostics {
	found, err := client.ListSetupLabels(ctx)
	if err != nil {
		return labelDiagnostics{Tail: " Linear labels could not be read."}
	}
	modes, _ := v.Linear.ModeLabels()
	agents, _ := v.Linear.CodingAgentLabels()
	bug, _ := v.Linear.BugLabel()
	plan := PlanLinearLabels(agent.WantedLabels(f.options.LabelAgents), found, modes, agents, bug)
	var blocking, warnings labelDiagnostics
	for _, e := range plan.Entries {
		target := &warnings
		severity := "Warning"
		if e.Wanted.Family == agent.LabelFamilyMode {
			target, severity = &blocking, "Blocking"
		}
		switch {
		case e.Severity != "":
			target.add(severity, shortLabelReason(e), "labels", e.Wanted.Name)
		case e.Status == StatusCreate:
			target.add(severity, "missing in Linear", "labels", e.Wanted.Name)
		}
	}
	d := labelDiagnostics{Findings: append(blocking.Findings, warnings.Findings...), RunnerLines: f.options.LabelAgents != nil}
	if d.RunnerLines {
		for _, runner := range unmappedRunners(f.options.LabelAgents, agents) {
			d.add("Warning", "no coding agent label is mapped for runner", "runners", runner)
		}
	}
	if len(d.Findings) > 0 {
		d.Tail = " Run `" + LabelSetupCommand(v.RepoID) + "`."
	}
	return d
}

func shortLabelReason(e LinearLabelEntry) string {
	switch e.Status {
	case StatusMissing:
		return "missing; its group exists only at workspace level"
	case StatusStored, StatusAdopted:
		return "name the factory cannot read"
	}
	return string(e.Status)
}

// unmappedRunners lists each registered runner kind without a stored coding agent key.
func unmappedRunners(kinds []string, stored map[string]string) []string {
	var out []string
	seen := map[string]bool{}
	for _, k := range kinds {
		k = strings.ToLower(strings.TrimSpace(k))
		if k == "" || seen[k] {
			continue
		}
		seen[k] = true
		if _, ok := linear.KeyForKind(stored, k); !ok {
			out = append(out, k)
		}
	}
	return out
}

// renderFindings writes one sentence per severity. limits[i] caps the names
// listed for findings[i]; 0 gives only a count.
func renderFindings(findings []labelFinding, limits []int) string {
	var b strings.Builder
	for i, fd := range findings {
		if i == 0 || findings[i-1].Severity != fd.Severity {
			fmt.Fprintf(&b, " %s: ", fd.Severity)
		} else {
			b.WriteString("; ")
		}
		n, limit := len(fd.Items), limits[i]
		switch {
		case limit >= n:
			fmt.Fprintf(&b, "%s: %s", fd.Lead, strings.Join(fd.Items, ", "))
		case limit == 0:
			fmt.Fprintf(&b, "%s: %d %s", fd.Lead, n, fd.Noun)
		default:
			fmt.Fprintf(&b, "%s: %s and %d more", fd.Lead, strings.Join(fd.Items[:limit], ", "), n-limit)
		}
		if i == len(findings)-1 || findings[i+1].Severity != fd.Severity {
			b.WriteString(".")
		}
	}
	return b.String()
}

// boundRemedy joins head, the findings and tail within maxRemedy bytes. It lists
// fewer names, then uses compactHead, then counts only, then cuts the head;
// the severity words and tail always stay.
func boundRemedy(head, compactHead string, findings []labelFinding, tail string) string {
	limits := make([]int, len(findings))
	fit := func(h string, floor int) (string, bool) {
		for i, fd := range findings {
			limits[i] = len(fd.Items)
		}
		for {
			if s := h + renderFindings(findings, limits) + tail; len(s) <= maxRemedy {
				return s, true
			}
			widest := -1
			for i, l := range limits {
				if l > floor && (widest < 0 || l > limits[widest]) {
					widest = i
				}
			}
			if widest < 0 {
				return "", false
			}
			limits[widest]--
		}
	}
	if s, ok := fit(head, 1); ok {
		return s
	}
	if s, ok := fit(compactHead, 0); ok {
		return s
	}
	body := renderFindings(findings, limits) + tail
	if len(body) >= maxRemedy {
		return cutUTF8(body, maxRemedy)
	}
	return cutUTF8(compactHead, maxRemedy-len(body)) + body
}

func cutUTF8(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}
