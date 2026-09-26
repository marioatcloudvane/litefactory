package config

import "testing"

func TestBugRuntimeOptionalRoleConfiguration(t *testing.T) {
	c := Config{Agents: Agents{Codex: &Codex{}}, ToolProfiles: ToolProfiles{Permissions: make(map[string]ToolProfile)}}
	for _, name := range canonicalPermissionProfiles {
		c.ToolProfiles.Permissions[name] = ToolProfile{Codex: &ToolProfileCodex{Sandbox: "read-only"}}
	}
	if errs := validatePermissionProfiles(&c); len(errs) != 0 {
		t.Fatalf("existing config requires Bug roles: %v", errs)
	}
	c.ToolProfiles.Permissions["bug-analyst"] = ToolProfile{Codex: &ToolProfileCodex{Sandbox: "read-only"}}
	c.ToolProfiles.Permissions["bug-fixer"] = ToolProfile{Codex: &ToolProfileCodex{Sandbox: "workspace-write"}}
	if errs := validatePermissionProfiles(&c); len(errs) != 0 {
		t.Fatalf("explicit independent profiles refused: %v", errs)
	}
	c.ToolProfiles.Permissions["bug-analyst"].Codex.Sandbox = "workspace-write"
	if errs := validatePermissionProfiles(&c); len(errs) == 0 {
		t.Fatal("analyst edit grant accepted")
	}
}

func TestBugRuntimeIsolationConfigurationBounds(t *testing.T) {
	if errs := validateBugIsolation(nil); len(errs) != 0 {
		t.Fatal(errs)
	}
	valid := BugCheckIsolation{Binary: "/usr/bin/bwrap", ReadOnlyRoots: []string{"/usr", "/lib"}, PATH: "/usr/bin:/bin", Environment: []string{"TEST_CONFIG"}}
	if errs := validateBugIsolation(&valid); len(errs) != 0 {
		t.Fatal(errs)
	}
	for _, tc := range []struct {
		name   string
		change func(*BugCheckIsolation)
	}{
		{"relative launcher", func(p *BugCheckIsolation) { p.Binary = "bwrap" }},
		{"broad root", func(p *BugCheckIsolation) { p.ReadOnlyRoots = []string{"/"} }},
		{"home root", func(p *BugCheckIsolation) { p.ReadOnlyRoots = []string{"/home"} }},
		{"cwd PATH", func(p *BugCheckIsolation) { p.PATH = ".:/usr/bin" }},
		{"duplicate env", func(p *BugCheckIsolation) { p.Environment = []string{"TEST_CONFIG", "TEST_CONFIG"} }},
		{"inline env", func(p *BugCheckIsolation) { p.Environment = []string{"TEST_CONFIG=value"} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := valid
			tc.change(&p)
			if errs := validateBugIsolation(&p); len(errs) == 0 {
				t.Fatal("unsafe isolation configuration accepted")
			}
		})
	}
}
