package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func selectedDelivery(t *testing.T) Delivery {
	t.Helper()
	d := Delivery{Root: t.TempDir(), Identity: "captured", MainRole: "worker", MainModel: "model", Roles: []string{"worker"}, Skills: []string{"plugin/skills/proof/SKILL.md"}, RequiredSkills: []string{"plugin/skills/proof/SKILL.md"}, Evidence: DeliveryEvidence{Specialists: []SpecialistEvidence{{ID: "worker", Model: "model"}}}, RenderedFiles: map[string]string{}}
	for _, name := range []string{"bootstrap.md", "plugin/.claude-plugin/plugin.json", "plugin/agents/worker.md", "plugin/skills/proof/SKILL.md", "plugin/skills/proof/resource.md"} {
		content := []byte(name)
		sum := sha256.Sum256(content)
		d.RenderedFiles[name] = hex.EncodeToString(sum[:])
		file := filepath.Join(d.Root, name)
		if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, content, 0600); err != nil {
			t.Fatal(err)
		}
	}
	return d
}

func TestGraphDeliverySelectionRefusal(t *testing.T) {
	tests := []struct {
		name   string
		change func(*Delivery)
	}{
		{"missing", func(d *Delivery) { *d = Delivery{} }},
		{"role", func(d *Delivery) { d.MainRole = "other" }},
		{"model", func(d *Delivery) { d.MainModel = "other" }},
		{"required skill", func(d *Delivery) { d.Skills = nil }},
		{"selected skill", func(d *Delivery) { d.RequiredSkills = nil }},
		{"duplicate skill", func(d *Delivery) {
			d.Skills = append(d.Skills, d.Skills[0])
			d.RequiredSkills = append(d.RequiredSkills, d.RequiredSkills[0])
		}},
		{"manifest", func(d *Delivery) { d.RenderedFiles = nil }},
		{"main role", func(d *Delivery) { delete(d.RenderedFiles, "plugin/agents/worker.md") }},
		{"bootstrap", func(d *Delivery) { delete(d.RenderedFiles, "bootstrap.md") }},
		{"skill source", func(d *Delivery) { delete(d.RenderedFiles, d.Skills[0]) }},
		{"unsafe", func(d *Delivery) { d.RenderedFiles["../outside"] = strings.Repeat("a", 64) }},
		{"extra role", func(d *Delivery) { d.RenderedFiles["plugin/agents/ambient.md"] = strings.Repeat("a", 64) }},
		{"extra skill", func(d *Delivery) { d.RenderedFiles["plugin/skills/ambient/SKILL.md"] = strings.Repeat("a", 64) }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := selectedDelivery(t)
			tc.change(&d)
			req := Request{Invocation: InvocationGraphNode, Mode: "implement", Graph: &GraphInvocation{InstanceID: 1, AttemptID: 2, RunID: 3, Node: "build"}, Delivery: &d}
			if tc.name == "missing" {
				req.Delivery = nil
			}
			for _, err := range []error{req.Validate(), VerifyRequestDelivery(context.Background(), req)} {
				if !errors.Is(err, ErrDeliveryRefused) {
					t.Fatalf("not a delivery refusal: %v", err)
				}
				for _, want := range []string{"implement", "build", "installed-workflow"} {
					if !strings.Contains(err.Error(), want) {
						t.Fatalf("missing %q: %v", want, err)
					}
				}
				if errors.Is(err, ErrSessionNotResumable) {
					t.Fatal("refusal eligible for retry")
				}
			}
		})
	}
}

func TestGraphDeliverySelectedResourcesAndNoSkills(t *testing.T) {
	for _, skills := range []bool{true, false} {
		t.Run(map[bool]string{true: "skills", false: "no skills"}[skills], func(t *testing.T) {
			d := selectedDelivery(t)
			if !skills {
				d.Skills = nil
				d.RequiredSkills = nil
				for _, name := range []string{"plugin/skills/proof/SKILL.md", "plugin/skills/proof/resource.md"} {
					delete(d.RenderedFiles, name)
					if err := os.Remove(filepath.Join(d.Root, name)); err != nil {
						t.Fatal(err)
					}
				}
				if err := os.RemoveAll(filepath.Join(d.Root, "plugin/skills")); err != nil {
					t.Fatal(err)
				}
			}
			req := Request{Invocation: InvocationGraphNode, Mode: "implement", Graph: &GraphInvocation{InstanceID: 1, AttemptID: 2, RunID: 3, Node: "build"}, Delivery: &d}
			if err := VerifyRequestDelivery(context.Background(), req); err != nil {
				t.Fatal(err)
			}
			clone := d.Clone()
			if skills {
				d.RequiredSkills[0] = "changed"
				d.Skills[0] = "changed"
				if clone.RequiredSkills[0] == "changed" || clone.Skills[0] == "changed" {
					t.Fatal("selection aliases source")
				}
			}
			d.RenderedFiles["bootstrap.md"] = "changed"
			if clone.RenderedFiles["bootstrap.md"] == "changed" {
				t.Fatal("manifest aliases source")
			}
		})
	}
}
