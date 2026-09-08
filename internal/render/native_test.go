package render_test

import (
	"bytes"
	"github.com/cloudvane/litefactory/internal/render"
	"strings"
	"testing"
)

func TestNativeRoleUsesLocalModelAndFileBackedBody(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"claude", "codex"} {
		t.Run(kind, func(t *testing.T) {
			role := render.NativeRole{Source: render.ResolvedSource{Frontmatter: render.Frontmatter{Name: "reviewer", Description: "Review Go", Model: "untrusted-model"}, Body: "SPECIALIST_BODY"}, Model: "operator-model", Tools: []string{"Read"}}
			got, err := render.Native(kind, role, "read-only")
			if err != nil {
				t.Fatal(err)
			}
			again, err := render.Native(kind, role, "read-only")
			if err != nil {
				t.Fatal(err)
			}
			if got.Path == "" || !bytes.Equal(got.Bytes, again.Bytes) {
				t.Fatal("native rendering must be deterministic and file backed")
			}
			for _, want := range []string{"operator-model", "SPECIALIST_BODY"} {
				if !strings.Contains(string(got.Bytes), want) {
					t.Errorf("missing %s: %s", want, got.Bytes)
				}
			}
			if strings.Contains(string(got.Bytes), "untrusted-model") {
				t.Error("source model overrides local tier resolution")
			}
		})
	}
}

func TestNativeRoleRefusesUnsafeNameAndUnknownHarness(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, kind string }{{"../escape", "claude"}, {"../escape", "codex"}, {"valid", "unknown"}} {
		t.Run(tc.kind+tc.name, func(t *testing.T) {
			_, err := render.Native(tc.kind, render.NativeRole{Source: render.ResolvedSource{Frontmatter: render.Frontmatter{Name: tc.name, Description: "test"}}, Model: "model"}, "read-only")
			if err == nil {
				t.Fatal("unsafe native role accepted")
			}
		})
	}
}
