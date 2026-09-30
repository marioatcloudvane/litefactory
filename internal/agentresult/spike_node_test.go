package agentresult

import (
	"strings"
	"testing"
)

func spikeContext(questions bool) Context {
	return Context{SchemaVersion: SchemaVersion, Mode: "spike", WorkflowVersion: 2, QuestionsAllowed: questions}
}

const spikeAskEnvelope = `{"version":1,"response":"ask","result":{"outcome":"incomplete","reason":"Need a decision","comment":"Blocked.","questions":[{"id":"q1","audience":"technical","text":"Which one?"}],"payload":{"spike":{"request":"r"}}}}`

const spikeDoneEnvelope = `{"version":1,"response":"done","result":{"outcome":"completed","comment":"Answered.","payload":{"spike":{"request":"r","findings":["f"],"proposed_fix":{"defects":["d"],"files":["a.go","a_test.go"],"change_kinds":["code","test"],"regression_test":"TestA"}}}}}`

func TestDecodeSpikeNodeAcceptsAskAndDoneOnly(t *testing.T) {
	if _, d := DecodeSpikeNode([]byte(spikeDoneEnvelope), spikeContext(true)); !d.Valid || !d.Complete {
		t.Fatalf("done refused: %+v", d)
	}
	if node, d := DecodeSpikeNode([]byte(spikeAskEnvelope), spikeContext(true)); !d.Valid || node.Response != "ask" {
		t.Fatalf("ask refused: %+v", d)
	}
	approved := strings.Replace(spikeDoneEnvelope, `"response":"done"`, `"response":"approved"`, 1)
	if _, d := DecodeSpikeNode([]byte(approved), spikeContext(true)); d.Valid {
		t.Fatal("Spike has no approval: approved was accepted")
	}
	unknown := strings.Replace(spikeDoneEnvelope, `"response":"done"`, `"response":"later"`, 1)
	if _, d := DecodeSpikeNode([]byte(unknown), spikeContext(true)); d.Valid {
		t.Fatal("an unknown response was accepted")
	}
	inconsistent := strings.Replace(spikeAskEnvelope, `"response":"ask"`, `"response":"done"`, 1)
	if _, d := DecodeSpikeNode([]byte(inconsistent), spikeContext(true)); d.Valid {
		t.Fatal("done with questions was accepted")
	}
	for _, mode := range []string{"research", "plan", "implement", ""} {
		ctx := spikeContext(true)
		ctx.Mode = mode
		if _, d := DecodeSpikeNode([]byte(spikeDoneEnvelope), ctx); d.Valid {
			t.Fatalf("mode %q decoded as Spike", mode)
		}
	}
}

func TestSpikeDecoderLeavesResearchAndPlanVocabularyUnchanged(t *testing.T) {
	research := `{"version":1,"response":"approved","result":{"outcome":"completed","comment":"ok","payload":{"research":{"request":"r"}}}}`
	if _, d := DecodeResearchNode([]byte(research), Context{SchemaVersion: SchemaVersion, Mode: "research", WorkflowVersion: 2}); !d.Valid {
		t.Fatalf("Research approved regressed: %+v", d)
	}
	plan := strings.Replace(research, `"research":{"request":"r"}`, `"plan":{}`, 1)
	_, d := DecodePlanNode([]byte(plan), Context{SchemaVersion: SchemaVersion, Mode: "plan", WorkflowVersion: 2})
	if d.Valid || !strings.Contains(diagnosticText(d), "Plan roles return ask") {
		t.Fatalf("Plan approved message regressed: %+v", d)
	}
	if _, d := DecodeResearchNode([]byte(spikeDoneEnvelope), spikeContext(true)); d.Valid {
		t.Fatal("a Spike invocation decoded as Research")
	}
}

func diagnosticText(d Diagnostics) string {
	var parts []string
	for _, v := range d.Diagnostics {
		parts = append(parts, v.Message)
	}
	return strings.Join(parts, " ")
}

func TestSpikeQuestionsFollowTrustedPermissionOnly(t *testing.T) {
	var node ResearchNode
	if d := Decode([]byte(spikeAskEnvelope), &node); !d.Valid {
		t.Fatal(d)
	}
	if d := ValidateResult(node.Result, spikeContext(true)); !d.Valid {
		t.Fatalf("graph Spike question refused: %+v", d)
	}
	notAllowed := spikeContext(false)
	if d := ValidateResult(node.Result, notAllowed); d.Valid {
		t.Fatal("a question was accepted where the owner forbade asking")
	}
	// Permission is factory input: an authored result cannot supply it.
	if _, d := DecodeSpikeNode([]byte(strings.Replace(spikeAskEnvelope, `"payload"`, `"graph_questions":true,"payload"`, 1)), spikeContext(true)); d.Valid {
		t.Fatal("an authored marker was accepted")
	}
	other := spikeContext(true)
	other.Mode = "implement"
	if d := ValidateResult(node.Result, other); d.Valid {
		t.Fatal("permission widened another mode")
	}
}

func TestSpikeProposedFixIsValidatedForShapeOnly(t *testing.T) {
	base := func(fix string) Result {
		var node ResearchNode
		envelope := `{"version":1,"response":"done","result":{"outcome":"completed","comment":"c","payload":{"spike":{"request":"r","proposed_fix":` + fix + `}}}}`
		if d := Decode([]byte(envelope), &node); !d.Valid {
			t.Fatalf("decode %s: %+v", fix, d)
		}
		return node.Result
	}
	valid := `{"defects":["d"],"files":["a.go"],"change_kinds":["code"],"regression_test":"T"}`
	if d := ValidateResult(base(valid), spikeContext(true)); !d.Valid {
		t.Fatalf("valid proposal refused: %+v", d)
	}
	// Size is the graph's judgement, never this package's.
	large := `{"defects":["a","b","c"],"files":["1.go","2.go","3.go","4.go","5.go","6.go"],"change_kinds":["code","dependency","schema"],"regression_test":""}`
	if d := ValidateResult(base(large), spikeContext(true)); !d.Valid {
		t.Fatalf("a large proposal must be validated by shape only: %+v", d)
	}
	for name, fix := range map[string]string{
		"blank defect":   `{"defects":[" "],"files":["a.go"],"change_kinds":["code"],"regression_test":"T"}`,
		"absolute file":  `{"defects":["d"],"files":["/etc/passwd"],"change_kinds":["code"],"regression_test":"T"}`,
		"escaping file":  `{"defects":["d"],"files":["../x.go"],"change_kinds":["code"],"regression_test":"T"}`,
		"duplicate file": `{"defects":["d"],"files":["a.go","a.go"],"change_kinds":["code"],"regression_test":"T"}`,
		"unknown kind":   `{"defects":["d"],"files":["a.go"],"change_kinds":["magic"],"regression_test":"T"}`,
	} {
		if d := ValidateResult(base(fix), spikeContext(true)); d.Valid {
			t.Errorf("%s accepted", name)
		}
	}
	var node ResearchNode
	withStatus := `{"version":1,"response":"done","result":{"outcome":"completed","comment":"c","payload":{"spike":{"request":"r","status":"answered"}}}}`
	if d := Decode([]byte(withStatus), &node); !d.Valid {
		t.Fatal(d)
	}
	if d := ValidateResult(node.Result, spikeContext(true)); d.Valid {
		t.Fatal("an authored status was accepted")
	}
}
