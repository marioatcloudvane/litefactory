package graphexec

import (
	"maps"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
)

func routedImplementDOT() string {
	dot := strings.Replace(implementTestDOT, ` budget -> worker_agent [lf_on="run"];`, `
 budget -> worker_sync [lf_on="run"];
 worker_sync [lf_action="lf_sync_base",lf_outcomes="synced,current,conflict,retry,deferred",lf_limit="8"];
 worker_sync -> sizing_valid [lf_on="synced"];
 worker_sync -> sizing_valid [lf_on="current"];
 worker_sync -> sizing_valid [lf_on="deferred"];
 worker_sync -> worker_sync [lf_on="retry"];
 worker_sync -> park_story [lf_on="conflict"];
 sizing_valid [lf_action="lf_if",lf_field="story.sizing_valid",lf_condition="true",lf_outcomes="true,false",lf_limit="3"];
 small [lf_action="lf_if",lf_field="story.size",lf_condition="S",lf_outcomes="true,false",lf_limit="3"];
 medium [lf_action="lf_if",lf_field="story.size",lf_condition="M",lf_outcomes="true,false",lf_limit="3"];
 sizing_valid -> small [lf_on="true"];
 sizing_valid -> worker_flagship [lf_on="false"];
 small -> worker_agent [lf_on="true"];
 small -> medium [lf_on="false"];
 medium -> worker_agent [lf_on="true"];
 medium -> worker_flagship [lf_on="false"];
 worker_flagship [lf_action="lf_agent",lf_outcomes="success,failed",lf_agent_markdown="story-worker",lf_skills="implement-result",lf_limit="3",lf_model_tier="flagship"];
 flagship_result [lf_action="lf_implement_result",lf_outcomes="completed,stopped",lf_source="worker_flagship",lf_limit="3"];
 worker_flagship -> flagship_result [lf_on="success"];
 worker_flagship -> budget [lf_on="failed"];
 flagship_result -> story_done [lf_on="completed"];
 flagship_result -> park_story [lf_on="stopped"];
`, 1)
	return dot
}

func TestImplementRoutingAdmitsExclusiveWorkers(t *testing.T) {
	dot := routedImplementDOT()
	if err := validateImplementDOT(t, dot); err != nil {
		t.Fatal(err)
	}
	p, err := ParseProfile([]byte(dot))
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateControlGraph(p, ImplementControlPolicy()); err != nil {
		t.Fatal(err)
	}
	gate, ok := ImplementWorkerSyncNode(p)
	if !ok || gate.Name != "worker_sync" {
		t.Fatalf("gate=%+v, %v", gate, ok)
	}
	// Node names and configured tier labels are not routing policy in admission.
	dot = strings.ReplaceAll(dot, "worker_flagship", "alternative_worker")
	dot = strings.ReplaceAll(dot, `lf_model_tier="flagship"`, `lf_model_tier="balanced"`)
	if err := validateImplementDOT(t, dot); err != nil {
		t.Fatal(err)
	}
}

func TestImplementRoutingRejectsUnsafeRegions(t *testing.T) {
	for name, change := range map[string][2]string{
		"selector skips work":  {`select_path -> budget [lf_on="worker"]`, `select_path -> worker_done [lf_on="worker"]`},
		"budget bypass":        {`budget -> worker_sync [lf_on="run"]`, `budget -> sizing_valid [lf_on="run"]`},
		"sync bypass":          {`select_path -> budget [lf_on="worker"]`, `select_path -> small [lf_on="worker"]`},
		"worker bypass":        {`select_path -> budget [lf_on="worker"]`, `select_path -> worker_flagship [lf_on="worker"]`},
		"gate bypass":          {`select_path -> budget [lf_on="worker"]`, `select_path -> worker_sync [lf_on="worker"]`},
		"cycle":                {`small -> medium [lf_on="false"]`, `small -> small [lf_on="false"]`},
		"multi node cycle":     {`medium -> worker_flagship [lf_on="false"]`, `medium -> small [lf_on="false"]`},
		"nonfact region":       {`small -> medium [lf_on="false"]`, `small -> budget [lf_on="false"]`},
		"dual worker path":     {`worker_agent -> worker_result [lf_on="success"]`, `worker_agent -> worker_flagship [lf_on="success"]`},
		"mixed results":        {`worker_flagship -> flagship_result [lf_on="success"]`, `worker_flagship -> worker_result [lf_on="success"]`},
		"mixed source":         {`lf_source="worker_flagship"`, `lf_source="worker_agent"`},
		"result bypass":        {`select_path -> budget [lf_on="worker"]`, `select_path -> flagship_result [lf_on="worker"]`},
		"save bypass":          {`select_path -> budget [lf_on="worker"]`, `select_path -> story_done [lf_on="worker"]`},
		"different role":       {`lf_agent_markdown="story-worker",lf_skills=`, `lf_agent_markdown="other-worker",lf_skills=`},
		"different bound":      {`lf_limit="3",lf_model_tier=`, `lf_limit="4",lf_model_tier=`},
		"different permission": {`lf_model_tier="flagship"`, `lf_access="write",lf_model_tier="flagship"`},
		"different budget":     {`lf_model_tier="flagship"`, `lf_budget="60",lf_model_tier="flagship"`},
		"different capacity":   {`lf_model_tier="flagship"`, `lf_slot_budget="2",lf_model_tier="flagship"`},
		"failure bypass":       {`worker_flagship -> budget [lf_on="failed"]`, `worker_flagship -> sizing_valid [lf_on="failed"]`},
		"sync retry bypass":    {`worker_sync -> worker_sync [lf_on="retry"]`, `worker_sync -> sizing_valid [lf_on="retry"]`},
	} {
		t.Run(name, func(t *testing.T) {
			dot := routedImplementDOT()
			broken := strings.Replace(dot, change[0], change[1], 1)
			if dot == broken {
				t.Fatal("mutation changed nothing")
			}
			if err := validateImplementDOT(t, broken); err == nil {
				t.Fatal("unsafe routed graph admitted")
			}
		})
	}
}

func TestImplementFactConditionsAreClosed(t *testing.T) {
	for _, tc := range []struct{ field, value string }{{"story.size", "S"}, {"story.size", "M"}, {"story.size", "L"}, {"story.sizing_valid", "true"}, {"story.sizing_valid", "false"}} {
		attrs := map[string]string{"lf_field": tc.field, "lf_condition": tc.value}
		condition, err := DecodeCondition(ResearchIfAction, attrs, ImplementControlPolicy())
		if err != nil {
			t.Fatal(err)
		}
		if outcome, err := condition.EvaluateImplementFacts(map[string]string{tc.field: tc.value}); err != nil || outcome != "true" {
			t.Fatalf("matching fact = %s, %v", outcome, err)
		}
		if outcome, err := condition.EvaluateImplementFacts(nil); err != nil || outcome != "false" {
			t.Fatalf("missing fact = %s, %v", outcome, err)
		}
		for _, key := range []string{"lf_source", "lf_default"} {
			forbidden := maps.Clone(attrs)
			forbidden[key] = ""
			if _, err := DecodeCondition(ResearchIfAction, forbidden, ImplementControlPolicy()); err == nil {
				t.Fatalf("accepted %s", key)
			}
		}
		if _, err := DecodeCondition(ResearchLoopAction, attrs, ImplementControlPolicy()); err == nil {
			t.Fatal("fact loop admitted")
		}
	}
	for _, tc := range []struct{ field, value string }{{"story.size", "s"}, {"story.size", ""}, {"story.size", "XL"}, {"story.size", "true"}, {"story.sizing_valid", "S"}, {"story.sizing_valid", "TRUE"}, {"story.fallback", "false"}, {"response", "completed"}} {
		attrs := map[string]string{"lf_field": tc.field, "lf_condition": tc.value}
		if _, err := DecodeCondition(ResearchIfAction, attrs, ImplementControlPolicy()); err == nil {
			t.Fatalf("accepted %+v", tc)
		}
	}
	for _, policy := range []ControlPolicy{ResearchControlPolicy(), {Mode: agent.ModeValidate}, {Workflow: agent.WorkflowBug}} {
		if _, err := DecodeCondition(ResearchIfAction, map[string]string{"lf_field": "story.size", "lf_condition": "S"}, policy); err == nil {
			t.Fatalf("cross-mode fact admitted: %s", policy.Mode)
		}
	}
	if _, err := DecodeCondition(ResearchIfAction, map[string]string{"lf_field": "story.fallback", "lf_condition": "false"}, PlanControlPolicy()); err != nil {
		t.Fatal("Plan fallback changed", err)
	}
}
