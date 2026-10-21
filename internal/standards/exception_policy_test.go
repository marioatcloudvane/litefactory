package standards

import (
	"encoding/json"
	"testing"
)

func TestExceptionPolicyCanonicalAndLegacyDigests(t *testing.T) {
	legacy := Standard{ID: "s", Title: "S", Body: "Body", CategoryIDs: []string{"c"}}
	b, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"id":"s","title":"S","body":"Body","category_ids":["c"]}` {
		t.Fatalf("legacy bytes changed: %s", b)
	}
	a := &ExceptionPolicy{Version: 1, Mode: "allow-task", RepositoryIDs: []int64{2, 1}, TaskRefs: []string{"github:o/r#2", "github:o/r#1"}, MaxLifetimeSeconds: 60}
	bpol := &ExceptionPolicy{Version: 1, Mode: "allow-task", RepositoryIDs: []int64{1, 2}, TaskRefs: []string{"github:o/r#1", "github:o/r#2"}, MaxLifetimeSeconds: 60}
	if PolicyDigest(a) != PolicyDigest(bpol) {
		t.Fatal("ordering changes policy digest")
	}
	ops := canonicalOps([]Operation{{Kind: "standard.add", Standard: &Standard{ID: "s", ExceptionPolicy: a}}})
	a.RepositoryIDs[0] = 99
	a.TaskRefs[0] = "changed"
	if ops[0].Standard.ExceptionPolicy.RepositoryIDs[1] != 2 || ops[0].Standard.ExceptionPolicy.TaskRefs[1] != "github:o/r#2" {
		t.Fatal("canonical terms alias caller policy")
	}
	bpol.MaxLifetimeSeconds++
	if PolicyDigest(ops[0].Standard.ExceptionPolicy) == PolicyDigest(bpol) {
		t.Fatal("lifetime omitted from digest")
	}
}

func TestExceptionPolicyValidation(t *testing.T) {
	for _, tc := range []struct {
		name  string
		p     *ExceptionPolicy
		valid bool
	}{
		{"absent", nil, true},
		{"deny", &ExceptionPolicy{Version: 1, Mode: "deny"}, true},
		{"all", &ExceptionPolicy{Version: 1, Mode: "allow-task", AllRegisteredRepositories: true, MaxLifetimeSeconds: 60}, true},
		{"exact", &ExceptionPolicy{Version: 1, Mode: "allow-task", RepositoryIDs: []int64{1}, TaskRefs: []string{"github:o/r#1"}, MaxLifetimeSeconds: 60}, true},
		{"unsupported", &ExceptionPolicy{Version: 2, Mode: "deny"}, false},
		{"deny terms", &ExceptionPolicy{Version: 1, Mode: "deny", MaxLifetimeSeconds: 60}, false},
		{"missing domain", &ExceptionPolicy{Version: 1, Mode: "allow-task", MaxLifetimeSeconds: 60}, false},
		{"ambiguous", &ExceptionPolicy{Version: 1, Mode: "allow-task", AllRegisteredRepositories: true, RepositoryIDs: []int64{1}, MaxLifetimeSeconds: 60}, false},
		{"missing lifetime", &ExceptionPolicy{Version: 1, Mode: "allow-task", AllRegisteredRepositories: true}, false},
		{"duplicate repository", &ExceptionPolicy{Version: 1, Mode: "allow-task", RepositoryIDs: []int64{1, 1}, MaxLifetimeSeconds: 60}, false},
		{"wildcard task", &ExceptionPolicy{Version: 1, Mode: "allow-task", AllRegisteredRepositories: true, TaskRefs: []string{"github:o/r#*"}, MaxLifetimeSeconds: 60}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n := 0
			validatePolicy(tc.p, "policy", func(string, string) { n++ })
			if (n == 0) != tc.valid {
				t.Fatalf("problems=%d valid=%t", n, tc.valid)
			}
		})
	}
}
