package mcpadapter

import (
	"bytes"
	"encoding/json"
	"errors"
	"math/big"
	"unicode/utf8"

	"github.com/cloudvane/litefactory/internal/submission"
)

// ProtocolVersion is the sole supported MCP version negotiated at initialization.
const ProtocolVersion = "2025-11-25"

// MaxInputBytes includes envelope space above the 4 MiB authored contribution.
const MaxInputBytes = (4 << 20) + (64 << 10)

// MaxOutputBytes bounds the combined structured and escaped text response.
const MaxOutputBytes = 64 << 20

// MaxWorkers bounds simultaneously executing calls in one connection.
const MaxWorkers = 4

// MaxQueuedResponses bounds completed responses waiting for serialized output.
const MaxQueuedResponses = 8

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}
type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

func failure(id json.RawMessage, code int, message string) response {
	if len(id) == 0 {
		id = json.RawMessage(`null`)
	}
	return response{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: message}}
}
func decode(data []byte, out interface{}) error {
	if !utf8.Valid(data) {
		return errors.New("invalid UTF-8")
	}
	return submission.DecodeJSON(data, out)
}
func parseRequest(data []byte) (request, error) {
	var q request
	err := decode(data, &q)
	if err == nil && (q.JSONRPC != "2.0" || q.Method == "" || len(q.Method) > 128) {
		err = errors.New("invalid JSON-RPC envelope")
	}
	if err == nil && len(q.ID) > 0 {
		_, err = requestKey(q.ID)
	}
	return q, err
}
func requestKey(id json.RawMessage) (string, error) {
	if len(id) == 0 || len(id) > 256 {
		return "", errors.New("invalid request ID")
	}
	var value string
	if len(id) > 0 && id[0] == '"' && json.Unmarshal(id, &value) == nil {
		return "s:" + value, nil
	}
	number := new(big.Int)
	if _, ok := number.SetString(string(id), 10); !ok {
		return "", errors.New("request ID must be a string or integer")
	}
	return "n:" + number.String(), nil
}
func object(data []byte) bool {
	return len(bytes.TrimSpace(data)) > 0 && bytes.TrimSpace(data)[0] == '{'
}
func emptyParams(data []byte) error {
	if len(data) == 0 {
		return nil
	}
	var empty struct{}
	if !object(data) {
		return errors.New("parameters must be an object")
	}
	return decode(data, &empty)
}

// encodeResponse is bounded before JSON's encoder allocates its temporary
// escaped representation. Result has already passed a bounded backend check.
func encodeResponse(r response) ([]byte, error) {
	var tail []byte
	if r.Error != nil {
		var err error
		tail, err = json.Marshal(r.Error)
		if err != nil {
			return nil, err
		}
	} else {
		tail = r.Result
	}
	if len(tail)+len(r.ID)+128 > MaxOutputBytes {
		return nil, errors.New("response exceeds limit")
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, tail); err != nil {
		return nil, err
	}
	tail = compact.Bytes()
	data := make([]byte, 0, len(tail)+len(r.ID)+64)
	data = append(data, []byte(`{"jsonrpc":"2.0","id":`)...)
	data = append(data, r.ID...)
	if r.Error != nil {
		data = append(data, []byte(`,"error":`)...)
	} else {
		data = append(data, []byte(`,"result":`)...)
	}
	data = append(data, tail...)
	return append(data, '}', '\n'), nil
}
func encodeNoHTML(value interface{}) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buffer.Bytes(), []byte{'\n'}), nil
}

func toolResponse(id json.RawMessage, r submission.Response) response {
	if len(r.Result) > submission.MaxFrameBytes || len(r.Error) > 4096 || len(r.RequestID) > 128 {
		return failure(id, -32603, "Backend response exceeds limit")
	}
	// Encoding duplicates the bounded application result as interoperable text
	// and structured content. Estimate worst-case escaping before allocation.
	if len(r.Result)*4+8192 > MaxOutputBytes {
		return failure(id, -32603, "Backend response exceeds limit")
	}
	raw, err := encodeNoHTML(r)
	if err != nil {
		return failure(id, -32603, "Invalid backend response")
	}
	rejected := r.Error != ""
	var diagnostic struct {
		Valid      *bool `json:"valid"`
		Validation *struct {
			Valid bool `json:"valid"`
		} `json:"Validation"`
	}
	if len(r.Result) > 0 && json.Unmarshal(r.Result, &diagnostic) == nil {
		rejected = rejected || (diagnostic.Valid != nil && !*diagnostic.Valid) || (diagnostic.Validation != nil && !diagnostic.Validation.Valid)
	}
	result, err := encodeNoHTML(struct {
		Content    []textContent   `json:"content"`
		Structured json.RawMessage `json:"structuredContent"`
		IsError    bool            `json:"isError"`
	}{[]textContent{{Type: "text", Text: string(raw)}}, raw, rejected})
	if err != nil {
		return failure(id, -32603, "Invalid backend response")
	}
	return response{JSONRPC: "2.0", ID: id, Result: result}
}

type textContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}
