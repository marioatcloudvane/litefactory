package bugrepair

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"slices"
	"strings"
)

// ValidationInput identifies a relevant tool, configuration or environment
// input by immutable bytes. Unidentified inputs cannot certify a generation.
type ValidationInput struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
}

// ValidationInputsDigest includes the ordered command contract and a canonical
// sorted manifest of relevant tools/configuration. Source and main are separate
// mandatory identities; commit metadata may be a declared input when relevant.
func ValidationInputsDigest(commands []ValidationRequirement, inputs []ValidationInput) (string, error) {
	if len(commands) == 0 || len(commands) > 128 || len(inputs) == 0 || len(inputs) > 256 {
		return "", errors.New("missing or excessive validation inputs")
	}
	seen := make(map[string]bool)
	for _, command := range commands {
		if command.ID == "" || seen[command.ID] || strings.TrimSpace(command.Command) == "" || (!fs.ValidPath(command.Directory) && command.Directory != ".") || strings.ContainsAny(command.Directory, "\\\x00\r\n") {
			return "", errors.New("invalid validation commands")
		}
		seen[command.ID] = true
	}
	manifest := slices.Clone(inputs)
	slices.SortFunc(manifest, func(a, b ValidationInput) int { return strings.Compare(a.Name, b.Name) })
	for i, input := range manifest {
		if strings.TrimSpace(input.Name) == "" || len(input.Name) > 256 || strings.ContainsAny(input.Name, "\x00\r\n") || !digest(input.SHA256, 32) || (i > 0 && manifest[i-1].Name == input.Name) {
			return "", errors.New("unknown or duplicate validation input")
		}
	}
	raw, err := json.Marshal(struct {
		Commands []ValidationRequirement
		Inputs   []ValidationInput
	}{commands, manifest})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}
