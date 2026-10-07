package submission_test

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/submission"
)

func prepareSpec(t *testing.T, p submission.ToolProfile) (submission.ToolSpec, bool) {
	t.Helper()
	for _, spec := range p.List() {
		if spec.Name == "issues_create_prepare" {
			return spec, true
		}
	}
	return submission.ToolSpec{}, false
}

func TestCreationPrepareCatalog_ModeAndFlagGating(t *testing.T) {
	t.Parallel()
	if submission.FamilyCatalogVersion != "families-v4" {
		t.Fatalf("catalog version %q", submission.FamilyCatalogVersion)
	}
	for _, mode := range []string{"research", "plan", "implement", "validate", "spike"} {
		for _, flag := range []bool{false, true} {
			p, err := submission.NewFamilyToolProfile(mode, flag, []string{"issues_create_prepare"})
			if err != nil {
				t.Fatal(err)
			}
			_, listed := prepareSpec(t, p)
			want := mode == "research"
			if listed != want || p.Allows("issues_create_prepare") != want {
				t.Errorf("%s flag=%t listed=%t allowed=%t want=%t", mode, flag, listed, p.Allows("issues_create_prepare"), want)
			}
			// Installing the drafting tool never installs or enables publication.
			if p.Allows("issues_create_batch") {
				t.Errorf("%s flag=%t: drafting installed the batch tool", mode, flag)
			}
		}
	}
}

func TestCreationPrepareCatalog_NotInstalledIsAbsent(t *testing.T) {
	t.Parallel()
	p, err := submission.NewFamilyToolProfile("research", true, []string{"issues_list"})
	if err != nil {
		t.Fatal(err)
	}
	if _, listed := prepareSpec(t, p); listed || p.Allows("issues_create_prepare") {
		t.Fatal("uninstalled drafting tool is visible")
	}
}

func TestCreationPrepareCatalog_SpecAndSchema(t *testing.T) {
	t.Parallel()
	p, err := submission.NewFamilyToolProfile("research", false, []string{"issues_create_prepare"})
	if err != nil {
		t.Fatal(err)
	}
	spec, ok := prepareSpec(t, p)
	if !ok {
		t.Fatal("tool missing")
	}
	if spec.Effect != "input" {
		t.Errorf("effect %q", spec.Effect)
	}
	for _, want := range []string{"split", "advice only", "publishes nothing", "needs no approval"} {
		if !strings.Contains(spec.Description, want) {
			t.Errorf("description lacks %q: %s", want, spec.Description)
		}
	}
	if strings.Contains(spec.Description, "/mcp-create") {
		t.Error("description carries an approval command")
	}
	var schema struct {
		AdditionalProperties *bool    `json:"additionalProperties"`
		Required             []string `json:"required"`
		Properties           map[string]struct {
			Enum     []string `json:"enum"`
			MinItems int      `json:"minItems"`
			MaxItems int      `json:"maxItems"`
			Items    struct {
				AdditionalProperties *bool                      `json:"additionalProperties"`
				Required             []string                   `json:"required"`
				Properties           map[string]json.RawMessage `json:"properties"`
			} `json:"items"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(spec.InputSchema, &schema); err != nil {
		t.Fatal(err)
	}
	if schema.AdditionalProperties == nil || *schema.AdditionalProperties || !slices.Equal(schema.Required, []string{"drafts"}) {
		t.Fatalf("schema is not strict: %s", spec.InputSchema)
	}
	for _, factoryField := range []string{"repository", "source_task", "version"} {
		if _, ok := schema.Properties[factoryField]; ok {
			t.Errorf("schema offers factory field %s", factoryField)
		}
	}
	if !slices.Equal(schema.Properties["relationship"].Enum, []string{"linked", "attach"}) {
		t.Errorf("relationship enum %v", schema.Properties["relationship"].Enum)
	}
	drafts := schema.Properties["drafts"]
	if drafts.MinItems != 1 || drafts.MaxItems != 5 || drafts.Items.AdditionalProperties == nil || *drafts.Items.AdditionalProperties || !slices.Equal(drafts.Items.Required, []string{"title", "body"}) {
		t.Errorf("drafts schema: %+v", drafts)
	}
	for _, field := range []string{"title", "body", "parent_issue", "link_issues"} {
		if _, ok := drafts.Items.Properties[field]; !ok {
			t.Errorf("draft lacks %s", field)
		}
	}
}
