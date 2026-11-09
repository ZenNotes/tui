package vault

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// finishes fails the test when a call does not return: these searches ran for ever on
// a name no file can have.
func finishes(t *testing.T, name string, call func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		call()
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatalf("%s never returned", name)
	}
}

func TestTitlesThatFillAFileName(t *testing.T) {
	root := t.TempDir()
	v, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	for name, title := range map[string]string{
		"latin":   strings.Repeat("x", 300),
		"chinese": strings.Repeat("漢", 200),
		"emoji":   strings.Repeat("😀", 200),
	} {
		t.Run(name, func(t *testing.T) {
			var meta NoteMeta
			var err error
			finishes(t, "CreateNote", func() { meta, err = v.CreateNote(FolderInbox, title, "", nil) })
			if err != nil {
				t.Fatal(err)
			}
			if base := filepath.Base(meta.Path); len(base) > 255 {
				t.Errorf("a %d-byte name", len(base))
			}
		})
	}
}

func TestSearchesForAFreeNameStopOnAnError(t *testing.T) {
	dir := t.TempDir()
	long := strings.Repeat("x", 300)
	var err error
	// The filesystem's answer, on the first try, and not giving up after many.
	stopped := func(what string) {
		t.Helper()
		if err == nil || strings.Contains(err.Error(), "no free name") {
			t.Errorf("%s: %v", what, err)
		}
	}
	finishes(t, "uniqueTitle", func() { _, err = uniqueTitle(dir, "a\x00b") })
	stopped("uniqueTitle, a NUL")
	// Windows answers a name this long as not existing, which ends the search too.
	if runtime.GOOS == "windows" {
		return
	}
	finishes(t, "uniqueTitle", func() { _, err = uniqueTitle(dir, long) })
	stopped("uniqueTitle, a name too long")
	finishes(t, "uniqueTemplateSlug", func() { _, err = uniqueTemplateSlug(dir, long, "") })
	stopped("uniqueTemplateSlug")
	finishes(t, "uniquePath", func() { _, err = uniquePath(dir, long, ".md") })
	stopped("uniquePath")
}

func TestSearchForAFreeNameInAnUnreadableFolder(t *testing.T) {
	if runtime.GOOS == "windows" || os.Getuid() == 0 {
		t.Skip("permissions do not stop this user")
	}
	dir := filepath.Join(t.TempDir(), "locked")
	if err := os.Mkdir(dir, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	var err error
	finishes(t, "uniqueTitle", func() { _, err = uniqueTitle(dir, "Note") })
	if err == nil {
		t.Error("found a name in a folder nothing can read")
	}
}
