package serve

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/protocol"
)

func TestStreamConformanceNDJSONAndSSE(t *testing.T) {
	files, err := filepath.Glob("../../spec/testdata/streams/*.ndjson")
	if err != nil || len(files) == 0 {
		t.Fatalf("no stream fixtures found: err=%v len=%d", err, len(files))
	}

	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			data, err := os.ReadFile(file)
			if err != nil {
				t.Fatalf("ReadFile %s: %v", file, err)
			}

			// Read the expected SSE companion
			sseFile := strings.TrimSuffix(file, ".ndjson") + ".sse"
			expectedSSEBytes, err := os.ReadFile(sseFile)
			if err != nil {
				t.Fatalf("ReadFile %s: %v", sseFile, err)
			}

			scanner := bufio.NewScanner(bytes.NewReader(data))
			var envelopes []protocol.Envelope
			var ndjsonLines []string

			for scanner.Scan() {
				line := strings.TrimSpace(scanner.Text())
				if line == "" {
					continue
				}
				ndjsonLines = append(ndjsonLines, line)
				var env protocol.Envelope
				if err := json.Unmarshal([]byte(line), &env); err != nil {
					t.Fatalf("unmarshal line in %s: %v\nline: %s", file, err, line)
				}
				envelopes = append(envelopes, env)
			}

			if len(envelopes) == 0 {
				t.Fatalf("no envelopes decoded from %s", file)
			}

			// Format each envelope into SSE block and check
			sseScanner := bufio.NewScanner(bytes.NewReader(expectedSSEBytes))
			var expectedSSEEvents []string
			var currentBlock strings.Builder
			for sseScanner.Scan() {
				line := sseScanner.Text()
				if line == "" {
					if currentBlock.Len() > 0 {
						expectedSSEEvents = append(expectedSSEEvents, currentBlock.String())
						currentBlock.Reset()
					}
				} else {
					currentBlock.WriteString(line + "\n")
				}
			}
			if currentBlock.Len() > 0 {
				expectedSSEEvents = append(expectedSSEEvents, currentBlock.String())
			}

			if len(expectedSSEEvents) != len(envelopes) {
				t.Fatalf("expected SSE event blocks = %d, envelopes = %d", len(expectedSSEEvents), len(envelopes))
			}

			for i, env := range envelopes {
				envJSON, err := json.Marshal(env)
				if err != nil {
					t.Fatal(err)
				}

				// Verify NDJSON compact JSON matches
				ndjsonLine := ndjsonLines[i]
				if string(envJSON) != ndjsonLine {
					t.Errorf("NDJSON line %d mismatch:\ngot:\n%s\nwant:\n%s", i, string(envJSON), ndjsonLine)
				}

				// Verify SSE block data and event match byte-for-byte
				sseBlock := expectedSSEEvents[i]
				if !strings.Contains(sseBlock, "data: "+string(envJSON)) {
					t.Errorf("SSE block %d data mismatch:\ngot block:\n%s\nwant data:\n%s", i, sseBlock, string(envJSON))
				}
				if !strings.Contains(sseBlock, "event: "+string(env.Type)) {
					t.Errorf("SSE block %d event type mismatch:\ngot block:\n%s\nwant event:\n%s", i, sseBlock, env.Type)
				}
			}
		})
	}
}
