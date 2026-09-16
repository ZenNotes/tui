package vault

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func linuxBirthtimeStat(t *testing.T, path string) unix.Statx_t {
	t.Helper()
	var st unix.Statx_t
	if err := unix.Statx(unix.AT_FDCWD, path, 0, unix.STATX_BTIME|unix.STATX_CTIME|unix.STATX_MTIME, &st); err != nil {
		if errors.Is(err, unix.ENOSYS) || errors.Is(err, unix.EOPNOTSUPP) {
			t.Skipf("Linux filesystem birth times unavailable: %v", err)
		}
		t.Fatalf("statx fixture: %v", err)
	}
	if st.Mask&unix.STATX_BTIME == 0 {
		t.Skip("Linux filesystem does not report STATX_BTIME")
	}
	return st
}

func linuxTimestampMillis(ts unix.StatxTimestamp) int64 {
	return ts.Sec*1000 + int64(ts.Nsec)/1_000_000
}

func TestReadNoteUsesLinuxFilesystemBirthtime(t *testing.T) {
	v := newTestVault(t)
	rel := "inbox/Filesystem creation date.md"
	abs := filepath.Join(v.Root(), filepath.FromSlash(rel))
	writeFile(t, abs, "# Keep the filesystem creation date\n")
	before := linuxBirthtimeStat(t, abs)

	// Updating the modification time changes ctime but leaves birth time
	// intact. Give ctime its own millisecond so the old implementation cannot
	// accidentally pass by reporting ctime from the original creation call.
	time.Sleep(20 * time.Millisecond)
	wantModified := time.Date(2000, 1, 1, 0, 0, 0, 123_000_000, time.UTC)
	if err := os.Chtimes(abs, wantModified, wantModified); err != nil {
		t.Fatal(err)
	}
	after := linuxBirthtimeStat(t, abs)
	if before.Btime != after.Btime {
		t.Fatal("fixture modification changed the filesystem birth time")
	}
	if linuxTimestampMillis(after.Ctime) == linuxTimestampMillis(after.Btime) {
		t.Fatal("fixture ctime did not advance beyond its birth time")
	}

	note, err := v.ReadNote(rel)
	if err != nil {
		t.Fatal(err)
	}
	if want := linuxTimestampMillis(after.Btime); note.CreatedAt != want {
		t.Errorf("createdAt = %d, want filesystem birth time %d (ctime is %d)", note.CreatedAt, want, linuxTimestampMillis(after.Ctime))
	}
	if note.UpdatedAt != wantModified.UnixMilli() {
		t.Errorf("updatedAt = %d, want modification time %d", note.UpdatedAt, wantModified.UnixMilli())
	}
}

func TestReadNoteCreationDateDoesNotChangeAfterLinuxChmod(t *testing.T) {
	v := newTestVault(t)
	rel := "inbox/Permissions change.md"
	abs := filepath.Join(v.Root(), filepath.FromSlash(rel))
	writeFile(t, abs, "# Permissions do not create a new note\n")
	beforeStat := linuxBirthtimeStat(t, abs)
	before, err := v.ReadNote(rel)
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(20 * time.Millisecond)
	if err := os.Chmod(abs, 0o600); err != nil {
		t.Fatal(err)
	}
	afterStat := linuxBirthtimeStat(t, abs)
	if beforeStat.Btime != afterStat.Btime {
		t.Fatal("chmod changed the fixture's filesystem birth time")
	}
	if linuxTimestampMillis(beforeStat.Ctime) == linuxTimestampMillis(afterStat.Ctime) {
		t.Fatal("chmod did not advance fixture ctime")
	}

	after, err := v.ReadNote(rel)
	if err != nil {
		t.Fatal(err)
	}
	if after.CreatedAt != before.CreatedAt {
		t.Errorf("chmod changed createdAt from %d to %d; filesystem birth time remained %d", before.CreatedAt, after.CreatedAt, linuxTimestampMillis(afterStat.Btime))
	}
	if after.UpdatedAt != before.UpdatedAt {
		t.Errorf("chmod changed updatedAt from %d to %d", before.UpdatedAt, after.UpdatedAt)
	}
}
