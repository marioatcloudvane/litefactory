package codex

import "strings"

func managedCacheEnv(env []string, cache string) []string {
	out := make([]string, 0, len(env)+2)
	for _, entry := range env {
		if !strings.HasPrefix(entry, "GOCACHE=") && !strings.HasPrefix(entry, "GOCACHEPROG=") {
			out = append(out, entry)
		}
	}
	return append(out, "GOCACHE="+cache, "GOCACHEPROG=")
}
