package graphexec_test

import (
	"os"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/graphexec"
)

func TestDeliveryGraphRequiresOwnerBoundary(t *testing.T) {
	raw, err := os.ReadFile("../../teams/source/base-team/spike/spike.dot")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ name, from, to string }{
		{"valid", "", ""},
		{"wrong phase", `lf_phase="prepare"`, `lf_phase="ship"`},
		{"missing approval", `lf_approve_reply="ship"`, `lf_approve_reply=""`},
		{"shipping bypass", `prepare -> review [lf_on="ready"]`, `prepare -> deliver [lf_on="ready"]`},
		{"review runs ahead", `review -> waiting [lf_on="published"]`, `review -> deliver [lf_on="published"]`},
		{"foreign source", `deliver [lf_action="lf_deliver", lf_phase="ship", lf_source="spike"`, `deliver [lf_action="lf_deliver", lf_phase="ship", lf_source="unknown"`},
	} {
		t.Run(test.name, func(t *testing.T) {
			dot := string(raw)
			if test.from != "" {
				if !strings.Contains(dot, test.from) {
					t.Fatal("graph mutation did not apply")
				}
				dot = strings.Replace(dot, test.from, test.to, 1)
			}
			p, e := graphexec.ParseProfile([]byte(dot))
			if e == nil {
				e = graphexec.ValidateControlGraph(p, routedPolicy())
			}
			if (e == nil) != (test.name == "valid") {
				t.Fatalf("validation = %v", e)
			}
		})
	}
	for _, mode := range []agent.Mode{agent.ModeResearch, agent.ModePlan, agent.ModeValidate, agent.ModeSpike} {
		registry := graphexec.Registry{}
		if err := registry.Register(graphexec.DeliverSpec(mode)); err != nil {
			t.Fatalf("generic delivery unavailable to %s: %v", mode, err)
		}
	}
}
