package submission

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"time"
)

// Call invokes one helper operation with JSON data, without subprocesses or
// environment changes. Submit correlation is synced locally before transmission;
// calling again with identical bytes reuses the same identity after lost replies.
func Call(ctx context.Context, descriptorPath, method string, payload json.RawMessage) (Response, error) {
	var response Response
	if method != "preflight" && method != "submit" && method != "receipt" && method != "tools/list" && method != "tools/call" {
		return response, errors.New("submission: unsupported method")
	}
	if len(payload) > MaxFrameBytes-1024 || !json.Valid(payload) {
		return response, errors.New("submission: invalid or oversized JSON")
	}
	if !filepath.IsAbs(descriptorPath) || filepath.Clean(descriptorPath) != descriptorPath || filepath.Base(descriptorPath) != "descriptor.json" {
		return response, errors.New("submission: invalid descriptor path")
	}
	dir := filepath.Dir(descriptorPath)
	root, pinned, err := openPrivateRoot(dir)
	if err != nil {
		return response, err
	}
	defer root.Close()
	data, descriptorInfo, err := privateRead(root, "descriptor.json", maxDescriptorBytes)
	if err != nil {
		return response, err
	}
	var d descriptor
	if err := decodeStrict(data, &d); err != nil || d.Version != 1 || d.Socket != "s" || len(d.Capability) != 32 {
		return response, errors.New("submission: invalid attempt descriptor")
	}
	socket, err := root.Lstat(d.Socket)
	if err != nil || socket.Mode()&os.ModeSocket == 0 || !privateOwner(socket) || !descriptorSocketMatches(d, socket) {
		return response, errors.New("submission: unsafe or replaced socket")
	}
	correlationMethod, correlationPayload := method, payload
	if method == "tools/call" {
		call, callErr := DecodeToolCall(payload)
		if callErr != nil {
			return response, callErr
		}
		correlationPayload = call.Arguments
		switch call.Name {
		case "communication_submit":
			correlationMethod = "submit"
		case "communication_receipt":
			correlationMethod = "receipt"
		}
	}
	switch correlationMethod {
	case "submit":
		response.RequestID, err = journalRequest(root, correlationPayload)
	case "receipt":
		var lookup struct {
			RequestID string `json:"request_id"`
		}
		err = decodeStrict(correlationPayload, &lookup)
		response.RequestID = lookup.RequestID
		if lookup.RequestID == "" || len(lookup.RequestID) > 128 {
			err = errors.New("submission: invalid receipt correlation")
		}
	}
	if err != nil {
		return response, err
	}
	boundCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	dialer := net.Dialer{}
	connection, err := dialer.DialContext(boundCtx, "unix", filepath.Join(dir, d.Socket))
	if err != nil {
		return response, errors.New("submission: broker unavailable; retain request correlation")
	}
	defer connection.Close()
	if !stableDirectory(root, dir, pinned) || !samePrivateFile(root, "descriptor.json", descriptorInfo) || !samePrivateFile(root, d.Socket, socket) {
		return response, errors.New("submission: attempt files changed")
	}
	deadline, ok := boundCtx.Deadline()
	if !ok {
		return response, errors.New("submission: missing request deadline")
	}
	if err := connection.SetDeadline(deadline); err != nil {
		return response, errors.New("submission: cannot bound request")
	}
	cancelled := make(chan struct{})
	stop := context.AfterFunc(boundCtx, func() { defer close(cancelled); _ = connection.Close() })
	defer func() {
		if !stop() {
			<-cancelled
		}
	}()
	data, err = marshalRequest(Request{Method: method, RequestID: response.RequestID, Payload: payload, Capability: d.Capability})
	if err != nil {
		return response, errors.New("submission: invalid request")
	}
	if err := writeFrame(connection, data); err != nil {
		return response, errors.New("submission: send interrupted; retain request correlation")
	}
	data, err = readFrame(connection)
	if err != nil {
		return response, errors.New("submission: reply unavailable; query retained request correlation")
	}
	var received Response
	if err := decodeStrict(data, &received); err != nil || received.RequestID != response.RequestID {
		return response, errors.New("submission: invalid response correlation")
	}
	return received, nil
}
func samePrivateFile(root *os.Root, name string, original fs.FileInfo) bool {
	current, err := root.Lstat(name)
	return err == nil && os.SameFile(original, current) && privateOwner(current) && original.Size() == current.Size() && original.ModTime().Equal(current.ModTime())
}

type requestJournal struct {
	RequestID string `json:"request_id"`
	Digest    string `json:"digest"`
}

func journalRequest(root *os.Root, payload []byte) (string, error) {
	hash := sha256.Sum256(payload)
	digest := hex.EncodeToString(hash[:])
	name := "request-" + digest + ".json"
	if _, err := root.Lstat(name); err == nil {
		return readJournal(root, name, digest)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", errors.New("submission: cannot inspect correlation")
	}
	id, err := randomID()
	if err != nil {
		return "", errors.New("submission: correlation unavailable")
	}
	data, err := json.Marshal(requestJournal{RequestID: id, Digest: digest})
	if err != nil {
		return "", err
	}
	// Publish a fully synced record with an exclusive hard link. Concurrent
	// callers elect one identity; a crash cannot leave a lock blocking correction.
	tempName := "pending-" + id + ".json"
	if err := privateCreate(root, tempName, data); err != nil {
		return "", errors.New("submission: cannot persist correlation")
	}
	defer func() { _ = root.Remove(tempName) }()
	if err := root.Link(tempName, name); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return readJournal(root, name, digest)
		}
		return "", errors.New("submission: cannot publish correlation")
	}
	directory, err := root.Open(".")
	if err != nil {
		return "", errors.New("submission: cannot persist correlation directory")
	}
	syncErr := directory.Sync()
	closeErr := directory.Close()
	if syncErr != nil || closeErr != nil {
		return "", errors.New("submission: cannot sync correlation directory")
	}

	return id, nil
}
func readJournal(root *os.Root, name, digest string) (string, error) {
	data, _, err := privateRead(root, name, 1024)
	if err != nil {
		return "", err
	}
	var journal requestJournal
	if err := decodeStrict(data, &journal); err != nil || journal.Digest != digest || len(journal.RequestID) != 32 {
		return "", errors.New("submission: invalid correlation journal")
	}
	return journal.RequestID, nil
}

// marshalRequest preserves authored bytes across the private transport envelope.
func marshalRequest(q Request) ([]byte, error) {
	meta, err := json.Marshal(struct {
		Method     string `json:"method"`
		RequestID  string `json:"request_id"`
		Capability string `json:"capability"`
	}{q.Method, q.RequestID, q.Capability})
	if err != nil {
		return nil, err
	}
	meta = meta[:len(meta)-1]
	meta = append(meta, []byte(`,"payload":`)...)
	meta = append(meta, q.Payload...)
	return append(meta, '}'), nil
}
