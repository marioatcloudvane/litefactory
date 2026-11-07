package graphexec_test

import (
	"testing"

	"github.com/cloudvane/litefactory/internal/graphexec"
)

// handoffDOT has a first agent whose measured proposal is handed to a second,
// write-capable agent. The second agent's ask is settled by a measure step
// (nobody is asked); its done is measured, noted and reported.
const handoffDOT = `digraph {team=builtin;mode=research;profile_version=1;start=loop;
loop [lf_action=lf_loop,lf_outcomes="true,false",lf_field=response,lf_condition=ask,lf_default=ask,lf_source=work];
work [lf_action=lf_agent,lf_outcomes="success,failed",lf_access=read];
wants [lf_action=lf_if,lf_outcomes="true,false",lf_field=response,lf_condition=ask,lf_source=work];
gauge [lf_action=lf_research_measure,lf_source=work,lf_forbid="alpha",lf_outcomes="one,two,failed"];
question [lf_action=lf_ask,lf_kind=question,lf_source=work,lf_outcomes=published];
report [lf_action=lf_comment,lf_kind=final,lf_source=work,lf_outcomes=published];
worker [lf_action=lf_agent,lf_outcomes="success,failed",lf_access=write];
wworker [lf_action=lf_if,lf_outcomes="true,false",lf_field=response,lf_condition=ask,lf_source=worker];
verify [lf_action=lf_research_finding,lf_source=worker,lf_forbid="alpha",lf_outcomes="one,two,failed"];
noteb [lf_action=lf_research_measure,lf_source=worker,lf_forbid="alpha",lf_outcomes="one,two,failed"];
report2 [lf_action=lf_comment,lf_kind=final,lf_source=worker,lf_outcomes=published];
oops [lf_action=lf_comment,lf_kind=failure,lf_outcomes=published];
wait [lf_terminal=true];end [lf_terminal=true];stop [lf_terminal=true];
loop -> work [lf_on=true];loop -> gauge [lf_on=false];
work -> wants [lf_on=success];work -> oops [lf_on=failed];
wants -> question [lf_on=true];wants -> gauge [lf_on=false];
gauge -> worker [lf_on=one];gauge -> report [lf_on=two];gauge -> oops [lf_on=failed];
worker -> wworker [lf_on=success];worker -> oops [lf_on=failed];
wworker -> noteb [lf_on=true];wworker -> verify [lf_on=false];
verify -> report2 [lf_on=one];verify -> report2 [lf_on=two];verify -> oops [lf_on=failed];
noteb -> report2 [lf_on=one];noteb -> report2 [lf_on=two];noteb -> oops [lf_on=failed];
question -> wait [lf_on=published];report -> end [lf_on=published];report2 -> end [lf_on=published];oops -> stop [lf_on=published];}`

func TestResponseReachesOwnerWaitFollowsTheEdges(t *testing.T) {
	policy := graphexec.ResearchControlPolicy()
	policy.MeasureActions = []string{"lf_research_measure", "lf_research_finding"}
	profile, err := graphexec.ParseProfile([]byte(handoffDOT))
	if err != nil {
		t.Fatal(err)
	}
	if !graphexec.ResponseReachesOwnerWait(profile, "work", policy) {
		t.Fatal("the first agent's ask ends at an owner question")
	}
	if graphexec.ResponseReachesOwnerWait(profile, "worker", policy) {
		t.Fatal("the handed-off agent's ask has no owner to wait for")
	}
	if graphexec.ResponseReachesOwnerWait(nil, "work", policy) || graphexec.ResponseReachesOwnerWait(profile, "missing", policy) {
		t.Fatal("an absent graph or agent reaches an owner wait")
	}
}
