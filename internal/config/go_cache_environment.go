package config

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// ValidateGoCacheEnvironment rejects external cache programs in the effective
// child environment and its Go environment file. An empty GOCACHEPROG alone
// does not override a persisted value in that file. No Go settings are changed.
// Nil uses the process environment; a non-nil slice describes the child exactly.
func ValidateGoCacheEnvironment(environ []string) error {
	_, err := readGoCacheEnvironment(environ)
	return err
}

// readGoCacheEnvironment resolves the two cache settings with Go's nonempty
// process-value precedence, reading the persisted file at most once.
func readGoCacheEnvironment(environ []string) (map[string]string, error) {
	if environ == nil {
		environ = os.Environ()
	}
	values := make(map[string]string, len(environ))
	for _, entry := range environ {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			values[key] = value
		}
	}
	if values["GOCACHEPROG"] != "" {
		return nil, fmt.Errorf("dedicated Go cache: GOCACHEPROG is incompatible with on-disk cache routing")
	}
	file := values["GOENV"]
	if file == "off" {
		return values, nil
	}
	if file != "" && !filepath.IsAbs(file) {
		return nil, fmt.Errorf("dedicated Go cache: GOENV must be absolute or off")
	}
	if file == "" {
		var directory string
		switch runtime.GOOS {
		case "darwin", "ios":
			if values["HOME"] != "" {
				directory = filepath.Join(values["HOME"], "Library", "Application Support")
			}
		case "windows":
			directory = values["AppData"]
			if directory == "" {
				directory = values["APPDATA"]
			}
		case "plan9":
			if values["home"] != "" {
				directory = filepath.Join(values["home"], "lib")
			}
		default:
			directory = values["XDG_CONFIG_HOME"]
			if directory != "" && !filepath.IsAbs(directory) {
				return nil, fmt.Errorf("dedicated Go cache: XDG_CONFIG_HOME must be absolute")
			}
			if directory == "" && values["HOME"] != "" {
				directory = filepath.Join(values["HOME"], ".config")
			}
		}
		// Go cannot read a user env file without a user config directory either.
		if directory == "" {
			return values, nil
		}
		file = filepath.Join(directory, "go", "env")
	}
	// #nosec G304 -- file is the effective operator-selected Go configuration.
	handle, err := os.Open(file)
	if os.IsNotExist(err) {
		return values, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read Go environment file %q: %w", file, err)
	}
	data, readErr := io.ReadAll(io.LimitReader(handle, (1<<20)+1))
	closeErr := handle.Close()
	if readErr != nil {
		return nil, fmt.Errorf("read Go environment file %q: %w", file, readErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("close Go environment file %q: %w", file, closeErr)
	}
	if len(data) > 1<<20 {
		return nil, fmt.Errorf("go environment file %q exceeds 1 MiB", file)
	}
	persisted := make(map[string]string, 2)
	for _, line := range strings.Split(string(data), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if ok && (key == "GOCACHEPROG" || key == "GOCACHE") {
			persisted[key] = value
		}
	}
	for key, value := range persisted {
		if values[key] == "" {
			values[key] = value
		}
	}
	if values["GOCACHEPROG"] != "" {
		return nil, fmt.Errorf("dedicated Go cache: GOCACHEPROG in Go environment file %q is incompatible with on-disk cache routing", file)
	}
	return values, nil
}
