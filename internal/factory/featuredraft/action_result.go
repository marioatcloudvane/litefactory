package featuredraft

import "github.com/cloudvane/litefactory/internal/owneraction"

// ActionResult retains the existing owner-action outcome for this draft result.
func (r Result) ActionResult() owneraction.Result {
	state := "succeeded"
	switch r.Status {
	case Uncertain:
		state = "uncertain"
	case Refusal, ReportableError:
		state = "failed"
	}
	return owneraction.Result{State: state, Summary: r.Text}
}
