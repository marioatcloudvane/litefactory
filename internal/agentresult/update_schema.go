package agentresult

import (
	"bytes"
	_ "embed"
)

//go:embed testdata/v1/updates/schema.json
var updateSchema []byte

// UpdateJSONSchema returns a copied versioned structural update schema. Typed
// target values, authorization and final graph/evidence rules are validated in Go.
func UpdateJSONSchema() []byte { return bytes.Clone(updateSchema) }
