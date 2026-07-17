package testutil

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/glapsfun/gskill/internal/git"
)

// CountingGit wraps a git.Runner, counting network calls per method and
// optionally injecting latency or a fixed error. A ResolveRef of a full
// commit SHA is not counted: the real runner answers it locally. Counters
// are safe for concurrent use.
type CountingGit struct {
	Inner   git.Runner
	Latency time.Duration // simulated network latency per counted call
	Fail    error         // when set, every counted call returns it

	Tags, Heads, Refs, Fetches atomic.Int64
}

// ResolutionCalls returns the total ls-remote-style round trips (everything
// except FetchCommit).
func (c *CountingGit) ResolutionCalls() int64 {
	return c.Tags.Load() + c.Heads.Load() + c.Refs.Load()
}

func (c *CountingGit) network() error {
	if c.Latency > 0 {
		time.Sleep(c.Latency)
	}
	return c.Fail
}

// LsRemoteTags counts the call, then delegates.
func (c *CountingGit) LsRemoteTags(ctx context.Context, url string) ([]git.TagRef, error) {
	c.Tags.Add(1)
	if err := c.network(); err != nil {
		return nil, err
	}
	return c.Inner.LsRemoteTags(ctx, url)
}

// LsRemoteHeads counts the call, then delegates.
func (c *CountingGit) LsRemoteHeads(ctx context.Context, url string) ([]git.BranchRef, error) {
	c.Heads.Add(1)
	if err := c.network(); err != nil {
		return nil, err
	}
	return c.Inner.LsRemoteHeads(ctx, url)
}

// ResolveRef counts non-SHA resolutions, then delegates.
func (c *CountingGit) ResolveRef(ctx context.Context, url, ref string) (string, error) {
	if !git.IsFullSHA(ref) {
		c.Refs.Add(1)
		if err := c.network(); err != nil {
			return "", err
		}
	}
	return c.Inner.ResolveRef(ctx, url, ref)
}

// FetchCommit counts the call, then delegates.
func (c *CountingGit) FetchCommit(ctx context.Context, url, commit, dest string) error {
	c.Fetches.Add(1)
	if err := c.network(); err != nil {
		return err
	}
	return c.Inner.FetchCommit(ctx, url, commit, dest)
}
