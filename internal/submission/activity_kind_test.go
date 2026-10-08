package submission_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/cloudvane/litefactory/internal/submission"
)

func TestActivityCatalogOptionalKindEnum(t *testing.T) {
	t.Parallel()
	profile, err := submission.NewFamilyToolProfile("implement", []string{"activity_update"})
	if err != nil {
		t.Fatal(err)
	}
	for _, spec := range profile.List() {
		if spec.Name != "activity_update" {
			continue
		}
		var schema struct {
			Properties map[string]struct {
				Type string   `json:"type"`
				Enum []string `json:"enum"`
			} `json:"properties"`
			Required             []string `json:"required"`
			AdditionalProperties bool     `json:"additionalProperties"`
		}
		if err := json.Unmarshal(spec.InputSchema, &schema); err != nil {
			t.Fatal(err)
		}
		if schema.Properties["kind"].Type != "string" || !reflect.DeepEqual(schema.Properties["kind"].Enum, []string{"implementation", "testing", "repair"}) || !reflect.DeepEqual(schema.Required, []string{"sequence", "message"}) || schema.AdditionalProperties {
			t.Fatalf("kind contract %+v", schema)
		}
		return
	}
	t.Fatal("activity tool missing")
}
