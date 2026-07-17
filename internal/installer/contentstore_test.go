package installer_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/glapsfun/gskill/internal/agent"
	"github.com/glapsfun/gskill/internal/cache"
	"github.com/glapsfun/gskill/internal/errs"
	"github.com/glapsfun/gskill/internal/installer"
	"github.com/glapsfun/gskill/internal/integrity"
	"github.com/glapsfun/gskill/internal/source"
)

// fakeContentStore is a ContentStore double that records calls and can
// simulate a present, corrupt, or absent object.
type fakeContentStore struct {
	root     string
	objects  map[string]string // hash -> content dir
	corrupt  map[string]bool
	puts     int
	verifies int
	touches  int
}

func newFakeContentStore(t *testing.T) *fakeContentStore {
	t.Helper()
	return &fakeContentStore{
		root:    t.TempDir(),
		objects: map[string]string{},
		corrupt: map[string]bool{},
	}
}

func (f *fakeContentStore) Root() string                  { return f.root }
func (f *fakeContentStore) Has(hash string) bool          { _, ok := f.objects[hash]; return ok }
func (f *fakeContentStore) ScopeLabel() string            { return "global" }
func (f *fakeContentStore) Touch(context.Context, string) { f.touches++ }

func (f *fakeContentStore) Path(hash string) string {
	if dir, ok := f.objects[hash]; ok {
		return dir
	}
	return filepath.Join(f.root, "absent")
}

func (f *fakeContentStore) Verify(hash string) error {
	f.verifies++
	if f.corrupt[hash] {
		return errs.Wrap(errs.CodeIntegrity, "corrupted global store object "+hash, nil)
	}
	return nil
}

func (f *fakeContentStore) Put(_ context.Context, hash, srcDir string, _ installer.ObjectOrigin) (string, error) {
	f.puts++
	if dir, ok := f.objects[hash]; ok {
		return dir, nil
	}
	dest := filepath.Join(f.root, "obj-"+hash[len(hash)-8:])
	if err := os.CopyFS(dest, os.DirFS(srcDir)); err != nil {
		return "", err
	}
	f.objects[hash] = dest
	return dest, nil
}

// seed puts a real skill dir into the fake store and returns its hash.
func (f *fakeContentStore) seed(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	skill := "---\nname: argocd\ndescription: The argocd skill\n---\n" + body
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(skill), 0o600); err != nil {
		t.Fatal(err)
	}
	hashes, err := integrity.HashDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	f.objects[hashes.ContentHash] = dir
	return hashes.ContentHash
}

// storeReq builds a minimal local-source install request for the store tests.
func storeReq(t *testing.T, hash string) installer.Request {
	t.Helper()
	return installer.Request{
		Ref:               source.Ref{Type: source.TypeLocal, LocalPath: t.TempDir()},
		Name:              "argocd",
		Agents:            []agent.Agent{agent.NewClaudeCode()},
		Scope:             installer.ScopeProject,
		ModePref:          installer.PrefAuto,
		ProjectRoot:       t.TempDir(),
		ExpectContentHash: hash,
	}
}

func TestInstall_StoreHitReusesWithoutFetch(t *testing.T) {
	t.Parallel()

	f := newFakeContentStore(t)
	hash := f.seed(t, "# argocd\n")
	// nil git runner: any fetch attempt would fail loudly.
	inst := installer.NewWithStore(nil, cache.New(t.TempDir()), f)

	req := storeReq(t, hash)
	res, err := inst.Install(context.Background(), req)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if res.StoreReuse != installer.StoreReused {
		t.Errorf("StoreReuse = %q, want %q", res.StoreReuse, installer.StoreReused)
	}
	if res.StoreScope != "global" {
		t.Errorf("StoreScope = %q, want global", res.StoreScope)
	}
	if res.ContentHash != hash {
		t.Errorf("ContentHash = %q, want %q", res.ContentHash, hash)
	}
	if f.verifies == 0 {
		t.Error("store object was activated without verification")
	}
	if f.touches == 0 {
		t.Error("reuse did not touch last-used")
	}
	if res.SkillFileHash == "" {
		t.Error("SkillFileHash empty on store reuse")
	}
	// The active entry resolves into the store content.
	active := filepath.Join(req.ProjectRoot, ".agents", "skills", "argocd")
	resolved, err := filepath.EvalSymlinks(active)
	if err != nil {
		t.Fatalf("active entry: %v", err)
	}
	wantContent, _ := filepath.EvalSymlinks(f.Path(hash))
	if resolved != wantContent {
		t.Errorf("active resolves to %q, want %q", resolved, wantContent)
	}
}

func TestInstall_StoreCorruptFailsClosed(t *testing.T) {
	t.Parallel()

	f := newFakeContentStore(t)
	hash := f.seed(t, "# argocd\n")
	f.corrupt[hash] = true
	inst := installer.NewWithStore(nil, cache.New(t.TempDir()), f)

	req := storeReq(t, hash)
	_, err := inst.Install(context.Background(), req)
	if err == nil {
		t.Fatal("Install activated a corrupt store object")
	}
	if !errors.Is(err, errs.ErrIntegrity) {
		t.Errorf("err = %v, want ErrIntegrity", err)
	}
	// Nothing was activated.
	if _, statErr := os.Stat(filepath.Join(req.ProjectRoot, ".agents", "skills", "argocd")); !os.IsNotExist(statErr) {
		t.Error("corrupt object was activated")
	}
}

func TestInstall_StoreMissFetchesAndAdmits(t *testing.T) {
	t.Parallel()

	f := newFakeContentStore(t)
	// Local source with a real skill; store is empty.
	src := t.TempDir()
	body := "---\nname: argocd\ndescription: The argocd skill\n---\n# argocd\n"
	if err := os.WriteFile(filepath.Join(src, "SKILL.md"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	hashes, err := integrity.HashDir(src)
	if err != nil {
		t.Fatal(err)
	}

	inst := installer.NewWithStore(nil, cache.New(t.TempDir()), f)
	res, err := inst.Install(context.Background(), installer.Request{
		Ref:               source.Ref{Type: source.TypeLocal, LocalPath: src},
		Name:              "argocd",
		Agents:            []agent.Agent{agent.NewClaudeCode()},
		Scope:             installer.ScopeProject,
		ModePref:          installer.PrefAuto,
		ProjectRoot:       t.TempDir(),
		ExpectContentHash: hashes.ContentHash,
	})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if res.StoreReuse != installer.StoreDownloaded {
		t.Errorf("StoreReuse = %q, want %q", res.StoreReuse, installer.StoreDownloaded)
	}
	if f.puts == 0 {
		t.Error("store miss did not admit the fetched content")
	}
	if !f.Has(hashes.ContentHash) {
		t.Error("content not admitted under its hash")
	}
}
