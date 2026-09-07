package team

import (
	"encoding/json"
	"strings"
	"testing"
)

const validIndex = `{"schema_version":2,"shared":{"reader":{"desc":"Read","path":"agents/shared/reader.md","tier":"balanced"}},"teams":{"go-team":{"desc":"Go","research":[{"ref":"reader"}],"plan":[],"implement":[],"validate":[],"spike":[]}}}`

func TestModeRegistryStrictCutover(t *testing.T) {
	tests := []struct{ name, document, want string }{
		{"legacy", `{"schemaVersion":1,"teams":[],"shared":{}}`, ""},
		{"version", strings.Replace(validIndex, `"schema_version":2`, `"schema_version":1`, 1), "unsupported"},
		{"duplicate", strings.Replace(validIndex, `"schema_version":2`, `"schema_version":2,"schema_version":2`, 1), "schema_version"},
		{"unknown", strings.Replace(validIndex, `"tier":"balanced"`, `"tier":"balanced","typo":true`, 1), "typo"},
		{"missing-mode", strings.Replace(validIndex, `,"spike":[]`, "", 1), "spike"},
		{"null-mode", strings.Replace(validIndex, `"spike":[]`, `"spike":null`, 1), "spike"},
		{"missing-ref", strings.Replace(validIndex, `"ref":"reader"`, `"ref":"absent"`, 1), "unknown shared"},
		{"mixed", strings.Replace(validIndex, `"ref":"reader"`, `"ref":"reader","id":"reader"`, 1), "ref"},
		{"duplicate-effective", strings.Replace(validIndex, `[{"ref":"reader"}]`, `[{"ref":"reader"},{"id":"reader","desc":"Other","path":"other.md","tier":"fast"}]`, 1), "reader"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Decode([]byte(tt.document))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Decode = %v; want error containing %q", err, tt.want)
			}
		})
	}
}
func TestModeRosterExactAndIndependent(t *testing.T) {
	idx, err := Decode([]byte(validIndex))
	if err != nil {
		t.Fatal(err)
	}
	idx.Shared["unused"] = AgentEntry{Desc: "Unused", Path: "unused.md", Tier: "balanced"}
	a := idx.Shared["reader"]
	a.Skills = []string{"read"}
	idx.Shared["reader"] = a
	got, err := idx.Roster("go-team", "research")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "reader" {
		t.Fatalf("roster=%+v", got)
	}
	got[0].Skills[0] = "mutated"
	if idx.Shared["reader"].Skills[0] != "read" {
		t.Fatal("roster aliases index skills")
	}
	empty, err := idx.Roster("go-team", "spike")
	if err != nil || len(empty) != 0 {
		t.Fatalf("empty=%v, err=%v", empty, err)
	}
}
func TestIndexRoundTrip(t *testing.T) {
	idx, err := Decode([]byte(validIndex))
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(idx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Decode(b); err != nil {
		t.Fatal(err)
	}
}

func TestInlineIdentityAgreesAcrossModes(t *testing.T) {
	for _, field := range []string{"identical", "desc", "path", "tier", "skills", "skills-order"} {
		t.Run(field, func(t *testing.T) {
			idx, err := Decode([]byte(validIndex))
			if err != nil {
				t.Fatal(err)
			}
			entry := AgentEntry{ID: "writer", Desc: "Write", Path: "agents/go-team/writer.md", Tier: "balanced", Skills: []string{"first", "second"}}
			other := entry
			switch field {
			case "desc":
				other.Desc = "Other"
			case "path":
				other.Path = "other.md"
			case "tier":
				other.Tier = "fast"
			case "skills":
				other.Skills = []string{"first"}
			case "skills-order":
				other.Skills = []string{"second", "first"}
			}
			team := idx.Teams["go-team"]
			team.Plan = []AgentEntry{entry}
			team.Implement = []AgentEntry{other}
			idx.Teams["go-team"] = team
			data, err := json.Marshal(idx)
			if err != nil {
				t.Fatal(err)
			}
			_, err = Decode(data)
			if field == "identical" {
				if err != nil {
					t.Fatalf("identical reuse rejected: %v", err)
				}
			} else if err == nil || !strings.Contains(err.Error(), "conflicts") {
				t.Fatalf("conflicting %s accepted: %v", field, err)
			}
		})
	}
}
