package agent

// DefaultID is the agent used when none is specified on the command line and
// none is detected in the project. Installs default to Claude.
const DefaultID = "claude"

// NewClaudeCode returns the Claude adapter, which stores skills under
// .claude/skills/<name> (FR-027).
func NewClaudeCode() Agent {
	return dirAgent{id: "claude", name: "Claude", markerDir: ".claude", symlinks: true}
}

// NewDefaultRegistry returns a registry populated with the v1 built-in agent
// adapters in priority order.
func NewDefaultRegistry() *Registry {
	reg := NewRegistry()
	// Registration cannot fail for distinct built-in IDs.
	_ = reg.Register(NewClaudeCode())
	_ = reg.Register(NewCodex())
	_ = reg.Register(NewCursor())
	_ = reg.Register(NewGeminiCLI())
	return reg
}
