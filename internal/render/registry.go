package render

import (
	"fmt"
	"sort"
	"sync"
)

// registry maps a harness name (e.g. "claude", "codex", "opencode") to the
// [Renderer] that produces its native output shape. It is a plain map on
// purpose (see the package doc): AC-1's extensibility check — a fake
// harness added purely as a new entry, with no other package touched — is
// true by construction because there is nothing else here to touch.
//
// Registration is expected to happen once per harness package, from that
// package's own init, before any concurrent Render/Lookup traffic starts —
// the same convention as image.RegisterFormat and sql.Register. The mutex
// exists so a test that registers a fake harness mid-run, or a Lookup that
// races a late init in some build configuration, is still race-detector
// clean; it is not a sign that concurrent registration during steady-state
// operation is an expected pattern.
var (
	registryMu sync.RWMutex
	registry   = map[string]Renderer{}
)

// Register adds r under harness. Calling Register twice for the same
// harness name is a programmer error — almost certainly two packages, or
// one package's init running twice, claiming the same harness — so it
// panics rather than silently letting the second registration win; this is
// the one place in this package panic is appropriate, since it can only
// ever fire at process wiring time, never on a request path.
func Register(harness string, r Renderer) {
	if harness == "" {
		panic("render: Register called with empty harness name")
	}
	if r == nil {
		panic(fmt.Sprintf("render: Register(%q, nil)", harness))
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, exists := registry[harness]; exists {
		panic(fmt.Sprintf("render: harness %q registered more than once", harness))
	}
	registry[harness] = r
}

// Lookup returns the [Renderer] registered under harness, or an
// [*UnknownHarnessError] if none is.
func Lookup(harness string) (Renderer, error) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	r, ok := registry[harness]
	if !ok {
		return nil, &UnknownHarnessError{Harness: harness, Registered: sortedHarnessesLocked()}
	}
	return r, nil
}

// Harnesses returns every registered harness name, sorted, so a caller
// listing them (a diagnostic, a help message) never depends on map
// iteration order (TEST-7).
func Harnesses() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()
	return sortedHarnessesLocked()
}

// sortedHarnessesLocked returns every registered harness name, sorted. The
// caller MUST hold registryMu (read or write) before calling it.
func sortedHarnessesLocked() []string {
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
