package turn

import (
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/store"
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
