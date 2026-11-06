package store

import "testing"

func TestLinearConnectionLabelMappings(t *testing.T) {
	db := newTestDB(t)
	repo := testRepo(t, db, "code", "repo")
	base := linearConnectionFixture(repo)

	empty := base
	empty.Linear.ModeLabelsJSON = ""
	saved, err := db.SetConnection(t.Context(), empty, 0)
	if err != nil {
		t.Fatalf("empty mode map refused: %v", err)
	}
	got, err := db.GetConnectionVersion(t.Context(), saved.ID)
	if err != nil {
		t.Fatal(err)
	}
	modes, merr := got.Linear.ModeLabels()
	agents, aerr := got.Linear.CodingAgentLabels()
	bug, berr := got.Linear.BugLabel()
	if merr != nil || aerr != nil || berr != nil || len(modes) != 0 || len(agents) != 0 || bug != "" {
		t.Fatalf("absent mappings must decode empty: %v %v %v / %v %v %q", merr, aerr, berr, modes, agents, bug)
	}

	for name, mutate := range map[string]func(*LinearConfiguration){
		"agent reuses mode label": func(c *LinearConfiguration) { c.CodingAgentLabelsJSON = `{"codex":"label-plan"}` },
		"bug reuses mode label":   func(c *LinearConfiguration) { c.BugLabelID = "label-implement" },
		"bug reuses agent label": func(c *LinearConfiguration) {
			c.CodingAgentLabelsJSON, c.BugLabelID = `{"codex":"label-x"}`, "label-x"
		},
		"two agents share a label": func(c *LinearConfiguration) { c.CodingAgentLabelsJSON = `{"a":"label-x","b":"label-x"}` },
		"empty agent key":          func(c *LinearConfiguration) { c.CodingAgentLabelsJSON = `{"":"label-x"}` },
		"agent key with colon":     func(c *LinearConfiguration) { c.CodingAgentLabelsJSON = `{"a:b":"label-x"}` },
		"agent key with newline":   func(c *LinearConfiguration) { c.CodingAgentLabelsJSON = "{\"a\\nb\":\"label-x\"}" },
		"bug id with whitespace":   func(c *LinearConfiguration) { c.BugLabelID = " label-bug" },
	} {
		v := base
		mutate(&v.Linear)
		if _, err := db.SetConnection(t.Context(), v, saved.Revision); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}

	ok := base
	ok.Linear.CodingAgentLabelsJSON, ok.Linear.BugLabelID = `{"codex":"label-codex","claude":"label-claude"}`, "label-bug"
	if _, err := db.SetConnection(t.Context(), ok, saved.Revision); err != nil {
		t.Fatalf("distinct agent and bug labels refused: %v", err)
	}
}
