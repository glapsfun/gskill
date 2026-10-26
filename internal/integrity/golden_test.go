package integrity_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/glapsfun/gskill/internal/integrity"
)

// Content hashes recorded by the pre-028 build (2026-10-26), which tagged a
// file "x" when its owner execute bit was set. They pin the legacy identity
// that existing lock files carry: never regenerate them from current code.
const (
	goldenPlainV1        = "sha256:bde41aec594248aa466976e307b04d1a20c56d1311c809f0605ba00ff76652d2"
	goldenPlainAllExecV1 = "sha256:ca67ee96c32cc862402d586cd694638698227fd8c825d1c8480b21ff53b42389"
	goldenExecV1         = "sha256:df5908c03716a82fce16c14156f70c07a12699192da9c1a01f6be1d2794fd823"
	goldenExecNoExecBits = "sha256:2c7f4dbe2045a7a2463c07de9b0105e65cfdf25850da460df209e447c7cf7f72"
	goldenExecAllExecV1  = "sha256:088872ffd550475c4750c0f7f56aa9a0e06cc477164a69ca16ff4d4406b596d3"
)

// goldenTree builds the fixture the golden hashes were recorded from. Modes
// are applied with Chmod after writing so the process umask cannot alter them.
func goldenTree(t *testing.T, scriptMode, otherMode os.FileMode, withScript bool) string {
	t.Helper()

	dir := t.TempDir()
	put := func(rel, body string, mode os.FileMode) {
		t.Helper()
		writeFile(t, dir, rel, []byte(body), 0o600)
		if err := os.Chmod(filepath.Join(dir, rel), mode); err != nil {
			t.Fatal(err)
		}
	}
	put("SKILL.md", "---\nname: golden\ndescription: spec 028 golden fixture\n---\n# golden\n", otherMode)
	put("docs/notes.txt", "line one\r\nline two\r\n", otherMode)
	if withScript {
		put("scripts/run.sh", "#!/bin/sh\necho ok\n", scriptMode)
	}
	if err := os.Symlink("SKILL.md", filepath.Join(dir, "ref.md")); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestHashDir_LegacyGoldenPre028(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		scriptMode os.FileMode
		otherMode  os.FileMode
		withScript bool
		want       string
	}{
		{"plain", 0, 0o644, false, goldenPlainV1},
		{"plain all executable", 0, 0o755, false, goldenPlainAllExecV1},
		{"executable script", 0o755, 0o644, true, goldenExecV1},
		{"all executable with script", 0o755, 0o755, true, goldenExecAllExecV1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h, err := integrity.HashDir(goldenTree(t, tc.scriptMode, tc.otherMode, tc.withScript))
			if err != nil {
				t.Fatal(err)
			}
			if h.LegacyContentHash != tc.want {
				t.Errorf("LegacyContentHash = %s, want pinned pre-028 value %s", h.LegacyContentHash, tc.want)
			}
		})
	}
}

func TestHashDir_ContentHashGoldenV2(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		scriptMode os.FileMode
		otherMode  os.FileMode
		withScript bool
		want       string
	}{
		{"plain", 0, 0o644, false, goldenPlainV1},
		{"plain all executable", 0, 0o755, false, goldenPlainV1},
		{"executable script", 0o755, 0o644, true, goldenExecNoExecBits},
		{"all executable with script", 0o755, 0o755, true, goldenExecNoExecBits},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h, err := integrity.HashDir(goldenTree(t, tc.scriptMode, tc.otherMode, tc.withScript))
			if err != nil {
				t.Fatal(err)
			}
			if h.ContentHash != tc.want {
				t.Errorf("ContentHash = %s, want %s: permissions must not change the identity", h.ContentHash, tc.want)
			}
		})
	}
}
