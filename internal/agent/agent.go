// Package agent defines the pluggable per-agent behavior gskill installs into,
// plus a registry of adapters. Concrete adapters (Claude Code, Codex, Cursor,
// and the shared-location agents such as Antigravity CLI) live in sibling
// files and register themselves with a Registry.
package agent

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"

	"github.com/glapsfun/gskill/internal/active"
)

// ErrInvalidAgent is returned when registering an agent with an empty ID.
var ErrInvalidAgent = errors.New("invalid agent")

// Agent is the behavior gskill needs from a target AI agent to detect it and
// place skills into its skill directory (FR-027, FR-031, SC-009).
type Agent interface {
	// ID is the stable, lowercase identifier (e.g. "claude").
	ID() string
	// DisplayName is the human-facing name.
	DisplayName() string
	// Detect reports whether this agent is configured for the given project.
	Detect(ctx context.Context, projectRoot string) (bool, error)
	// ProjectSkillDir is the per-project directory skills install into.
	ProjectSkillDir(projectRoot string) string
	// GlobalSkillDir is the user-global directory skills install into.
	GlobalSkillDir(home string) string
	// SupportsSymlinks reports whether the agent tolerates symlinked skills.
	SupportsSymlinks() bool
	// ValidateInstallation checks that a skill installed at skillDir is usable
	// by the agent.
	ValidateInstallation(ctx context.Context, skillDir string) error
}

// Advisor is implemented by agents that can spot problems with an installed
// skill's content that would stop the agent from loading it. Advice is
// non-fatal guidance for the user and never fails an install (spec 027 FR-011).
type Advisor interface {
	// Advise returns one line per problem with the skill installed as
	// skillName at skillDir.
	Advise(skillName, skillDir string) []string
}

// ProjectNoter is implemented by agents that need one environment note after
// a project-scope install targets them, such as a trust step the agent requires
// before it loads project skills (spec 027 FR-012).
type ProjectNoter interface {
	ProjectNote() string
}

// UsesSharedDir reports whether a reads project skills straight from gskill's
// repo-owned store (.agents/skills), so its project target is the active entry
// itself. Such a target must never be linked, copied, or deleted on the
// agent's behalf (spec 027 FR-005, FR-010).
func UsesSharedDir(a Agent, projectRoot string) bool {
	return filepath.Clean(a.ProjectSkillDir(projectRoot)) == filepath.Clean(active.Dir(projectRoot))
}

// Registry holds the known agent adapters in registration order.
type Registry struct {
	mu     sync.RWMutex
	agents map[string]Agent
	order  []string
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{agents: make(map[string]Agent)}
}

// Register adds a to the registry. It errors on an empty ID or a duplicate.
func (r *Registry) Register(a Agent) error {
	id := a.ID()
	if id == "" {
		return fmt.Errorf("%w: empty ID", ErrInvalidAgent)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.agents[id]; exists {
		return fmt.Errorf("%w: agent %q already registered", ErrInvalidAgent, id)
	}
	r.agents[id] = a
	r.order = append(r.order, id)
	return nil
}

// Get returns the agent with the given ID.
func (r *Registry) Get(id string) (Agent, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	a, ok := r.agents[id]
	return a, ok
}

// All returns every registered agent in registration order.
func (r *Registry) All() []Agent {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]Agent, 0, len(r.order))
	for _, id := range r.order {
		out = append(out, r.agents[id])
	}
	return out
}

// Detect returns the registered agents that report being configured for
// projectRoot, preserving registration order.
func (r *Registry) Detect(ctx context.Context, projectRoot string) ([]Agent, error) {
	var detected []Agent
	for _, a := range r.All() {
		ok, err := a.Detect(ctx, projectRoot)
		if err != nil {
			return nil, fmt.Errorf("detect agent %q: %w", a.ID(), err)
		}
		if ok {
			detected = append(detected, a)
		}
	}
	return detected, nil
}
