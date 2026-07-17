package projstate

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/glapsfun/gskill/internal/fsutil"
)

// schemaVersion is the state.json schema this build reads and writes.
const schemaVersion = 1

// fileName is the state file under the project's .gskill directory.
const (
	stateDirName = ".gskill"
	fileName     = "state.json"
)

// Path returns the state.json path for a project root.
func Path(root string) string {
	return filepath.Join(root, stateDirName, fileName)
}

// State is a project's machine-local installation record. It is gitignored,
// never required to reproduce the project (the lockfile alone restores it,
// FR-015), and safe to delete: a fresh state with a new project ID is
// generated on the next gskill operation.
type State struct {
	SchemaVersion int                   `json:"schemaVersion"`
	ProjectID     string                `json:"projectId"`
	Skills        map[string]SkillState `json:"skills"`

	root string
}

// SkillState records which global object a skill activates and which links
// gskill owns for it — enough for safe repair and removal (FR-014).
type SkillState struct {
	StoreHash    string                `json:"storeHash"`
	StoreScope   string                `json:"storeScope,omitempty"`
	ActiveTarget string                `json:"activeTarget,omitempty"`
	ActiveMode   string                `json:"activeMode,omitempty"`
	Agents       map[string]AgentState `json:"agents,omitempty"`
}

// AgentState is one gskill-created agent target.
type AgentState struct {
	Target string `json:"target"`
	Mode   string `json:"mode"`
}

// LoadOrInit loads the project state under root, or initializes a fresh one
// (with a newly generated stable project ID) when none exists. A fresh state
// is not persisted until Save is called.
func LoadOrInit(root string) (*State, error) {
	path := Path(root)
	data, err := os.ReadFile(path) //nolint:gosec // project-relative fixed path
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("read project state: %w", err)
		}
		id, err := newProjectID()
		if err != nil {
			return nil, err
		}
		return &State{
			SchemaVersion: schemaVersion,
			ProjectID:     id,
			Skills:        map[string]SkillState{},
			root:          root,
		}, nil
	}

	var st State
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, fmt.Errorf("parse project state %s: %w", path, err)
	}
	if st.SchemaVersion != schemaVersion {
		return nil, fmt.Errorf("project state %s has schema version %d; this gskill understands version %d",
			path, st.SchemaVersion, schemaVersion)
	}
	if st.Skills == nil {
		st.Skills = map[string]SkillState{}
	}
	st.root = root
	return &st, nil
}

// Save writes the state atomically and deterministically (map keys are
// sorted by encoding/json).
func (s *State) Save() error {
	return fsutil.WriteJSONAtomic(Path(s.root), s, 0o600)
}

// Skill returns the recorded state for name.
func (s *State) Skill(name string) (SkillState, bool) {
	sk, ok := s.Skills[name]
	return sk, ok
}

// SetSkill records the state for name.
func (s *State) SetSkill(name string, sk SkillState) {
	s.Skills[name] = sk
}

// RemoveSkill drops the record for name.
func (s *State) RemoveSkill(name string) {
	delete(s.Skills, name)
}

// Root returns the project root this state belongs to.
func (s *State) Root() string { return s.root }

// newProjectID generates a stable random project identifier. It is not
// derived from the path, so it survives project moves (FR-017).
func newProjectID() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("generate project id: %w", err)
	}
	return "p-" + hex.EncodeToString(buf[:]), nil
}
