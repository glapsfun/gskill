package skillslock

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
)

// object is one JSON object with original key order and raw value bytes
// preserved, the unit of losslessness (FR-003, FR-005): keys gskill does not
// understand keep their position and value across every rewrite.
type object struct {
	keys []string
	vals map[string]json.RawMessage
	// origLen counts the keys that came from parsed input. Keys added later
	// live in keys[origLen:] and are kept sorted there when inserted via
	// setSortedSuffix, so rewrites stay deterministic and minimal-diff.
	origLen int
}

func newObject() *object { return &object{vals: map[string]json.RawMessage{}} }

func (o *object) get(k string) (json.RawMessage, bool) {
	v, ok := o.vals[k]
	return v, ok
}

func (o *object) has(k string) bool {
	_, ok := o.vals[k]
	return ok
}

// set replaces k in place, or appends it at the end in insertion order.
func (o *object) set(k string, v json.RawMessage) {
	if !o.has(k) {
		o.keys = append(o.keys, k)
	}
	o.vals[k] = v
}

// setSortedSuffix replaces k in place, or inserts it into the appended suffix
// keys[origLen:] keeping that suffix sorted.
func (o *object) setSortedSuffix(k string, v json.RawMessage) {
	if o.has(k) {
		o.vals[k] = v
		return
	}
	suffix := o.keys[o.origLen:]
	i := o.origLen + sort.SearchStrings(suffix, k)
	o.keys = append(o.keys, "")
	copy(o.keys[i+1:], o.keys[i:])
	o.keys[i] = k
	o.vals[k] = v
}

func (o *object) remove(k string) bool {
	if !o.has(k) {
		return false
	}
	for i, key := range o.keys {
		if key == k {
			o.keys = append(o.keys[:i], o.keys[i+1:]...)
			if i < o.origLen {
				o.origLen--
			}
			break
		}
	}
	delete(o.vals, k)
	return true
}

// parseObject consumes one JSON object from dec, preserving key order and raw
// value bytes.
func parseObject(dec *json.Decoder) (*object, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil, fmt.Errorf("expected object, found %v", tok)
	}
	o := newObject()
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, ok := kt.(string)
		if !ok {
			return nil, fmt.Errorf("expected object key, found %v", kt)
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, err
		}
		o.set(key, raw)
	}
	if _, err := dec.Token(); err != nil { // consume closing '}'
		return nil, err
	}
	o.origLen = len(o.keys)
	return o, nil
}

// parseChildObject parses a raw value that must itself be a JSON object.
func parseChildObject(raw json.RawMessage) (*object, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	o, err := parseObject(dec)
	if err != nil {
		return nil, err
	}
	return o, nil
}

// marshalRaw serializes v compactly without HTML escaping; writeValue
// re-indents it into place.
func marshalRaw(v any) (json.RawMessage, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return json.RawMessage(bytes.TrimSuffix(buf.Bytes(), []byte("\n"))), nil
}

// writeKey writes the JSON-quoted key without HTML escaping.
func writeKey(b *bytes.Buffer, k string) error {
	raw, err := marshalRaw(k)
	if err != nil {
		return err
	}
	b.Write(raw)
	return nil
}

// writeValue writes raw re-indented so nested lines align under prefix.
func writeValue(b *bytes.Buffer, raw json.RawMessage, prefix string) error {
	return json.Indent(b, raw, prefix, "  ")
}

// valueWriter emits the value for one key of an object being written.
type valueWriter func(b *bytes.Buffer, key, prefix string) error

// writeObjectKeys writes an object with the given key order, delegating each
// value to write. prefix is the indentation of the object's opening brace.
func writeObjectKeys(b *bytes.Buffer, keys []string, prefix string, write valueWriter) error {
	if len(keys) == 0 {
		b.WriteString("{}")
		return nil
	}
	b.WriteString("{\n")
	inner := prefix + "  "
	for i, k := range keys {
		b.WriteString(inner)
		if err := writeKey(b, k); err != nil {
			return err
		}
		b.WriteString(": ")
		if err := write(b, k, inner); err != nil {
			return err
		}
		if i < len(keys)-1 {
			b.WriteByte(',')
		}
		b.WriteByte('\n')
	}
	b.WriteString(prefix)
	b.WriteByte('}')
	return nil
}

// writeRawObject writes o using its stored raw values.
func writeRawObject(b *bytes.Buffer, o *object, prefix string) error {
	return writeObjectKeys(b, o.keys, prefix, func(b *bytes.Buffer, key, inner string) error {
		return writeValue(b, o.vals[key], inner)
	})
}

// parseErr wraps a JSON error as ErrInvalid, attaching position info when the
// underlying error carries it.
func parseErr(err error) error {
	var syn *json.SyntaxError
	if errors.As(err, &syn) {
		return fmt.Errorf("%w: %w (offset %d)", ErrInvalid, syn, syn.Offset)
	}
	var typ *json.UnmarshalTypeError
	if errors.As(err, &typ) {
		return fmt.Errorf("%w: %w (offset %d)", ErrInvalid, typ, typ.Offset)
	}
	return fmt.Errorf("%w: %w", ErrInvalid, err)
}
