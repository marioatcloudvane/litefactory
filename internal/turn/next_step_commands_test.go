package turn

import (
	"regexp"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/teamsource"
)

// quotedModeCommand finds a backticked mode command a reply tells the owner to
// type.
var quotedModeCommand = regexp.MustCompile("`(/(?:spike|research|plan|implement|validate)(?: [^`]*)?)`")

// Every next-step line that tells the owner to type a mode command must name a
// form the parser runs. A bare `/plan` is parsed as an empty command and only
// earns a hint, so a reply that suggests it sends the owner nowhere.
func TestNextStepCommandsStartARun(t *testing.T) {
	t.Parallel()
	specs, err := ResearchSpecs()
	if err != nil {
		t.Fatal(err)
	}
	d, err := teamsource.CaptureResearch(t.Context(), specs)
	if err != nil {
		t.Fatal(err)
	}
	steps := map[string]string{
		"research review": researchNextStep(d.Profile, "review", "review"),
		"spike hand-back": spikeNextStep(d.Profile, "", "final"),
		"plan ready":      planReadyText(naming.Ref{}, nil),
	}
	for _, node := range d.Profile.Nodes {
		if step := researchNextStep(d.Profile, node.Name, "final"); strings.Contains(step, "/plan") {
			steps["research final"] = step
		}
	}
	if _, ok := steps["research final"]; !ok {
		t.Fatal("no research final next step names /plan")
	}
	for name, step := range steps {
		matches := quotedModeCommand.FindAllStringSubmatch(step, -1)
		if len(matches) == 0 {
			t.Errorf("%s names no mode command: %q", name, step)
		}
		for _, m := range matches {
			if got := ParseCommand(m[1]).Kind; got != KindSpike {
				t.Errorf("%s suggests %q, which parses as kind %v, not KindSpike", name, m[1], got)
			}
		}
	}
}
