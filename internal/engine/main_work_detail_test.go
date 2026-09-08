package engine

import (
	"context"
	"io"
	"testing"

	"github.com/onembyte/kolkrabbi/protocol"
)

type detailWork struct{ steps []string }

func (d *detailWork) StartWork(context.Context, string) func() { return func() {} }
func (d *detailWork) WorkDetail(step string)                   { d.steps = append(d.steps, step) }

// The main turn's steps reach the screen's activity line through the work
// port, with or without an event bus: the screen is the reader that matters.
func TestMainWorkStepsReachTheScreensActivity(t *testing.T) {
	work := &detailWork{}
	a := &Agent{Options: Options{Work: work, Out: io.Discard}}
	a.lastTurnID = "t_01ARYZ6S41TSV4RRFFQ69G5FAW"
	a.publishMainWork(protocol.WorkStateWorking, protocol.WorkPhaseProvider, "model is responding", "m", "medium")
	a.publishMainWork(protocol.WorkStateWorking, protocol.WorkPhaseProvider, "   ", "m", "medium")
	if len(work.steps) != 1 || work.steps[0] != "model is responding" {
		t.Fatalf("steps reaching the screen = %q", work.steps)
	}
}
