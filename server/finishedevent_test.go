package server

import (
	"context"
	"testing"
)

// TestARowThatRanBeforeItsRequestLookedEndsWithFinished: the loop admits a
// row and runs it to its end before the request reaches the queue's select,
// so r.admitted and r.done are both closed when it does. Every such generate
// must still emit exactly one Finished event. Before, select picked r.done
// about half the time and returned r.result.err (nil) with no Finished: the
// compat layer's "the generate finished without a finished event". Each run
// is a coin flip on the old code, so 40 runs miss it with probability 2^-40.
func TestARowThatRanBeforeItsRequestLookedEndsWithFinished(t *testing.T) {
	e, lm, _ := hostBatchEngine(t, deviceModel, Config{})
	lm.loop.beforeQueueWait = func(r *row) { <-r.done }
	for i := range 40 {
		finished := 0
		err := e.Generate(context.Background(), GenerateOptions{ModelID: "dev",
			Prompt: Prompt{Kind: PromptText, Text: "Once upon a time"}, MaxTokens: 2},
			func(ev Event) error {
				if ev.Kind == EventFinished {
					finished++
				}
				return nil
			})
		if err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
		if finished != 1 {
			t.Fatalf("run %d: %d Finished events, want exactly 1", i, finished)
		}
	}
}
