package brainstormtasks

import (
	"bytes"
	_ "embed"
	"fmt"
	"strings"
	"text/template"

	"github.com/cloudvane/litefactory/internal/store"
)

const (
	outcomeStarted      = "started"
	outcomeNotStarted   = "not_started"
	outcomeUncertain    = "uncertain"
	outcomeNotCreated   = "not_created"
	outcomeNotAttempted = "not_attempted"
	outcomeReplaced     = "replaced"
)

//go:embed templates/summary.md.tmpl
var summarySource string

var summaryTemplate = template.Must(template.New("summary").Parse(summarySource))

type summaryLine struct{ Title, Outcome, Issue, Reason string }

// renderSummary gives every frozen item of a settled continuation one outcome.
func renderSummary(c store.BrainstormTaskContinuation, items map[string]store.BrainstormTaskItem) (string, error) {
	view := struct {
		Created int
		Lines   []summaryLine
	}{}
	for _, f := range c.Items {
		it, ok := items[f.ItemID]
		if !ok {
			continue
		}
		o, settled := outcome(it, f, true)
		if !settled {
			o = outcomeNotAttempted
		}
		line := summaryLine{Title: it.Title, Outcome: o, Reason: it.Reason}
		if it.IssueNumber > 0 {
			line.Issue = fmt.Sprintf("#%d", it.IssueNumber)
			view.Created++
		}
		view.Lines = append(view.Lines, line)
	}
	var b bytes.Buffer
	if err := summaryTemplate.Execute(&b, view); err != nil {
		return "", err
	}
	return strings.TrimSpace(b.String()), nil
}
