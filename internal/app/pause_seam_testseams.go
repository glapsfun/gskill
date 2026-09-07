//go:build testseams

package app

import (
	"context"
	"fmt"
	"os"
	"time"
)

// pauseBeforeActivate blocks before an upgrade activates content when
// GSKILL_TEST_PAUSE=before-activate is set, printing "paused" on stderr so a
// harness can interrupt the process deterministically. It resumes on
// cancellation or after a bounded wait so a forgotten variable never hangs.
func pauseBeforeActivate(ctx context.Context) {
	if os.Getenv("GSKILL_TEST_PAUSE") != "before-activate" {
		return
	}
	fmt.Fprintln(os.Stderr, "paused")
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
}
