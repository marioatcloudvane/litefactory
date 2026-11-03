package turn

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/teamsource/teamsourcetest"
)

// Transform captured bytes in memory; shipped team graphs remain owner-owned.
func operationValidateProfile(t *testing.T) *graphexec.Profile {
	t.Helper()
	raw := string(captureValidate(t).Files["validate.dot"])
	for _, op := range []string{"push", "merge", "close"} {
		raw = strings.ReplaceAll(raw, `lf_action="lf_validate_`+op+`"`, `lf_action="lf_deliver",lf_operation="`+op+`"`)
	}
	p, err := graphexec.ParseProfile([]byte(raw))
	mustProgress(t, err)
	specs, err := ValidateFlowSpecs()
	mustProgress(t, err)
	mustProgress(t, graphexec.Validate(p, specs))
	mustProgress(t, graphexec.ValidateControlGraph(p, graphexec.ValidateControlPolicy()))
	return p
}

func TestDeliveryValidatePreservesCommandBoundaries(t *testing.T) {
	for _, command := range []string{"ship", "push"} {
		t.Run(command, func(t *testing.T) {
			o := newOwnerFixtureOn(t, store.ForgeGitHub, true)
			if command == "ship" {
				o.mixed()
				o.say(o.ship())
				if o.final() != "delivered" || len(o.forge.merges) != 1 || len(o.forge.issueCloses) != 2 {
					t.Fatalf("shipping failed: final=%s merges=%v closes=%v", o.final(), o.forge.merges, o.forge.issueCloses)
				}
			} else {
				o.readyToPush()
				o.say(o.command(ValidatePushCommand))
				if len(o.forge.prCreates) != 1 || len(o.forge.merges) != 0 || len(o.forge.issueCloses) != 0 {
					t.Fatal("push exceeded its authorization")
				}
				o.say(o.command(ValidateMergeCommand, "601", o.state().Commit))
				if len(o.forge.merges) != 1 || len(o.forge.issueCloses) != 0 {
					t.Fatal("merge exceeded its authorization")
				}
				o.say(o.command(ValidateCloseCommand, "497", "520"))
				if o.final() != "delivered" {
					t.Fatalf("close: %s", o.final())
				}
			}
		})
	}
}

func TestDeliveryBugSeparatesEffectsAndRechecksCandidate(t *testing.T) {
	for _, drift := range []string{"none", "head", "base", "missing preparation"} {
		t.Run(drift, func(t *testing.T) {
			f, binding, req, forge, head := graphFinalizationFixture(t)
			actual, base := head, "base"
			saves := 0
			save := binding.Save
			binding.Save = func(ctx context.Context, opts Options, req ActivityRequest, b GraphDeliveryBinding) (string, error) {
				saves++
				return save(ctx, opts, req, b)
			}
			binding.Inspect = func(context.Context, GraphDeliveryBinding) (string, string, error) { return actual, base, nil }
			profile := &graphexec.Profile{Workflow: agent.WorkflowBug, Nodes: []graphexec.Node{
				{Name: req.Node, Action: graphexec.DeliverAction, Attrs: map[string]string{"lf_operation": "push"}},
				{Name: "merge", Action: graphexec.DeliverAction, Attrs: map[string]string{"lf_operation": "merge"}},
				{Name: "close", Action: graphexec.DeliverAction, Attrs: map[string]string{"lf_operation": "close"}},
			}}
			h := DeliveryActivity{profile: profile, bug: &binding}
			if drift != "missing preparation" {
				out, err := h.Execute(t.Context(), f.opts, req)
				mustProgress(t, err)
				if out.Outcome != "ready" || forge.merges != 0 || forge.closes != 0 {
					t.Fatalf("push crossed boundary: %s", out.Diagnostic)
				}
				_, err = f.db.RecordGraphOutput(t.Context(), store.GraphOutput{NodeAttemptID: req.NodeAttemptID, Kind: out.Kind, Payload: out.Output, CreatedAt: f.opts.MCP.Now()})
				mustProgress(t, err)
			}
			if drift == "head" {
				actual = "changed"
			}
			if drift == "base" {
				base = "changed"
			}
			if drift == "none" {
				req.Node = "close"
				denied, err := h.Execute(t.Context(), f.opts, req)
				mustProgress(t, err)
				if denied.Outcome != "blocked" || forge.merges != 0 || forge.closes != 0 {
					t.Fatal("close authorized an unmerged candidate")
				}
			}
			req.Node = "merge"
			out, err := h.Execute(t.Context(), f.opts, req)
			mustProgress(t, err)
			if drift != "none" {
				if out.Outcome != "revalidate" && out.Outcome != "blocked" || forge.merges != 0 || forge.closes != 0 {
					t.Fatal("changed or unprepared candidate shipped")
				}
				return
			}
			if out.Outcome != "merged" || forge.merges != 1 || forge.closes != 0 {
				t.Fatalf("merge crossed boundary: %s", out.Diagnostic)
			}
			req.Node = "close"
			out, err = h.Execute(t.Context(), f.opts, req)
			mustProgress(t, err)
			if out.Outcome != "completed" || forge.merges != 1 || forge.closes != 1 || saves != 1 {
				t.Fatalf("close repeated earlier effects: %s saves=%d", out.Diagnostic, saves)
			}
			var progress graphDeliveryProgress
			mustProgress(t, json.Unmarshal(out.Output, &progress))
			if !progress.Integration || !progress.Closed {
				t.Fatal("missing confirmed evidence")
			}
		})
	}
}

func TestDeliveryBugGraphPreservesApprovalRoutes(t *testing.T) {
	specs, err := SimplifiedBugSpecs()
	mustProgress(t, err)
	def, err := teamsourcetest.CaptureBug(t.Context(), specs)
	mustProgress(t, err)
	raw := string(def.Files["bug.dot"])
	for _, line := range strings.Split(raw, "\n") {
		if strings.Contains(line, `lf_action="lf_bug_finalize"`) {
			push := strings.ReplaceAll(strings.ReplaceAll(line, `lf_action="lf_bug_finalize"`, `lf_action="lf_deliver",lf_operation="push"`), `completed,revalidate`, `ready,revalidate`)
			merge := strings.ReplaceAll(strings.ReplaceAll(push, `finalization [`, `integrate [`), `lf_operation="push"`, `lf_operation="merge"`)
			merge = strings.ReplaceAll(merge, `ready,revalidate`, `merged,revalidate`)
			closeDecl := strings.ReplaceAll(strings.ReplaceAll(push, `finalization [`, `finish_delivery [`), `lf_operation="push"`, `lf_operation="close"`)
			closeDecl = strings.ReplaceAll(closeDecl, `ready,revalidate`, `completed,revalidate`)
			raw = strings.Replace(raw, line, push+"\n"+merge+"\n"+closeDecl, 1)
			break
		}
	}
	raw = strings.Replace(raw, `finalization -> completed [lf_on="completed"];`, `finalization -> integrate [lf_on="ready"];
 integrate -> finish_delivery [lf_on="merged"];
 integrate -> sync_before_fixer [lf_on="revalidate"];
 finish_delivery -> sync_before_fixer [lf_on="revalidate"];
 finish_delivery -> completed [lf_on="completed"];`, 1)
	for _, test := range []struct{ name, from, to string }{
		{"valid", "", ""},
		{"missing operation", `lf_operation="push"`, ``},
		{"bypass approval", `fixer -> finalization`, `conversation -> finalization`},
		{"merge bypass", `finalization -> integrate [lf_on="ready"]`, `fixer -> integrate [lf_on="ready"]`},
	} {
		t.Run(test.name, func(t *testing.T) {
			dot := raw
			if test.from != "" {
				dot = strings.Replace(dot, test.from, test.to, 1)
			}
			p, err := graphexec.ParseProfile([]byte(dot))
			if err == nil {
				err = graphexec.Validate(p, specs)
			}
			if (err == nil) != (test.name == "valid") {
				t.Fatalf("validation: %v", err)
			}
		})
	}
}
