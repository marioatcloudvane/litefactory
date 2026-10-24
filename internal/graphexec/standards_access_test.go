package graphexec

import "testing"

func TestStandardsGrantMustNameARoleTheNodeRuns(t *testing.T) {
	for name, test := range map[string]struct {
		attrs map[string]string
		ok    bool
	}{
		"absent":           {map[string]string{"lf_agent_markdown": "story-worker"}, true},
		"own agent":        {map[string]string{"lf_agent_markdown": "story-worker", StandardsAttribute: "story-worker"}, true},
		"one of the roles": {map[string]string{"lf_roles": "validate-code,validate-architecture", StandardsAttribute: "validate-architecture"}, true},
		"foreign role":     {map[string]string{"lf_agent_markdown": "bug-fixer", StandardsAttribute: "story-worker"}, false},
		"empty":            {map[string]string{"lf_agent_markdown": "story-worker", StandardsAttribute: ""}, false},
		"repeated":         {map[string]string{"lf_roles": "validate-code,validate-architecture", StandardsAttribute: "validate-code,validate-code"}, false},
	} {
		err := validateStandardsAttr(Node{Name: "n", Attrs: test.attrs})
		if (err == nil) != test.ok {
			t.Fatalf("%s: err=%v", name, err)
		}
	}
	attrs := map[string]string{"lf_roles": "validate-code,validate-architecture", StandardsAttribute: "validate-architecture"}
	if StandardsRole(attrs, "validate-architecture") != "validate-architecture" || StandardsRole(attrs, "validate-code") != "" || StandardsRole(map[string]string{}, "story-worker") != "" {
		t.Fatal("grant does not follow the declaration")
	}
}
