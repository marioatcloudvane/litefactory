package config

import "testing"

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
