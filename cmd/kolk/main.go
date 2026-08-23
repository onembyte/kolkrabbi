// Kolkrabbi (binary: kolk) is a fast, lightweight agentic CLI for any model
// on OpenRouter (or any OpenAI-compatible endpoint): three modes — chat,
// code (Claude-Code style tool loop), and agent (orchestrated
// plan/delegate/synthesize) — with an effort dial that scales model tier and
// orchestration depth, persistent sessions, rewindable file checkpoints, and
// a 100% local usage/rating dashboard.
//
// This file is deliberately trivial. All behaviour lives in internal/cli, so
// the terminal is one client of the engine rather than the program itself.
package main

import (
	"context"
	"os"

	"github.com/onembyte/kolkrabbi/internal/cli"
)

func main() {
	os.Exit(cli.Main(context.Background(), os.Args[1:]))
}
