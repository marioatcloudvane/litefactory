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
	c.Agents.Claude = &Claude{}
	c.ToolProfiles.Permissions["bug-fixer"] = ToolProfile{Codex: &ToolProfileCodex{Sandbox: "workspace-write"}, Claude: &ToolProfileClaude{Tools: []string{"Read", "Grep", "Edit", "Write"}}}
	if errs := validateBugRoleProfile(&c, "bug-fixer", c.ToolProfiles.Permissions["bug-fixer"]); len(errs) != 0 {
		t.Fatalf("Claude fixer edit surface refused: %v", errs)
	}
	shell := ToolProfile{Claude: &ToolProfileClaude{Tools: []string{"Read", "Edit", "Bash", "Task"}, AllowedTools: []string{"Read", "Edit", "Bash(go test:*)"}}}
	if errs := validateBugRoleProfile(&c, "bug-fixer", shell); len(errs) != 0 {
		t.Fatalf("Claude fixer implement surface refused: %v", errs)
	}
	analystShell := ToolProfile{Claude: &ToolProfileClaude{Tools: []string{"Read", "Bash"}}}
	if errs := validateBugRoleProfile(&c, "bug-analyst", analystShell); len(errs) == 0 {
		t.Fatal("Claude analyst shell accepted")
	}
	spikeShell := ToolProfile{Claude: &ToolProfileClaude{Tools: []string{"Read", "Edit", "Bash"}}}
	if errs := validateBugRoleProfile(&c, SpikeFixerProfile, spikeShell); len(errs) == 0 {
		t.Fatal("Claude Spike fixer shell accepted")
	}
	c.Agents.Claude = nil
	c.ToolProfiles.Permissions["bug-fixer"] = ToolProfile{Codex: &ToolProfileCodex{Sandbox: "workspace-write"}}
	c.ToolProfiles.Permissions["bug-analyst"].Codex.Sandbox = "workspace-write"
	if errs := validatePermissionProfiles(&c); len(errs) == 0 {
		t.Fatal("analyst edit grant accepted")
	}
}

func TestBugRuntimeIsolationConfigurationBounds(t *testing.T) {
	if errs := validateBugIsolation(nil); len(errs) != 0 {
		t.Fatal(errs)
	}
	valid := BugCheckIsolation{Binary: "/opt/homebrew/bin/codex", WritableRoots: []string{"/Users/op/Library/Caches/go-build"}, DenyRead: []string{"/Users/op/.ssh"}, PATH: "/usr/bin:/bin", Environment: []string{"TEST_CONFIG"}}
	if errs := validateBugIsolation(&valid); len(errs) != 0 {
		t.Fatal(errs)
	}
	for _, tc := range []struct {
		name   string
		change func(*BugCheckIsolation)
	}{
		{"relative launcher", func(p *BugCheckIsolation) { p.Binary = "codex" }},
		{"writable filesystem root", func(p *BugCheckIsolation) { p.WritableRoots = []string{"/"} }},
		{"relative writable root", func(p *BugCheckIsolation) { p.WritableRoots = []string{"cache"} }},
		{"unclean writable root", func(p *BugCheckIsolation) { p.WritableRoots = []string{"/tmp/../etc"} }},
		{"quoted writable root", func(p *BugCheckIsolation) { p.WritableRoots = []string{`/tmp/a"b`} }},
		{"unreadable filesystem root", func(p *BugCheckIsolation) { p.DenyRead = []string{"/"} }},
		{"relative unreadable path", func(p *BugCheckIsolation) { p.DenyRead = []string{".ssh"} }},
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
