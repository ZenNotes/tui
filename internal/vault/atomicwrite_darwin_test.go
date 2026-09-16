package vault

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

func TestWriteNotePreservesExistingCreationTime(t *testing.T) {
	v := newTestVault(t)
	rel := "inbox/Creation time.md"
	abs := filepath.Join(v.Root(), filepath.FromSlash(rel))
	writeFile(t, abs, "# Original note\n")

	// Give the existing note a known, older birth time so the assertion does
	// not depend on sleeps or the filesystem clock advancing between writes.
	wantBirth := syscall.Timespec{Sec: 946684800, Nsec: 123456789}
	var birthTime [16]byte
	binary.NativeEndian.PutUint64(birthTime[:8], uint64(wantBirth.Sec))
	binary.NativeEndian.PutUint64(birthTime[8:], uint64(wantBirth.Nsec))
	attrs := unix.Attrlist{Bitmapcount: unix.ATTR_BIT_MAP_COUNT, Commonattr: unix.ATTR_CMN_CRTIME}
	if err := unix.Setattrlist(abs, &attrs, birthTime[:], 0); err != nil {
		t.Fatalf("set fixture creation time: %v", err)
	}
	var before syscall.Stat_t
	if err := syscall.Stat(abs, &before); err != nil {
		t.Fatal(err)
	}
	if before.Birthtimespec != wantBirth {
		t.Fatalf("fixture birth time = %+v, want %+v", before.Birthtimespec, wantBirth)
	}

	wantBody := "# Café 日本語\n\nTrailing spaces stay here.  \n\tIndentation survives.\t \n\n"
	meta, err := v.WriteNote(rel, wantBody)
	if err != nil {
		t.Fatal(err)
	}
	gotBody, err := os.ReadFile(abs)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotBody) != wantBody {
		t.Errorf("saved body = %q, want exact bytes %q", gotBody, wantBody)
	}
	var after syscall.Stat_t
	if err := syscall.Stat(abs, &after); err != nil {
		t.Fatal(err)
	}
	if after.Birthtimespec != before.Birthtimespec {
		t.Errorf("WriteNote changed existing creation time: before %+v, after %+v", before.Birthtimespec, after.Birthtimespec)
	}
	wantCreatedAt := wantBirth.Sec*1000 + wantBirth.Nsec/1_000_000
	if meta.CreatedAt != wantCreatedAt {
		t.Errorf("returned createdAt = %d, want %d", meta.CreatedAt, wantCreatedAt)
	}
}
