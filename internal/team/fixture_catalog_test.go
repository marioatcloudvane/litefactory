package team

import (
	"io/fs"
	"os"
)

// Historical catalog fixtures remain test-only; no prompt bytes enter the app.
func Embedded() (*Index, error) {
	b, err := os.ReadFile("testdata/teams/index.json")
	if err != nil {
		return nil, err
	}
	return Decode(b)
}

func EmbeddedFS() (fs.FS, error) { return os.DirFS("testdata/teams"), nil }
