package vault

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func assetVault(t *testing.T, files map[string]string) (*Vault, string) {
	t.Helper()
	root := t.TempDir()
	for rel, body := range files {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	v, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	return v, root
}

func caseInsensitive(t *testing.T, dir string) bool {
	t.Helper()
	probe := filepath.Join(dir, "CaseProbe")
	if err := os.WriteFile(probe, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(probe)
	_, err := os.Stat(filepath.Join(dir, "caseprobe"))
	return err == nil
}

func names(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Name()
	}
	return out
}

func TestRenameAssetOntoAHardLinkOfItselfIsACollision(t *testing.T) {
	v, root := assetVault(t, map[string]string{"assets/a.png": "png", "assets/a.png.zenrename.tmp": "precious"})
	dir := filepath.Join(root, "assets")
	if err := os.Link(filepath.Join(dir, "a.png"), filepath.Join(dir, "b.png")); err != nil {
		t.Skipf("no hard links here: %v", err)
	}
	_, err := v.RenameAsset("assets/a.png", "b.png")
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("%v", err)
	}
	if got := strings.Join(names(t, dir), ","); got != "a.png,a.png.zenrename.tmp,b.png" {
		t.Fatalf("%s", got)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "a.png.zenrename.tmp"))
	if string(data) != "precious" {
		t.Fatalf("a file was overwritten: %q", data)
	}
}

func TestRenameAssetDoesNotReplaceALink(t *testing.T) {
	for name, target := range map[string]string{"dangling": "nowhere.png", "to itself": "a.png", "to another": "c.png"} {
		t.Run(name, func(t *testing.T) {
			v, root := assetVault(t, map[string]string{"assets/a.png": "png", "assets/c.png": "other"})
			dest := filepath.Join(root, "assets", "b.png")
			if err := os.Symlink(target, dest); err != nil {
				t.Skip(err)
			}
			_, err := v.RenameAsset("assets/a.png", "b.png")
			if err == nil || !strings.Contains(err.Error(), "already exists") {
				t.Fatalf("%v", err)
			}
			if got, err := os.Readlink(dest); err != nil || got != target {
				t.Fatalf("the link was replaced: %q %v", got, err)
			}
			if _, err := os.Stat(filepath.Join(root, "assets", "a.png")); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCaseOnlyAssetRenameLeavesOtherFilesAlone(t *testing.T) {
	v, root := assetVault(t, map[string]string{"assets/pic.png": "png", "assets/pic.png.zenrename.tmp": "precious"})
	dir := filepath.Join(root, "assets")
	if !caseInsensitive(t, dir) {
		t.Skip("needs a case-insensitive volume")
	}
	meta, err := v.RenameAsset("assets/pic.png", "PIC.png")
	if err != nil {
		t.Fatal(err)
	}
	if meta.Path != "assets/PIC.png" {
		t.Fatalf("%+v", meta)
	}
	if got := strings.Join(names(t, dir), ","); got != "PIC.png,pic.png.zenrename.tmp" {
		t.Fatalf("%s", got)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "pic.png.zenrename.tmp"))
	if string(data) != "precious" {
		t.Fatalf("a file was overwritten: %q", data)
	}
}

func TestMoveAssetToItsOwnFolderUnderAnotherNameDoesNothing(t *testing.T) {
	t.Run("through a link", func(t *testing.T) {
		v, root := assetVault(t, map[string]string{"assets/pic.png": "png"})
		if err := os.Symlink("assets", filepath.Join(root, "alias")); err != nil {
			t.Skip(err)
		}
		meta, err := v.MoveAsset("assets/pic.png", "alias")
		if err != nil {
			t.Fatal(err)
		}
		if meta.Path != "assets/pic.png" {
			t.Fatalf("%+v", meta)
		}
		if got := strings.Join(names(t, filepath.Join(root, "assets")), ","); got != "pic.png" {
			t.Fatalf("%s", got)
		}
	})
	t.Run("in another case", func(t *testing.T) {
		v, root := assetVault(t, map[string]string{"assets/pic.png": "png"})
		if !caseInsensitive(t, root) {
			t.Skip("needs a case-insensitive volume")
		}
		meta, err := v.MoveAsset("assets/pic.png", "ASSETS")
		if err != nil {
			t.Fatal(err)
		}
		if meta.Path != "assets/pic.png" {
			t.Fatalf("%+v", meta)
		}
		if got := strings.Join(names(t, filepath.Join(root, "assets")), ","); got != "pic.png" {
			t.Fatalf("%s", got)
		}
	})
}

func TestSectionUnderAHeadingWithOnlySpaces(t *testing.T) {
	body := "#   \nfirst\nsecond\n# Next\n"
	s, err := FindSection(body, "#", -1)
	if err != nil {
		t.Fatal(err)
	}
	if s.BodyStart != s.Line+1 {
		t.Fatalf("%+v", s)
	}
	if got := SectionBody(body, s); got != "first\nsecond" {
		t.Fatalf("%q", got)
	}
	// A setext heading still has two lines to skip.
	setext := "Title\n=====\nbody\n"
	s, err = FindSection(setext, "Title", -1)
	if err != nil {
		t.Fatal(err)
	}
	if got := SectionBody(setext, s); got != "body" {
		t.Fatalf("%q", got)
	}
}
