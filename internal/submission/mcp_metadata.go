package submission

import (
	"bytes"
	"encoding/json"
	"errors"
)

// MaxMCPMetadataBytes bounds ignored transport metadata independently of authored
// tool arguments, which have their own larger contribution budget.
const MaxMCPMetadataBytes = 64 << 10

// ValidateMCPMetadata permits MCP's extensible metadata object without giving
// its members application meaning or authority. Only the standard progressToken
// has a prescribed type; extension values remain opaque.
func ValidateMCPMetadata(data json.RawMessage) error {
	if len(data) == 0 {
		return nil
	}
	if len(data) > MaxMCPMetadataBytes {
		return errors.New("submission: MCP metadata exceeds limit")
	}
	data = bytes.TrimSpace(data)
	if len(data) == 0 || data[0] != '{' {
		return errors.New("submission: MCP metadata must be an object")
	}
	var fields map[string]json.RawMessage
	if err := decodeStrict(data, &fields); err != nil {
		return err
	}
	if token, ok := fields["progressToken"]; ok {
		// JSON syntax was validated above. A progress token is a string or
		// number, not necessarily an integer JSON-RPC request ID.
		if token[0] != '"' && token[0] != '-' && (token[0] < '0' || token[0] > '9') {
			return errors.New("submission: MCP progress token must be a string or number")
		}
	}
	return nil
}
