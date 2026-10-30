package owneraction

import "testing"

func TestActionRegistryCoversEnabledMutationCatalogue(t *testing.T) {
	cases := []struct{ command, action, tier, scope string }{
		{"draft", "", Conversational, "repository:actions"},
		{"diagnose", "", Conversational, "repository:actions"},
		{"answer", "", Conversational, "repository:actions"},
		{"confirm", "", Binding, "repository:actions"},
		{"standards-analysis", "start", Conversational, "repository:actions"},
		{"standards-author", "start", Conversational, "standards:management"},
		{"standards-author", "answer", Conversational, "standards:management"},
		{"standards-author", "revise", Conversational, "standards:management"},
		{"standards-author", "edit", Conversational, "standards:management"},
		{"standards-author", "continue", Conversational, "standards:management"},
		{"standards-author", "validate", Conversational, "standards:management"},
		{"standards-author", "complete", Conversational, "standards:management"},
		{"standards-author", "rebase", Conversational, "standards:management"},
		{"standards-author", "publish", Binding, "standards:management"},
		{"standards-proposal", "edit", Conversational, "standards:management"},
		{"standards-proposal", "approve", Binding, "standards:management"},
		{"standards-proposal", "publish", Binding, "standards:management"},
		{"standards-exception", "propose", Conversational, "repository:actions"},
		{"standards-exception", "approve", Binding, "repository:actions"},
		{"standards-exception", "revoke", Binding, "repository:actions"},
	}
	for _, tc := range cases {
		t.Run(tc.command+"/"+tc.action, func(t *testing.T) {
			tier, scope, err := Classify(Invocation{Command: tc.command, Arguments: map[string]string{"action": tc.action}})
			if err != nil || tier != tc.tier || scope != tc.scope {
				t.Fatalf("classification: %q %q %v", tier, scope, err)
			}
		})
	}
	for _, in := range []Invocation{{Command: "ship"}, {Command: "merge"}, {Command: "task-pause"}, {Command: "standards-author", Arguments: map[string]string{"action": "show"}}, {Command: "standards-author", Arguments: map[string]string{"action": "delete"}}} {
		if _, _, err := Classify(in); err == nil {
			t.Fatalf("unclassified operation accepted: %+v", in)
		}
	}
}
