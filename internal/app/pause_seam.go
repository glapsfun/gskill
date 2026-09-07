//go:build !testseams

package app

import "context"

// pauseBeforeActivate is a no-op outside test builds.
func pauseBeforeActivate(context.Context) {}
