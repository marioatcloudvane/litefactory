package teamsource

import (
	"reflect"
	"testing"
)

func TestTaskOwnersComeOnlyFromRetainedExplicitDeclarations(t *testing.T) {
	d := &WorkflowDefinition{Files: map[string][]byte{
		"skills/owners/implementation-engineer.md": []byte("---\nname: implementation-engineer\ndescription: Build the story\ntask_owner: true\n---\nAdvice."),
		"skills/other/implementation-engineer.md":  []byte("---\nname: implementation-engineer\ndescription: Duplicate retained guidance\ntask_owner: true\n---\nAdvice."),
		"roles/unit-test-engineer.md":              []byte("---\nname: unit-test-engineer\ndescription: Write unit tests\ntask_owner: true\n---\nAdvice."),
		"roles/foreign.md":                         []byte("# A filename cannot authorize an owner"),
		"roles/observer.md":                        []byte("---\nname: observer\ndescription: Observe\ntask_owner: false\n---\nAdvice."),
	}}
	got, err := TaskOwnerRoles(d)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"implementation-engineer", "unit-test-engineer"}) {
		t.Fatal("wrong retained owner scope", got)
	}
}
func TestTaskOwnersRefuseInvalidRetainedDeclarations(t *testing.T) {
	for _, raw := range []string{
		"---\nname: implementation-engineer\ndescription: Advice\ntask_owner: true\n",
		"---\nname: foreign\ndescription: Advice\ntask_owner: true\n---\n",
		"---\nname: implementation-engineer\ndescription: ''\ntask_owner: true\n---\n",
		"---\nname: implementation-engineer\ndescription: Advice\ntask_owner: yes-invalid\n---\n",
		"---\nname: implementation-engineer\ndescription: Advice\ntask_owner: true\nextra: forbidden\n---\n",
	} {
		if _, err := TaskOwnerRoles(&WorkflowDefinition{Files: map[string][]byte{"roles/implementation-engineer.md": []byte(raw)}}); err == nil {
			t.Fatal("invalid retained owner admitted")
		}
	}
	if _, err := TaskOwnerRoles(nil); err == nil {
		t.Fatal("absent admission authorized owners")
	}
}
