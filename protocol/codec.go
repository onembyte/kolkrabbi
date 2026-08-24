package protocol

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
)

// StreamFormat selects one of Kolkrabbi's exact event stream grammars.
type StreamFormat string

const (
	// StreamNDJSON is one compact envelope followed by LF per event.
	StreamNDJSON StreamFormat = "ndjson"
	// StreamSSE is Kolkrabbi's strict id/event/data SSE block grammar.
	StreamSSE StreamFormat = "sse"

	// MaxStreamFrameBytes bounds the envelope JSON carried by either stream.
	// Transport prefixes and line terminators are not included.
	MaxStreamFrameBytes = 1 << 20
)

// ErrStreamFrameTooLarge reports an envelope above MaxStreamFrameBytes.
var ErrStreamFrameTooLarge = errors.New("protocol: stream frame exceeds 1 MiB")

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

// EncodeNDJSON renders one validated envelope as exactly one LF-terminated
// compact JSON line.
func EncodeNDJSON(e Envelope) ([]byte, error) {
	frame, err := Encode(e)
	if err != nil {
		return nil, err
	}
	return append(frame, '\n'), nil
}

// EncodeSSE renders one validated envelope as one Server-Sent Event block.
// Its data field is byte-identical to Encode and to EncodeNDJSON without LF.
func EncodeSSE(e Envelope) ([]byte, error) {
	frame, err := Encode(e)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, len(frame)+len(e.Type)+40)
	out = append(out, "id: "...)
	out = strconv.AppendUint(out, e.Seq, 10)
	out = append(out, '\n')
	out = append(out, "event: "...)
	out = append(out, e.Type...)
	out = append(out, '\n')
	out = append(out, "data: "...)
	out = append(out, frame...)
	out = append(out, '\n', '\n')
	return out, nil
}

// SSEHeartbeat returns the exact idle-connection comment block. The returned
// bytes do not share mutable storage with later calls.
func SSEHeartbeat() []byte {
	return []byte(": ping\n\n")
}

// DecodeStream reads validated envelopes one at a time and delivers them to
// yield. It retains no event collection. Closing the supplied reader is the
// caller's cancellation mechanism; any yield error stops delivery immediately.
func DecodeStream(reader io.Reader, format StreamFormat, yield func(Envelope) error) error {
	if reader == nil {
		return errors.New("protocol: stream reader is nil")
	}
	if yield == nil {
		return errors.New("protocol: stream callback is nil")
	}
	if format != StreamNDJSON && format != StreamSSE {
		return fmt.Errorf("protocol: unsupported stream format %q", format)
	}

	buffered := bufio.NewReader(reader)
	for {
		var (
			envelope Envelope
			done     bool
			err      error
		)
		switch format {
		case StreamNDJSON:
			envelope, done, err = decodeNDJSONEnvelope(buffered)
		case StreamSSE:
			envelope, done, err = decodeSSEEnvelope(buffered)
		}
		if err != nil {
			return err
		}
		if done {
			return nil
		}
		if err := yield(envelope); err != nil {
			return err
		}
	}
}

func decodeNDJSONEnvelope(reader *bufio.Reader) (Envelope, bool, error) {
	line, err := readStreamLine(reader, MaxStreamFrameBytes)
	if errors.Is(err, io.EOF) {
		return Envelope{}, true, nil
	}
	if err != nil {
		return Envelope{}, false, fmt.Errorf("protocol: read NDJSON frame: %w", err)
	}
	if err := validateFrameWhitespace(line); err != nil {
		return Envelope{}, false, fmt.Errorf("protocol: invalid NDJSON frame: %w", err)
	}
	envelope, err := Decode(line)
	if err != nil {
		return Envelope{}, false, fmt.Errorf("protocol: decode NDJSON frame: %w", err)
	}
	return envelope, false, nil
}

func decodeSSEEnvelope(reader *bufio.Reader) (Envelope, bool, error) {
	for {
		first, err := readStreamLine(reader, len("id: ")+20)
		if errors.Is(err, io.EOF) {
			return Envelope{}, true, nil
		}
		if err != nil {
			return Envelope{}, false, fmt.Errorf("protocol: read SSE id: %w", err)
		}
		if bytes.Equal(first, []byte(": ping")) {
			blank, err := readStreamLine(reader, 0)
			if err != nil {
				return Envelope{}, false, fmt.Errorf("protocol: read SSE heartbeat terminator: %w", err)
			}
			if len(blank) != 0 {
				return Envelope{}, false, errors.New("protocol: SSE heartbeat lacks blank terminator")
			}
			continue
		}
		if len(first) == 0 || first[0] == ':' {
			return Envelope{}, false, errors.New("protocol: invalid SSE block start")
		}

		const idPrefix = "id: "
		if !bytes.HasPrefix(first, []byte(idPrefix)) {
			return Envelope{}, false, errors.New("protocol: SSE block must start with id")
		}
		idText := string(first[len(idPrefix):])
		seq, err := strconv.ParseUint(idText, 10, 64)
		if err != nil || strconv.FormatUint(seq, 10) != idText {
			return Envelope{}, false, errors.New("protocol: SSE id must be canonical unsigned decimal")
		}

		eventLine, err := readStreamLine(reader, MaxStreamFrameBytes)
		if err != nil {
			return Envelope{}, false, fmt.Errorf("protocol: read SSE event: %w", err)
		}
		const eventPrefix = "event: "
		if !bytes.HasPrefix(eventLine, []byte(eventPrefix)) || len(eventLine) == len(eventPrefix) {
			return Envelope{}, false, errors.New("protocol: SSE id must be followed by event")
		}
		eventType := EventType(eventLine[len(eventPrefix):])

		dataLine, err := readStreamLine(reader, MaxStreamFrameBytes+len("data: "))
		if err != nil {
			return Envelope{}, false, fmt.Errorf("protocol: read SSE data: %w", err)
		}
		const dataPrefix = "data: "
		if !bytes.HasPrefix(dataLine, []byte(dataPrefix)) {
			return Envelope{}, false, errors.New("protocol: SSE event must be followed by data")
		}
		frame := dataLine[len(dataPrefix):]
		if len(frame) > MaxStreamFrameBytes {
			return Envelope{}, false, ErrStreamFrameTooLarge
		}
		if err := validateFrameWhitespace(frame); err != nil {
			return Envelope{}, false, fmt.Errorf("protocol: invalid SSE data frame: %w", err)
		}

		blank, err := readStreamLine(reader, 0)
		if err != nil {
			return Envelope{}, false, fmt.Errorf("protocol: read SSE block terminator: %w", err)
		}
		if len(blank) != 0 {
			return Envelope{}, false, errors.New("protocol: SSE data must be followed by a blank line")
		}

		envelope, err := Decode(frame)
		if err != nil {
			return Envelope{}, false, fmt.Errorf("protocol: decode SSE data frame: %w", err)
		}
		if envelope.Seq != seq {
			return Envelope{}, false, fmt.Errorf("protocol: SSE id %d does not match envelope seq %d", seq, envelope.Seq)
		}
		if envelope.Type != eventType {
			return Envelope{}, false, fmt.Errorf("protocol: SSE event %q does not match envelope type %q", eventType, envelope.Type)
		}
		return envelope, false, nil
	}
}

func readStreamLine(reader *bufio.Reader, max int) ([]byte, error) {
	line := make([]byte, 0, min(max, reader.Size()))
	for {
		fragment, err := reader.ReadSlice('\n')
		switch {
		case err == nil:
			payload := fragment[:len(fragment)-1]
			if len(line)+len(payload) > max {
				return nil, ErrStreamFrameTooLarge
			}
			line = append(line, payload...)
			if bytes.IndexByte(line, '\r') >= 0 {
				return nil, errors.New("protocol: CR is not valid stream framing")
			}
			return line, nil
		case errors.Is(err, bufio.ErrBufferFull):
			if len(line)+len(fragment) > max {
				return nil, ErrStreamFrameTooLarge
			}
			line = append(line, fragment...)
		case errors.Is(err, io.EOF):
			if len(line) == 0 && len(fragment) == 0 {
				return nil, io.EOF
			}
			return nil, fmt.Errorf("protocol: unterminated stream line: %w", io.ErrUnexpectedEOF)
		default:
			return nil, err
		}
	}
}

func validateFrameWhitespace(frame []byte) error {
	if len(frame) == 0 {
		return errors.New("protocol: empty stream frame")
	}
	if jsonWhitespace(frame[0]) || jsonWhitespace(frame[len(frame)-1]) {
		return errors.New("protocol: transport whitespace around JSON frame")
	}
	return nil
}

func jsonWhitespace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\r' || b == '\n'
}
