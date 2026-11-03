package graphexec_test

import (
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/graphexec"
)

func TestDeliveryGraphRequiresOwnerBoundary(t *testing.T) {
	raw := []byte(deliveryOperationFixture)
	for _, test := range []struct{ name, from, to string }{
		{"valid", "", ""},
		{"wrong operation", `lf_operation="push"`, `lf_operation="merge"`},
		{"missing approval", `lf_approve_reply="ship"`, `lf_approve_reply=""`},
		{"shipping bypass", `prepare -> review [lf_on="ready"]`, `prepare -> deliver [lf_on="ready"]`},
		{"review runs ahead", `review -> waiting [lf_on="published"]`, `review -> deliver [lf_on="published"]`},
		{"foreign source", `deliver [lf_action="lf_deliver", lf_operation="merge", lf_source="spike"`, `deliver [lf_action="lf_deliver", lf_operation="merge", lf_source="unknown"`},
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

const deliveryOperationFixture = `// One conversational agent authors checked results. Generic factory delivery
// prepares changed candidates, waits for an exact authenticated ship reply,
// and merges/closes only the reviewed commit. Source changes return for checks.
// Questions and ordinary review feedback resume the same agent.
digraph spike {
 graph [team="{{team}}", mode="spike", profile_version="2", start="spike"];

 spike    [lf_action="lf_agent", lf_agent_markdown="spike", lf_skills="spike-result", lf_access="write", lf_response="result", lf_outcomes="ask,done,failed", lf_limit="8"];
 prepare [lf_action="lf_deliver", lf_operation="push", lf_source="spike", lf_outcomes="ready,unchanged,revalidate,blocked", lf_limit="8", lf_park="owner", lf_stop="blocked,failed,invalid,exhausted"];
 review [lf_action="lf_ask", lf_kind="review", lf_source="spike", lf_approve_reply="ship", lf_outcomes="published,approved", lf_limit="8"];
 deliver [lf_action="lf_deliver", lf_operation="merge", lf_source="spike", lf_outcomes="merged,revalidate,blocked", lf_limit="8", lf_park="owner", lf_stop="blocked,failed,invalid,exhausted"];
 close [lf_action="lf_deliver", lf_operation="close", lf_source="spike", lf_outcomes="completed,revalidate,blocked", lf_limit="8", lf_park="owner", lf_stop="blocked,failed,invalid,exhausted"];
 ask      [lf_action="lf_ask", lf_kind="question", lf_outcomes="published", lf_source="spike", lf_limit="8"];
 report   [lf_action="lf_comment", lf_kind="final", lf_outcomes="published", lf_source="spike", lf_limit="8"];
 failure  [lf_action="lf_comment", lf_kind="failure", lf_outcomes="published", lf_limit="8"];
 waiting  [lf_terminal="true"];
 complete [lf_terminal="true"];
 stopped  [lf_terminal="true"];

 spike   -> ask [lf_on="ask"];
 spike   -> prepare  [lf_on="done"];
 prepare -> report [lf_on="unchanged"];
 prepare -> spike [lf_on="revalidate"];
 prepare -> review [lf_on="ready"];
 review -> waiting [lf_on="published"];
 review -> deliver [lf_on="approved"];
 deliver -> spike [lf_on="revalidate"];
 deliver -> close [lf_on="merged"];
 close -> complete [lf_on="completed"];
 close -> spike [lf_on="revalidate"];
 spike   -> failure  [lf_on="failed"];
 ask     -> waiting  [lf_on="published"];
 report  -> complete [lf_on="published"];
 failure -> stopped  [lf_on="published"];
}
`
