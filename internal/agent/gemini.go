package agent

// NewGeminiCLI returns the Gemini CLI adapter, which stores skills under
// .gemini/skills/<name> (FR-031, SC-009).
func NewGeminiCLI() Agent {
	return dirAgent{id: "gemini-cli", name: "Gemini CLI", markerDir: ".gemini", symlinks: true}
}
