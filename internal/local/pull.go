package local

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// PullHostModel asks the user's own Ollama to pull a model, streaming the
// server's progress to progress so a multi-gigabyte download is watched rather
// than waited for. It returns when the server says success, or with the
// server's reason when it does not.
//
// The pull is the host's: the bytes land in its store, under its digests, and
// `ollama list` shows them afterwards exactly as if the user had typed
// `ollama pull` — because that is what this is, with kolk's fit plan and
// approval in front of it.
func PullHostModel(ctx context.Context, addr, name string, progress io.Writer) error {
	body, _ := json.Marshal(map[string]any{"model": name, "stream": true})
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+addr+"/api/pull", bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	// No client timeout: a pull takes as long as the download takes, and the
	// caller's context is the bound.
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return fmt.Errorf("asking ollama at %s to pull %s: %w", addr, name, err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		reason, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return fmt.Errorf("ollama at %s refused to pull %s: HTTP %d: %s", addr, name, response.StatusCode, strings.TrimSpace(string(reason)))
	}

	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	lastStatus, lastPercent := "", -1
	for scanner.Scan() {
		var line struct {
			Status    string `json:"status"`
			Error     string `json:"error"`
			Total     int64  `json:"total"`
			Completed int64  `json:"completed"`
		}
		if json.Unmarshal(scanner.Bytes(), &line) != nil {
			continue
		}
		if line.Error != "" {
			return fmt.Errorf("pulling %s: %s", name, line.Error)
		}
		// One line per status, and one per ten percent of a layer: a 4 GB
		// pull reports thousands of chunks, and printing each is a wall.
		if line.Total > 0 {
			percent := int(line.Completed * 100 / line.Total)
			if line.Status == lastStatus && percent/10 == lastPercent/10 {
				continue
			}
			lastStatus, lastPercent = line.Status, percent
			fmt.Fprintf(progress, "  %s %d%%\n", line.Status, percent)
			continue
		}
		if line.Status != lastStatus {
			lastStatus, lastPercent = line.Status, -1
			fmt.Fprintf(progress, "  %s\n", line.Status)
		}
		if line.Status == "success" {
			return nil
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("pulling %s: the stream ended early: %w", name, err)
	}
	return fmt.Errorf("pulling %s: the server stopped without saying success", name)
}
