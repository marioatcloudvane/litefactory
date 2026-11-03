package graphexec_test

import (
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/graphexec"
)

func routedPolicy() graphexec.ControlPolicy {
	return graphexec.ControlPolicy{Mode: agent.ModeSpike, Responses: []string{"ask", "done"}, Defaults: []string{"ask", "done"}, Ask: "ask"}
}

const routedGraph = `digraph {team=test;mode=spike;profile_version=1;start=work;
 work [lf_action=lf_agent,lf_response=result,lf_outcomes="ask,done,failed",lf_limit=8];
 ask [lf_action=lf_ask,lf_kind=question,lf_outcomes=published,lf_source=work];
 report [lf_action=lf_comment,lf_kind=final,lf_outcomes=published,lf_source=work];
 failure [lf_action=lf_comment,lf_kind=failure,lf_outcomes=published];
 waiting [lf_terminal=true]; complete [lf_terminal=true]; stopped [lf_terminal=true];
 work -> ask [lf_on=ask]; work -> report [lf_on=done]; work -> failure [lf_on=failed];
 ask -> waiting [lf_on=published]; report -> complete [lf_on=published]; failure -> stopped [lf_on=published];
}`

func parseRouted(t *testing.T, dot string) *graphexec.Profile {
	t.Helper()
	p, err := graphexec.ParseProfile([]byte(dot))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// A result-routed agent's response edges are validated like any response
// path: ask must come to rest at an owner question, done at a report.
func TestRoutedAgentResponsesGoStraightToPublications(t *testing.T) {
	p := parseRouted(t, routedGraph)
	if err := graphexec.ValidateControlGraph(p, routedPolicy()); err != nil {
		t.Fatalf("routed graph refused: %v", err)
	}
	if !graphexec.ResponseReachesOwnerWait(p, "work", routedPolicy()) {
		t.Fatal("a routed agent's ask must reach the owner wait")
	}
	for name, edit := range map[string]func(string) string{
		"ask ends at a report": func(s string) string {
			return strings.Replace(s, "work -> ask [lf_on=ask]", "work -> report [lf_on=ask]", 1)
		},
		"done waits for the owner": func(s string) string {
			return strings.Replace(s, "work -> report [lf_on=done]", "work -> ask [lf_on=done]", 1)
		},
		"routed agent declares success": func(s string) string {
			return strings.Replace(strings.Replace(s, `"ask,done,failed"`, `"success,ask,done,failed"`, 1), "work -> failure", "work -> report [lf_on=success]; work -> failure", 1)
		},
		"response outcome without routing": func(s string) string {
			return strings.Replace(s, "lf_response=result,", "", 1)
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := graphexec.ValidateControlGraph(parseRouted(t, edit(routedGraph)), routedPolicy()); err == nil {
				t.Fatal("invalid routed graph was accepted")
			}
		})
	}
}
