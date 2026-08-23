package protocol

import "encoding/json"

// Encode validates and renders exactly one compact JSON frame. It does not add
// a newline or SSE prefix: future transports wrap these exact bytes so NDJSON
// lines and SSE data payloads cannot drift.
func Encode(e Envelope) ([]byte, error) {
	if err := e.validate(); err != nil {
		return nil, err
	}
	return json.Marshal(e)
}

// Decode reads exactly one JSON frame. encoding/json ignores unknown object
// fields by default, which is the forward-compatibility rule in protocol/doc.go.
func Decode(frame []byte) (Envelope, error) {
	var e Envelope
	if err := json.Unmarshal(frame, &e); err != nil {
		return Envelope{}, err
	}
	if err := e.validate(); err != nil {
		return Envelope{}, err
	}
	return e, nil
}
