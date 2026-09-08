package teamremote

import (
	"fmt"
	"regexp"
	"strings"
)

var versionPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?(?:\+([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?$`)

func versionParts(v string) ([]string, error) {
	p := versionPattern.FindStringSubmatch(v)
	if p == nil {
		return nil, fmt.Errorf("invalid exact semantic version %q", v)
	}
	if p[4] != "" {
		for _, id := range strings.Split(p[4], ".") {
			if numeric(id) && len(id) > 1 && id[0] == '0' {
				return nil, fmt.Errorf("numeric prerelease identifier has a leading zero")
			}
		}
	}
	return p, nil
}

// ValidateVersion requires exact SemVer 2.0, without v prefixes or ranges.
func ValidateVersion(v string) error { _, err := versionParts(v); return err }

func numeric(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return s != ""
}
func compareNumber(a, b string) int {
	if len(a) < len(b) {
		return -1
	}
	if len(a) > len(b) {
		return 1
	}
	return strings.Compare(a, b)
}

// CompareVersions compares arbitrary-length numeric components without overflow.
// Build metadata does not change SemVer precedence.
func CompareVersions(a, b string) (int, error) {
	x, err := versionParts(a)
	if err != nil {
		return 0, err
	}
	y, err := versionParts(b)
	if err != nil {
		return 0, err
	}
	for i := 1; i <= 3; i++ {
		if c := compareNumber(x[i], y[i]); c != 0 {
			return c, nil
		}
	}
	if x[4] == y[4] {
		return 0, nil
	}
	if x[4] == "" {
		return 1, nil
	}
	if y[4] == "" {
		return -1, nil
	}
	xs, ys := strings.Split(x[4], "."), strings.Split(y[4], ".")
	for i := 0; i < len(xs) && i < len(ys); i++ {
		a, b := xs[i], ys[i]
		if a == b {
			continue
		}
		an, bn := numeric(a), numeric(b)
		if an && bn {
			return compareNumber(a, b), nil
		}
		if an {
			return -1, nil
		}
		if bn {
			return 1, nil
		}
		return strings.Compare(a, b), nil
	}
	if len(xs) < len(ys) {
		return -1, nil
	}
	return 1, nil
}
