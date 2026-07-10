package skillslock

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"

	"github.com/glapsfun/gskill/internal/fsutil"
)

// FileName is the shared project-level lock file gskill co-owns with
// compatible external tooling.
const FileName = "skills-lock.json"

// SchemaVersion is the skills-lock.json schema this build understands.
const SchemaVersion = 1

// Sentinel errors.
var (
	// ErrInvalid is returned for a structurally or semantically invalid lock.
	ErrInvalid = errors.New("invalid skills-lock.json")
	// ErrUnsupportedSchema is returned for a version other than SchemaVersion;
	// gskill refuses to guess at (or rewrite) a schema it does not understand.
	ErrUnsupportedSchema = errors.New("unsupported skills-lock.json schema version")
)

// Lock is the lossless in-memory form of skills-lock.json: typed access to
// the fields gskill understands, raw preservation for everything else.
type Lock struct {
	doc     *object            // top level: version, skills, unknown fields
	skills  *object            // entry-name ordering (values live in entries)
	entries map[string]*object // per-entry key order + raw values
}

// Entry is the typed view of one skill entry's core fields plus gskill's
// namespaced extension block (nil when the entry was never installed by
// gskill). Unknown entry fields stay in the Lock and survive rewrites.
type Entry struct {
	Source       string
	Ref          string // optional branch/tag used for installation (npx "ref")
	SourceType   string
	SkillPath    string
	ComputedHash string
	Ext          *Ext
}

// New returns an empty lock at the current schema version.
func New() *Lock {
	doc := newObject()
	doc.set("version", json.RawMessage("1"))
	doc.set("skills", json.RawMessage("{}"))
	doc.origLen = len(doc.keys)
	return &Lock{doc: doc, skills: newObject(), entries: map[string]*object{}}
}

// Unmarshal parses lock bytes, enforcing the structural schema: valid JSON,
// version == SchemaVersion, and a skills object whose values are objects.
// Entry-level field validation is Validate's job.
func Unmarshal(data []byte) (*Lock, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	doc, err := parseObject(dec)
	if err != nil {
		return nil, parseErr(err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%w: trailing content after top-level object", ErrInvalid)
	}

	rawVer, ok := doc.get("version")
	if !ok {
		return nil, fmt.Errorf("%w: missing \"version\"", ErrInvalid)
	}
	var version int
	if err := json.Unmarshal(rawVer, &version); err != nil {
		return nil, fmt.Errorf("%w: \"version\" must be an integer: %w", ErrInvalid, err)
	}
	if version != SchemaVersion {
		return nil, fmt.Errorf("%w: %d (this build understands %d; upgrade gskill)",
			ErrUnsupportedSchema, version, SchemaVersion)
	}

	rawSkills, ok := doc.get("skills")
	if !ok {
		return nil, fmt.Errorf("%w: missing \"skills\"", ErrInvalid)
	}
	skills, err := parseChildObject(rawSkills)
	if err != nil {
		return nil, fmt.Errorf("%w: \"skills\" must be an object: %w", ErrInvalid, err)
	}
	entries := make(map[string]*object, len(skills.keys))
	for _, name := range skills.keys {
		raw, _ := skills.get(name)
		eo, err := parseChildObject(raw)
		if err != nil {
			return nil, fmt.Errorf("%w: skill %q must be an object: %w", ErrInvalid, name, err)
		}
		entries[name] = eo
	}
	return &Lock{doc: doc, skills: skills, entries: entries}, nil
}

// Load reads and parses the lock file at path.
func Load(path string) (*Lock, error) {
	data, err := os.ReadFile(path) //nolint:gosec // project-root lock file path
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return Unmarshal(data)
}

// Marshal serializes the lock deterministically: original key order, unknown
// values re-emitted verbatim, 2-space indent, trailing newline, no HTML
// escaping. New keys appear in sorted order after the original ones.
func Marshal(l *Lock) ([]byte, error) {
	var b bytes.Buffer
	err := writeObjectKeys(&b, l.doc.keys, "", func(b *bytes.Buffer, key, inner string) error {
		if key == "skills" {
			return l.writeSkills(b, inner)
		}
		raw, _ := l.doc.get(key)
		return writeValue(b, raw, inner)
	})
	if err != nil {
		return nil, fmt.Errorf("marshal %s: %w", FileName, err)
	}
	b.WriteByte('\n')
	return b.Bytes(), nil
}

// writeSkills emits the skills object from the per-entry ordered objects.
func (l *Lock) writeSkills(b *bytes.Buffer, prefix string) error {
	return writeObjectKeys(b, l.skills.keys, prefix, func(b *bytes.Buffer, name, inner string) error {
		return writeRawObject(b, l.entries[name], inner)
	})
}

// Save writes the lock to path atomically. The file is committed and shared
// with other tools, so it is world-readable.
func Save(path string, l *Lock) error {
	data, err := Marshal(l)
	if err != nil {
		return err
	}
	if err := fsutil.WriteFileAtomic(path, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// Version returns the parsed schema version.
func (l *Lock) Version() int { return SchemaVersion }

// Names returns the entry names in file order.
func (l *Lock) Names() []string {
	out := make([]string, len(l.skills.keys))
	copy(out, l.skills.keys)
	return out
}

// Has reports whether an entry exists.
func (l *Lock) Has(name string) bool { return l.entries[name] != nil }

// Entry returns the typed view of one entry.
func (l *Lock) Entry(name string) (Entry, bool) {
	eo, ok := l.entries[name]
	if !ok {
		return Entry{}, false
	}
	var e Entry
	e.Source = stringField(eo, "source")
	e.Ref = stringField(eo, "ref")
	e.SourceType = stringField(eo, "sourceType")
	e.SkillPath = stringField(eo, "skillPath")
	e.ComputedHash = stringField(eo, "computedHash")
	if raw, ok := eo.get("gskill"); ok {
		var ext Ext
		if err := json.Unmarshal(raw, &ext); err == nil {
			e.Ext = &ext
		}
	}
	return e, true
}

// SetEntry creates or updates an entry, preserving any fields it does not
// understand. Core identity fields (source, ref, sourceType, skillPath) are
// owned by whichever tool wrote them first: gskill fills them only when
// absent and never rewrites them. computedHash is the shared verification
// fact and is updated in place; the gskill extension block is always gskill's
// to replace. New entries are inserted in sorted order after the original
// ones.
func (l *Lock) SetEntry(name string, e Entry) {
	eo, ok := l.entries[name]
	if !ok {
		eo = newObject()
		l.entries[name] = eo
		// The value in skills is unused (entries carries content); the key
		// slot fixes ordering.
		l.skills.setSortedSuffix(name, nil)
	}
	setStringFieldIfAbsent(eo, "source", e.Source)
	setStringFieldIfAbsent(eo, "ref", e.Ref)
	setStringFieldIfAbsent(eo, "sourceType", e.SourceType)
	setStringFieldIfAbsent(eo, "skillPath", e.SkillPath)
	setStringField(eo, "computedHash", e.ComputedHash)
	if e.Ext != nil {
		raw, err := marshalRaw(e.Ext)
		if err == nil {
			eo.set("gskill", raw)
		}
	}
}

// SetExt creates or replaces the namespaced gskill block on an existing entry.
func (l *Lock) SetExt(name string, ext *Ext) error {
	eo, ok := l.entries[name]
	if !ok {
		return fmt.Errorf("%w: no entry %q to attach gskill metadata to", ErrInvalid, name)
	}
	raw, err := marshalRaw(ext)
	if err != nil {
		return fmt.Errorf("marshal gskill block for %q: %w", name, err)
	}
	eo.set("gskill", raw)
	return nil
}

// Remove deletes an entry, leaving every other byte of the document untouched.
func (l *Lock) Remove(name string) bool {
	if _, ok := l.entries[name]; !ok {
		return false
	}
	delete(l.entries, name)
	return l.skills.remove(name)
}

// Validate checks every entry's required core fields and rejects skillPath
// values that are absolute or escape the project (path traversal, FR-027).
func (l *Lock) Validate() error {
	for _, name := range l.skills.keys {
		e, _ := l.Entry(name)
		if e.Source == "" {
			return fmt.Errorf("%w: skill %q: missing \"source\"", ErrInvalid, name)
		}
		if e.SourceType == "" {
			return fmt.Errorf("%w: skill %q: missing \"sourceType\"", ErrInvalid, name)
		}
		if e.SkillPath == "" {
			return fmt.Errorf("%w: skill %q: missing \"skillPath\"", ErrInvalid, name)
		}
		if err := validSkillPath(e.SkillPath); err != nil {
			return fmt.Errorf("%w: skill %q: invalid \"skillPath\" %q: %w", ErrInvalid, name, e.SkillPath, err)
		}
		if e.ComputedHash == "" {
			return fmt.Errorf("%w: skill %q: missing \"computedHash\"", ErrInvalid, name)
		}
	}
	return nil
}

// validSkillPath rejects absolute paths and any path that escapes its root.
func validSkillPath(p string) error {
	if strings.HasPrefix(p, "/") || strings.HasPrefix(p, "\\") || strings.Contains(p, ":") {
		return errors.New("must be relative")
	}
	clean := path.Clean(strings.ReplaceAll(p, "\\", "/"))
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return errors.New("escapes the project root")
	}
	return nil
}

// stringField reads a string-valued key from an entry object ("" if absent or
// not a string).
func stringField(o *object, key string) string {
	raw, ok := o.get(key)
	if !ok {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	return s
}

// setStringField writes a string-valued key. Empty values are never written
// and never overwrite an existing value: callers with partial knowledge (e.g.
// the legacy bridge, which cannot derive computedHash) must not erase facts
// another writer recorded.
func setStringField(o *object, key, val string) {
	if val == "" {
		return
	}
	raw, err := marshalRaw(val)
	if err != nil {
		return
	}
	o.set(key, raw)
}

// setStringFieldIfAbsent writes a string-valued key only when it is not
// already present, so a rewrite never repurposes a core field another tool
// recorded.
func setStringFieldIfAbsent(o *object, key, val string) {
	if o.has(key) {
		return
	}
	setStringField(o, key, val)
}
