package agent

// NewClaudeCode returns the Claude Code adapter, which stores skills under
// .claude/skills/<name> (FR-027).
func NewClaudeCode() Agent {
	return dirAgent{id: "claude-code", name: "Claude Code", markerDir: ".claude", symlinks: true}
}

// NewDefaultRegistry returns a registry populated with the v1 built-in agent
// adapters in priority order.
func NewDefaultRegistry() *Registry {
	reg := NewRegistry()
	// Registration cannot fail for distinct built-in IDs.
	_ = reg.Register(NewClaudeCode())
	_ = reg.Register(NewCodex())
	return reg
}
