package globalstore

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/glapsfun/gskill/internal/fsutil"
	"github.com/glapsfun/gskill/internal/home"
)

// ErrObjectNotFound reports a store lookup for a key with no admitted object.
var ErrObjectNotFound = errors.New("store object not found")

// contentDirName and metadataFileName are the fixed members of an object dir.
const (
	contentDirName   = "content"
	metadataFileName = "metadata.json"
)

// Store is the global content-addressed store rooted in a gskill home.
type Store struct {
	home  *home.Home
	locks *Locker
}

// New returns a Store over h. The home layout must already be ensured.
func New(h *home.Home) *Store { return &Store{home: h} }

// Home returns the store's home.
func (s *Store) Home() *home.Home { return s.home }

// Root returns the store root directory (<home>/store).
func (s *Store) Root() string { return s.home.StoreDir() }

// ObjectPath returns the object directory for a content key, whether or not
// it exists.
func (s *Store) ObjectPath(key string) string {
	return fsutil.KeyPath(s.Root(), key)
}

// ContentPath returns the immutable content directory for a content key.
func (s *Store) ContentPath(key string) string {
	return filepath.Join(s.ObjectPath(key), contentDirName)
}

// MetadataPath returns the metadata file for a content key.
func (s *Store) MetadataPath(key string) string {
	return filepath.Join(s.ObjectPath(key), metadataFileName)
}

// Has reports whether an admitted object exists for key. Presence means the
// content directory exists; integrity is established separately by
// VerifyObject.
func (s *Store) Has(key string) bool {
	_, err := os.Stat(s.ContentPath(key))
	return err == nil
}

// Object is an admitted store object.
type Object struct {
	// Key is the canonical content key ("sha256:<hex>").
	Key string
	// Path is the object directory.
	Path string
	// ContentPath is the immutable content directory.
	ContentPath string
	// Metadata is the object's descriptive record.
	Metadata Metadata
}

// Open loads the object for key, returning ErrObjectNotFound when it is not
// admitted and a metadata error when its record is unreadable or from an
// unsupported schema.
func (s *Store) Open(key string) (*Object, error) {
	if !s.Has(key) {
		return nil, fmt.Errorf("%w: %s", ErrObjectNotFound, key)
	}
	meta, err := ReadMetadata(s.MetadataPath(key))
	if err != nil {
		return nil, fmt.Errorf("object %s: %w", key, err)
	}
	return &Object{
		Key:         key,
		Path:        s.ObjectPath(key),
		ContentPath: s.ContentPath(key),
		Metadata:    meta,
	}, nil
}

// ListKeys returns the content keys of every object directory in the store,
// in no particular order. Malformed layouts are included (their key is the
// directory-derived name) so verification can report them.
func (s *Store) ListKeys() ([]string, error) {
	algos, err := os.ReadDir(s.Root())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read store root: %w", err)
	}
	var keys []string
	for _, algo := range algos {
		if !algo.IsDir() {
			continue
		}
		entries, err := os.ReadDir(filepath.Join(s.Root(), algo.Name()))
		if err != nil {
			return nil, fmt.Errorf("read store %s: %w", algo.Name(), err)
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			keys = append(keys, algo.Name()+":"+entry.Name())
		}
	}
	return keys, nil
}
