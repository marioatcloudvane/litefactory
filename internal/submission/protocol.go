package submission

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// MaxFrameBytes bounds request and response metadata, excluding shared snapshots.
const MaxFrameBytes = 16 << 20
const maxDescriptorBytes = 4096

// Request is the authenticated local call passed to the factory handler. The
// capability is removed before handler dispatch and never appears in responses.
type Request struct {
	Method     string          `json:"method"`
	RequestID  string          `json:"request_id"`
	Payload    json.RawMessage `json:"payload"`
	Capability string          `json:"capability"`
}

// Response carries durable correlation even when delivery remains uncertain.
// Result is the factory's structured diagnostics or authoritative receipt.
type Response struct {
	RequestID string          `json:"request_id"`
	Result    json.RawMessage `json:"result,omitempty"`
	Error     string          `json:"error,omitempty"`
}

type descriptor struct {
	Version      int    `json:"version"`
	SocketDevice int64  `json:"socket_device"`
	SocketInode  uint64 `json:"socket_inode"`
	Socket       string `json:"socket"`
	Capability   string `json:"capability"`
}

func readFrame(r io.Reader) ([]byte, error) {
	var size [4]byte
	if _, err := io.ReadFull(r, size[:]); err != nil {
		return nil, err
	}
	n := binary.BigEndian.Uint32(size[:])
	if n == 0 || n > MaxFrameBytes {
		return nil, errors.New("submission: invalid frame size")
	}
	data := make([]byte, int(n))
	_, err := io.ReadFull(r, data)
	return data, err
}
func writeFrame(w io.Writer, data []byte) error {
	n := len(data)
	if n == 0 || n > MaxFrameBytes {
		return errors.New("submission: invalid frame size")
	}
	var size [4]byte
	binary.BigEndian.PutUint32(size[:], uint32(n))
	if _, err := io.Copy(w, bytes.NewReader(size[:])); err != nil {
		return err
	}
	_, err := io.Copy(w, bytes.NewReader(data))
	return err
}
func decodeStrict(data []byte, out interface{}) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return errors.New("submission: invalid JSON fields or types")
	}
	if err := decoder.Decode(&json.RawMessage{}); !errors.Is(err, io.EOF) {
		return errors.New("submission: trailing JSON")
	}
	// Duplicate keys require a separate walk; typed decoding would retain only
	// the final value and could hide an authorization field.
	check := json.NewDecoder(bytes.NewReader(data))
	_, requestEnvelope := out.(*Request)
	return uniqueJSON(check, 0, requestEnvelope)
}
func uniqueJSON(d *json.Decoder, depth int, requestEnvelope bool) error {
	if depth > 32 {
		return errors.New("submission: nesting limit")
	}
	token, err := d.Token()
	if err != nil {
		return errors.New("submission: malformed JSON")
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for d.More() {
			key, err := d.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			if !ok || seen[name] {
				return errors.New("submission: duplicate or invalid key")
			}
			seen[name] = true
			if depth == 0 && requestEnvelope && name == "payload" {
				var payload json.RawMessage
				if err := d.Decode(&payload); err != nil {
					return errors.New("submission: malformed payload")
				}
				continue
			}
			if err := uniqueJSON(d, depth+1, requestEnvelope); err != nil {
				return err
			}
		}
	case '[':
		for d.More() {
			if err := uniqueJSON(d, depth+1, requestEnvelope); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("submission: unexpected delimiter")
	}
	_, err = d.Token()
	return err
}

// DecodeJSON shares strict envelope decoding (duplicate keys, depth and unknown
// fields) with the MCP edge without duplicating application validation.
func DecodeJSON(data []byte, out interface{}) error { return decodeStrict(data, out) }
