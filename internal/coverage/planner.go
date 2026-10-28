package coverage

import (
	"path"
	"slices"
	"strings"
)

// Build selects failures first, then every affected or unfinished obligation.
// Unknown facts expand execution instead of certifying inherited coverage.
func Build(in PlanInput) Plan {
	p := Plan{}
	prev := in.Previous
	failures, missing := priorFailureWork(in.Current, prev)
	p.Unresolved = missing
	switch {
	case in.Full:
		p.Full, p.Reason = true, "owner requested a fresh full suite"
	case !in.Current.Complete:
		p.Full, p.Reason = true, "current dependency inventory is incomplete: "+in.Current.Reason
	case prev == nil:
		p.Full, p.Reason = true, "no prior coverage assessment"
	case prev.Version != Version || !prev.Inventory.Complete || !prev.Complete:
		p.Full, p.Reason = true, "prior coverage is incomplete or incompatible"
	case prev.Recipe != in.Recipe || prev.Inventory.Conditions != in.Current.Conditions:
		p.Full, p.Reason = true, "recipe or execution conditions changed"
	}
	if p.Full {
		return fullPlan(p, in.Current, failures)
	}
	prior := map[string]Result{}
	for _, r := range prev.Results {
		prior[r.Unit] = r
	}
	// Union both snapshots: removed imports and deleted sources still affect
	// reverse consumers at the original execution anchor.
	units := map[string]Unit{}
	for _, u := range prev.Inventory.Units {
		units[u.Key] = u
	}
	for _, u := range in.Current.Units {
		old := units[u.Key]
		u.Files = append(append([]string(nil), u.Files...), old.Files...)
		u.Dependencies = append(append([]string(nil), u.Dependencies...), old.Dependencies...)
		units[u.Key] = u
	}
	knownPaths := map[string]bool{}
	for _, u := range units {
		for _, file := range u.Files {
			knownPaths[file] = true
		}
	}
	for _, change := range in.Changes {
		for _, file := range change.Paths {
			if !knownPaths[file] {
				p.Full, p.Reason = true, "changed input is outside the proven dependency inventory"
				return fullPlan(p, in.Current, failures)
			}
		}
	}
	var rest []Work
	for _, u := range in.Current.Units {
		r, ok := prior[u.Key]
		reusable := ok && (r.Kind == Executed || r.Kind == Inherited) && r.Origin.Unit == u.Key && r.Origin.Evidence != "" && r.Origin.Commit != "" && r.Origin.Conditions == in.Current.Conditions
		change, known := in.Changes[r.Origin.Commit]
		if reusable && (!known || !change.Proven || change.Anchor != r.Origin.Commit) {
			p.Full, p.Reason = true, "original coverage ancestry is unproven"
			return fullPlan(p, in.Current, failures)
		}
		if !reusable {
			rest = append(rest, Work{Unit: u, Purpose: "unfinished"})
			continue
		}
		for _, f := range change.Paths {
			if shared(f) {
				p.Full, p.Reason = true, "shared input, recipe, toolchain or guidance changed"
				return fullPlan(p, in.Current, failures)
			}
		}
		if affected(u.Key, units, change.Paths, map[string]bool{}) {
			rest = append(rest, Work{Unit: u, Purpose: "affected"})
		} else {
			r.Kind = Inherited
			r.Reason = "unchanged dependencies since original execution anchor"
			p.Inherited = append(p.Inherited, r)
		}
	}
	p.Work = append(p.Work, failures...)
	p.Work = append(p.Work, rest...)
	return p
}

func fullPlan(p Plan, current Inventory, failures []Work) Plan {
	p.Inherited = nil
	p.Work = append(p.Work, failures...)
	for _, u := range current.Units {
		p.Work = append(p.Work, Work{Unit: u, Purpose: "full"})
	}
	return p
}

func priorFailureWork(current Inventory, previous *Manifest) ([]Work, []Result) {
	if previous == nil {
		return nil, nil
	}
	units := map[string]Unit{}
	for _, u := range current.Units {
		units[u.Key] = u
	}
	failures := map[string][]Observation{}
	for _, r := range previous.Results {
		for _, o := range r.Cases {
			if o.Status == "fail" {
				failures[r.Unit] = append(failures[r.Unit], o)
			}
		}
	}
	for _, o := range previous.RequiredFailures {
		failures[o.Unit] = append(failures[o.Unit], o)
	}
	var work []Work
	// Current inventory order preserves declared recipe order within the phase.
	for _, u := range current.Units {
		var cases []string
		for _, o := range failures[u.Key] {
			if o.Case != "" && !slices.Contains(cases, o.Case) {
				cases = append(cases, o.Case)
			}
		}
		if len(cases) > 0 {
			work = append(work, Work{Unit: u, Cases: cases, Purpose: "prior-failure"})
		}
	}
	var missing []Result
	var keys []string
	for key := range failures {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		if _, ok := units[key]; !ok {
			missing = append(missing, Result{Unit: key, Kind: Unresolved, Cases: failures[key], Reason: "previously failing unit is absent from the current inventory"})
		}
	}
	return work, missing
}

func shared(f string) bool {
	b := path.Base(f)
	if slices.Contains([]string{"go.mod", "go.sum", "go.work", "go.work.sum", "Makefile", "pyproject.toml", "pytest.ini", "conftest.py", "requirements.txt", "requirements.lock", "global.json", "Directory.Build.props", "Directory.Build.targets", "Package.swift", "Package.resolved", "project.pbxproj"}, b) {
		return true
	}
	return strings.HasPrefix(f, "teams/") || strings.HasPrefix(f, "internal/teamsource/") || strings.HasPrefix(f, ".litefactory/") || strings.HasSuffix(f, ".lock")
}

func affected(key string, units map[string]Unit, files []string, seen map[string]bool) bool {
	if seen[key] {
		return false
	}
	seen[key] = true
	u, ok := units[key]
	if !ok {
		return true
	}
	for _, f := range files {
		if slices.Contains(u.Files, f) {
			return true
		}
	}
	for _, d := range u.Dependencies {
		if affected(d, units, files, seen) {
			return true
		}
	}
	return false
}

// Assess preserves failed attempts and requires a complete, fresh retry and
// equivalent pinned-base diagnostics before granting a base exemption.
func Assess(m Manifest) Manifest {
	m.Version = Version
	m.Complete = m.Inventory.Complete && m.Inventory.Commit == m.Commit
	m.Outcome = "passed"
	byUnit := map[string][]Attempt{}
	for _, a := range m.Attempts {
		for _, o := range a.Observations {
			byUnit[o.Unit] = appendAttempt(byUnit[o.Unit], a)
		}
	}
	retained := map[string]Result{}
	for _, r := range m.Results {
		if r.Kind == Inherited || r.Kind == Unresolved {
			retained[r.Unit] = r
		}
	}
	m.Results = nil
	m.Comparisons = nil
	for _, u := range m.Inventory.Units {
		if r, ok := retained[u.Key]; ok {
			delete(retained, u.Key)
			if r.Kind == Inherited && (r.Origin.Unit != u.Key || r.Origin.Evidence == "" || r.Origin.Commit == "" || r.Origin.Conditions != m.Inventory.Conditions) {
				r.Kind = Unresolved
				r.Reason = "incompatible inherited origin"
			}
			m.Results = append(m.Results, r)
			applyResult(&m, r)
			continue
		}
		r := Result{Unit: u.Key, Kind: Executed}
		attempts := byUnit[u.Key]
		var original *Attempt
		for i := range attempts {
			a := &attempts[i]
			if a.Purpose != "base" && a.Purpose != "retry" {
				original = a
			}
		}
		if !usableAttempt(original, m.Commit, m.Inventory.Conditions) {
			r.Kind, r.Reason = Unresolved, "missing complete fresh execution"
		} else {
			r.Origin = Origin{Unit: u.Key, Commit: m.Commit, Evidence: original.ID, Conditions: original.Conditions}
			r.Cases = observedUnit(*original, u.Key)
			if len(r.Cases) == 0 {
				r.Kind, r.Reason = Unresolved, "no observed test identities"
			}
			for _, o := range r.Cases {
				if !o.Complete || o.Case == "" || (o.Status != "pass" && o.Status != "fail") {
					r.Kind = moreSevere(r.Kind, Unresolved)
				}
			}
		}
		// A later full pass establishes group completion, but cannot erase an
		// earlier failure from the same tested commit. Attribute each identity
		// against its earliest observed failure and its explicit fresh retry.
		type caseIdentity struct{ Case, Phase string }
		seenFailures := map[caseIdentity]bool{}
		for _, a := range attempts {
			if a.Purpose == "base" || a.Purpose == "retry" || a.Commit != m.Commit {
				continue
			}
			for _, o := range observedUnit(a, u.Key) {
				key := caseIdentity{o.Case, o.Phase}
				if o.Status != "fail" || seenFailures[key] {
					continue
				}
				seenFailures[key] = true
				if !usableAttempt(&a, m.Commit, m.Inventory.Conditions) || !o.Complete || o.Case == "" {
					r.Kind = moreSevere(r.Kind, Unresolved)
					continue
				}
				comparison := assessFailure(m, attempts, a, o)
				m.Comparisons = append(m.Comparisons, comparison)
				r.Kind = moreSevere(r.Kind, comparison.Kind)
			}
		}
		// A containing retry may expose a new failure. Its first observation
		// becomes the original attempt; only a later fresh retry can settle it.
		for i, a := range attempts {
			if a.Purpose != "retry" || a.Commit != m.Commit {
				continue
			}
			for _, o := range observedUnit(a, u.Key) {
				key := caseIdentity{o.Case, o.Phase}
				if o.Status != "fail" || seenFailures[key] {
					continue
				}
				seenFailures[key] = true
				comparison := assessFailure(m, attempts[i+1:], a, o)
				m.Comparisons = append(m.Comparisons, comparison)
				r.Cases = append(r.Cases, o)
				r.Kind = moreSevere(r.Kind, comparison.Kind)
				if comparison.Kind == Unresolved {
					r.Reason = "new failure discovered during retry lacks a completed retry assessment"
				}
			}
		}
		m.Results = append(m.Results, r)
		applyResult(&m, r)
	}
	// A removed failing unit remains unresolved even though discovery no longer
	// includes it. Its absence is evidence loss, not an observed passing repair.
	var remaining []string
	for key := range retained {
		remaining = append(remaining, key)
	}
	slices.Sort(remaining)
	for _, key := range remaining {
		r := retained[key]
		r.Kind = Unresolved
		m.Results = append(m.Results, r)
		applyResult(&m, r)
	}
	for _, required := range m.RequiredFailures {
		if !observedRequiredFailure(m.Attempts, required, m.Commit, m.Inventory.Conditions) {
			m.Results = append(m.Results, Result{Unit: required.Unit, Kind: Unresolved, Cases: []Observation{required}, Reason: "previous failure was not observed on the tested commit"})
			m.Complete = false
			m.Outcome = "blocked"
		}
	}
	if len(m.Inventory.Units) == 0 || !m.Complete {
		m.Outcome = "blocked"
		m.Complete = false
	}
	return m
}

func assessFailure(m Manifest, attempts []Attempt, original Attempt, o Observation) Comparison {
	c := Comparison{Unit: o.Unit, Case: o.Case, Phase: o.Phase, Original: original.ID, Kind: Unresolved}
	retry, ro := findCase(attempts, o.Unit, o.Case, o.Phase, "retry")
	if retry != nil {
		c.Retry = retry.ID
	}
	if !usableAttempt(retry, m.Commit, original.Conditions) || !ro.Complete {
		return c
	}
	if ro.Status == "pass" {
		c.Kind = Transient
		return c
	}
	if ro.Status != "fail" {
		return c
	}
	base, bo := findCase(attempts, o.Unit, o.Case, o.Phase, "base")
	if base != nil {
		c.Base = base.ID
		c.BaseCommit = base.Commit
	}
	if m.BaseCommit == "" || !usableAttempt(base, m.BaseCommit, original.Conditions) || !bo.Complete {
		return c
	}
	switch {
	case bo.Status == "fail" && unambiguousDiagnostic(o.Diagnostic) && o.Diagnostic == ro.Diagnostic && ro.Diagnostic == bo.Diagnostic:
		c.Kind = PreExisting
	case bo.Status == "pass":
		c.Kind = Failed
	}
	return c
}

func usableAttempt(a *Attempt, commit, conditions string) bool {
	return a != nil && a.ID != "" && a.Commit == commit && a.Complete && a.Fresh && a.Conditions == conditions
}

func observedRequiredFailure(attempts []Attempt, required Observation, commit, conditions string) bool {
	for _, a := range attempts {
		if a.Purpose == "base" || !usableAttempt(&a, commit, conditions) {
			continue
		}
		for _, o := range a.Observations {
			if o.Unit == required.Unit && o.Case == required.Case && (o.Phase == required.Phase || o.Phase == "" && o.Status == "pass") && o.Complete && (o.Status == "pass" || o.Status == "fail") {
				return true
			}
		}
	}
	return false
}

func moreSevere(old, next string) string {
	ranks := map[string]int{Executed: 0, Transient: 1, PreExisting: 2, Failed: 3, Unresolved: 4}
	if ranks[next] > ranks[old] {
		return next
	}
	return old
}

func applyResult(m *Manifest, r Result) {
	switch r.Kind {
	case Unresolved:
		m.Complete = false
		m.Outcome = "blocked"
	case Failed:
		if m.Outcome != "blocked" {
			m.Outcome = "failed"
		}
	case Inherited, Transient, PreExisting:
		if m.Outcome == "passed" {
			m.Outcome = "covered"
		}
	}
}

func observedUnit(a Attempt, u string) []Observation {
	var out []Observation
	for _, o := range a.Observations {
		if o.Unit == u {
			out = append(out, o)
		}
	}
	return out
}
func appendAttempt(as []Attempt, a Attempt) []Attempt {
	for _, old := range as {
		if old.ID == a.ID {
			return as
		}
	}
	return append(as, a)
}
func findCase(as []Attempt, u, c, phase, p string) (*Attempt, Observation) {
	for i := len(as) - 1; i >= 0; i-- {
		a := &as[i]
		if a.Purpose != p {
			continue
		}
		for _, o := range a.Observations {
			if o.Unit == u && o.Case == c && o.Phase == phase {
				return a, o
			}
		}
	}
	// A phase-free passing observation is an adapter's complete aggregate
	// case pass (including setup/call/teardown), so it may settle a phase
	// failure. A pass from a different named phase cannot do so.
	if phase != "" {
		for i := len(as) - 1; i >= 0; i-- {
			a := &as[i]
			if a.Purpose != p {
				continue
			}
			for _, o := range a.Observations {
				if o.Unit == u && o.Case == c && o.Phase == "" && o.Status == "pass" && o.Complete {
					return a, o
				}
			}
		}
	}
	return nil, Observation{}
}

func unambiguousDiagnostic(text string) bool {
	return text != "" && !strings.Contains(strings.ToLower(text), "[redacted]") && !strings.ContainsRune(text, '\uFFFD')
}
